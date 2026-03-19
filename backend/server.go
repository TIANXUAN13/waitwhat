package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Server struct {
	repo         *Repository
	tokens       *TokenManager
	loginLimiter *LoginRateLimiter
}

var requestSeq uint64

func NewServer(repo *Repository) *Server {
	return &Server{
		repo:         repo,
		tokens:       NewTokenManager(repo.cfg.Auth.TokenSecret),
		loginLimiter: NewLoginRateLimiter(repo.cfg.Auth.LoginLimitMaxFail, time.Duration(repo.cfg.Auth.LoginLimitWindow)*time.Second),
	}
}

type LoginRateLimiter struct {
	mu       sync.Mutex
	attempts map[string]*loginAttempt
	limit    int
	window   time.Duration
}

type loginAttempt struct {
	failedUntil time.Time
	failedCount int
	lastSeen    time.Time
}

func NewLoginRateLimiter(limit int, window time.Duration) *LoginRateLimiter {
	return &LoginRateLimiter{
		attempts: make(map[string]*loginAttempt),
		limit:    limit,
		window:   window,
	}
}

func (l *LoginRateLimiter) key(ip, username string) string {
	return strings.ToLower(strings.TrimSpace(username)) + "|" + strings.TrimSpace(ip)
}

func (l *LoginRateLimiter) Allow(ip, username string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cleanupLocked()

	k := l.key(ip, username)
	item, ok := l.attempts[k]
	if !ok {
		return true, 0
	}
	now := time.Now()
	if now.Before(item.failedUntil) {
		return false, time.Until(item.failedUntil)
	}
	return true, 0
}

func (l *LoginRateLimiter) Fail(ip, username string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	k := l.key(ip, username)
	now := time.Now()
	item, ok := l.attempts[k]
	if !ok {
		l.attempts[k] = &loginAttempt{failedCount: 1, lastSeen: now}
		return
	}
	item.failedCount++
	item.lastSeen = now
	if item.failedCount >= l.limit {
		item.failedUntil = now.Add(l.window)
		item.failedCount = 0
	}
}

func (l *LoginRateLimiter) Success(ip, username string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, l.key(ip, username))
}

func (l *LoginRateLimiter) cleanupLocked() {
	now := time.Now()
	for k, item := range l.attempts {
		if now.Sub(item.lastSeen) > 30*time.Minute {
			delete(l.attempts, k)
		}
	}
}

func (l *LoginRateLimiter) Update(limit int, window time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if limit > 0 {
		l.limit = limit
	}
	if window > 0 {
		l.window = window
	}
	l.attempts = make(map[string]*loginAttempt)
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/health/live", s.handleHealthLive)
	mux.HandleFunc("/api/health/ready", s.handleHealthReady)
	mux.HandleFunc("/api/bootstrap", s.handleBootstrap)
	mux.HandleFunc("/api/database/init", s.handleInitDatabase)
	mux.HandleFunc("/api/database/reset", s.handleResetDatabase)
	mux.HandleFunc("/api/auth/setup-admin", s.handleSetupAdmin)
	mux.HandleFunc("/api/auth/login", s.handleLogin)
	mux.HandleFunc("/api/auth/register", s.handleRegister)
	mux.HandleFunc("/api/auth/me", s.handleMe)
	mux.HandleFunc("/api/auth/logout", s.handleLogout)
	mux.HandleFunc("/api/admin/users", s.handleAdminUsers)
	mux.HandleFunc("/api/admin/users/", s.handleAdminUserByID)
	mux.HandleFunc("/api/admin/login-policy", s.handleAdminLoginPolicy)
	mux.HandleFunc("/api/admin/audit-logs", s.handleAdminAuditLogs)
	mux.HandleFunc("/api/mail/config", s.handleMailConfig)
	mux.HandleFunc("/api/mail/test", s.handleMailTest)
	mux.HandleFunc("/api/mail/diagnose", s.handleMailDiagnose)
	mux.HandleFunc("/api/dingtalk/config", s.handleDingTalkConfig)
	mux.HandleFunc("/api/dingtalk/test", s.handleDingTalkTest)
	mux.HandleFunc("/api/reminders/dispatch", s.handleDispatchReminders)
	mux.HandleFunc("/api/notify-groups", s.handleNotifyGroups)
	mux.HandleFunc("/api/notify-groups/", s.handleNotifyGroupByID)
	mux.HandleFunc("/api/events", s.handleEvents)
	mux.HandleFunc("/api/events/", s.handleEventByID)
	registerFrontendRoutes(mux)

	return withRequestLogging(withCORS(mux))
}

func registerFrontendRoutes(mux *http.ServeMux) {
	webDir := strings.TrimSpace(envOrDefault("APP_WEB_DIR", "./web"))
	if webDir == "" {
		return
	}
	indexPath := webDir + "/index.html"
	if _, err := os.Stat(indexPath); err != nil {
		log.Printf("frontend static disabled: %s not found", indexPath)
		return
	}
	log.Printf("frontend static enabled: %s", webDir)
	fileServer := http.FileServer(http.Dir(webDir))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		target := webDir + r.URL.Path
		if info, err := os.Stat(target); err == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, indexPath)
	}))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true,
	})
}

func (s *Server) handleHealthLive(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":   true,
		"type": "live",
	})
}

func (s *Server) handleHealthReady(w http.ResponseWriter, r *http.Request) {
	db, err := s.repo.openDB()
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "type": "ready", "error": err.Error()})
		return
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := pingDB(ctx, db); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "type": "ready", "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":   true,
		"type": "ready",
	})
}

func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var currentUserID int64
	if user, err := s.authUser(r); err == nil {
		currentUserID = user.ID
	}
	state, err := s.repo.Bootstrap(r.Context(), currentUserID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	state.Database = state.Database.Safe()
	if user, err := s.authUser(r); err == nil {
		state.Auth.CurrentUser = &user
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) handleInitDatabase(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req InitDatabaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体解析失败"})
		return
	}

	config, err := s.repo.InitDatabase(context.Background(), req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message":  "数据库初始化配置已保存",
		"database": config,
	})
}

func (s *Server) handleResetDatabase(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if err := s.repo.ResetDatabaseConfig(true); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "数据库配置已重置，请重新初始化"})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		user, err := s.requireAuth(r)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
			return
		}
		var req CreateEventRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体解析失败"})
			return
		}
		req.UserID = user.ID
		event, err := s.repo.CreateEvent(r.Context(), req)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"message": "事件创建成功",
			"event":   event,
		})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (s *Server) handleEventByID(w http.ResponseWriter, r *http.Request) {
	user, err := s.requireAuth(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	idText := strings.TrimPrefix(r.URL.Path, "/api/events/")
	eventID, err := strconv.ParseInt(idText, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "事件 ID 不合法"})
		return
	}

	switch r.Method {
	case http.MethodPut:
		var req UpdateEventRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体解析失败"})
			return
		}
		req.UserID = user.ID
		event, err := s.repo.UpdateEvent(r.Context(), eventID, user.ID, req)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"message": "事件已更新", "event": event})
	case http.MethodDelete:
		if err := s.repo.DeleteEvent(r.Context(), eventID, user.ID); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"message": "事件已删除"})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (s *Server) handleMailConfig(w http.ResponseWriter, r *http.Request) {
	user, err := s.requireAuth(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req SaveMailConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体解析失败"})
		return
	}

	cfg, err := s.repo.SaveMailConfig(r.Context(), user.ID, req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message": "SMTP 配置已保存",
		"mail":    cfg,
	})
}

func (s *Server) handleDispatchReminders(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAuth(r); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	result, err := s.repo.DispatchDueReminders(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message": "提醒扫描完成",
		"result":  result,
	})
}

func (s *Server) handleMailTest(w http.ResponseWriter, r *http.Request) {
	user, err := s.requireAuth(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req SendTestMailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体解析失败"})
		return
	}

	if err := s.repo.SendTestMail(r.Context(), user.ID, req.To); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message": "测试邮件已发送",
	})
}

func (s *Server) handleMailDiagnose(w http.ResponseWriter, r *http.Request) {
	user, err := s.requireAuth(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req DiagnoseMailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体解析失败"})
		return
	}
	result, err := s.repo.DiagnoseMail(r.Context(), user.ID, req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"message": "SMTP 连接诊断完成",
		"result":  result,
	})
}

func (s *Server) handleDingTalkConfig(w http.ResponseWriter, r *http.Request) {
	user, err := s.requireAuth(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req SaveDingTalkConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体解析失败"})
		return
	}

	cfg, err := s.repo.SaveDingTalkConfig(r.Context(), user.ID, req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message":  "钉钉机器人配置已保存",
		"dingTalk": cfg,
	})
}

func (s *Server) handleDingTalkTest(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAuth(r); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req SendTestDingTalkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体解析失败"})
		return
	}
	if err := s.repo.SendTestDingTalkWebhook(r.Context(), req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "钉钉测试消息已发送"})
}

func (s *Server) handleNotifyGroups(w http.ResponseWriter, r *http.Request) {
	user, err := s.requireAuth(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req SaveNotificationGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体解析失败"})
		return
	}
	group, err := s.repo.SaveNotificationGroup(r.Context(), user.ID, req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"message": "通知组已保存", "group": group})
}

func (s *Server) handleNotifyGroupByID(w http.ResponseWriter, r *http.Request) {
	user, err := s.requireAuth(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	if r.Method != http.MethodDelete {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	idText := strings.TrimPrefix(r.URL.Path, "/api/notify-groups/")
	groupID, err := strconv.ParseInt(idText, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "通知组 ID 不合法"})
		return
	}
	if err := s.repo.DeleteNotificationGroup(r.Context(), user.ID, groupID); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "通知组已删除"})
}

func (s *Server) handleSetupAdmin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req SetupAdminRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体解析失败"})
		return
	}
	user, err := s.repo.SetupAdmin(r.Context(), req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	token, err := s.tokens.Create(user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, AuthResponse{Token: token, User: user})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体解析失败"})
		return
	}
	clientIP := requestClientIP(r)
	if ok, wait := s.loginLimiter.Allow(clientIP, req.Username); !ok {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": fmt.Sprintf("登录尝试过于频繁，请在 %d 秒后重试", int(wait.Seconds())+1)})
		return
	}
	user, err := s.repo.Authenticate(r.Context(), req.Username, req.Password)
	if err != nil {
		s.loginLimiter.Fail(clientIP, req.Username)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	s.loginLimiter.Success(clientIP, req.Username)
	token, err := s.tokens.Create(user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, AuthResponse{Token: token, User: user})
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体解析失败"})
		return
	}
	user, err := s.repo.Register(r.Context(), req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	token, err := s.tokens.Create(user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, AuthResponse{Token: token, User: user})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	user, err := s.requireAuth(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"message": "已退出登录"})
}

func (s *Server) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	admin, err := s.requireAdmin(r)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}
	_ = admin
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	users, err := s.repo.AdminListUsers(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

func (s *Server) handleAdminUserByID(w http.ResponseWriter, r *http.Request) {
	admin, err := s.requireAdmin(r)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}
	idText := strings.TrimPrefix(r.URL.Path, "/api/admin/users/")
	userID, err := strconv.ParseInt(idText, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "用户 ID 不合法"})
		return
	}
	switch r.Method {
	case http.MethodDelete:
		if userID == admin.ID {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "不能删除当前登录管理员"})
			return
		}
		target, err := s.repo.adminGetUserByID(r.Context(), userID)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := s.repo.AdminDeleteUser(r.Context(), userID); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		_ = s.repo.AppendAdminAuditLog(r.Context(), admin, AdminAuditLog{
			Action:         "admin_delete_user",
			TargetUserID:   target.ID,
			TargetUsername: target.Username,
			Detail:         "管理员删除用户及其相关数据",
		})
		writeJSON(w, http.StatusOK, map[string]string{"message": "用户已删除"})
	case http.MethodPut:
		var req AdminUpdateUserPasswordRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体解析失败"})
			return
		}
		target, err := s.repo.adminGetUserByID(r.Context(), userID)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := s.repo.AdminUpdateUserPassword(r.Context(), userID, req.Password); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		_ = s.repo.AppendAdminAuditLog(r.Context(), admin, AdminAuditLog{
			Action:         "admin_update_user_password",
			TargetUserID:   target.ID,
			TargetUsername: target.Username,
			Detail:         "管理员修改用户密码",
		})
		writeJSON(w, http.StatusOK, map[string]string{"message": "用户密码已更新"})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (s *Server) handleAdminLoginPolicy(w http.ResponseWriter, r *http.Request) {
	admin, err := s.requireAdmin(r)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req AdminUpdateLoginPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体解析失败"})
		return
	}
	if req.LoginMaxFailed < 1 || req.LoginMaxFailed > 20 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "失败次数阈值范围应为 1-20"})
		return
	}
	if req.LoginWindowSec < 30 || req.LoginWindowSec > 3600 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "限流窗口范围应为 30-3600 秒"})
		return
	}

	cfg := s.repo.cfg
	cfg.Auth.LoginLimitMaxFail = req.LoginMaxFailed
	cfg.Auth.LoginLimitWindow = req.LoginWindowSec
	if err := saveConfig(cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.repo.cfg = cfg
	s.loginLimiter.Update(cfg.Auth.LoginLimitMaxFail, time.Duration(cfg.Auth.LoginLimitWindow)*time.Second)
	_ = s.repo.AppendAdminAuditLog(r.Context(), admin, AdminAuditLog{
		Action:         "admin_update_login_policy",
		TargetUserID:   admin.ID,
		TargetUsername: admin.Username,
		Detail:         fmt.Sprintf("更新登录限流: maxFailed=%d, windowSec=%d", cfg.Auth.LoginLimitMaxFail, cfg.Auth.LoginLimitWindow),
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"message": "登录限流策略已更新",
		"policy": map[string]int{
			"loginMaxFailed": cfg.Auth.LoginLimitMaxFail,
			"loginWindowSec": cfg.Auth.LoginLimitWindow,
		},
	})
}

func (s *Server) handleAdminAuditLogs(w http.ResponseWriter, r *http.Request) {
	_, err := s.requireAdmin(r)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	limit := 20
	offset := 0
	if text := strings.TrimSpace(r.URL.Query().Get("limit")); text != "" {
		if v, parseErr := strconv.Atoi(text); parseErr == nil {
			limit = v
		}
	}
	if text := strings.TrimSpace(r.URL.Query().Get("offset")); text != "" {
		if v, parseErr := strconv.Atoi(text); parseErr == nil {
			offset = v
		}
	}
	items, total, err := s.repo.AdminListAuditLogs(r.Context(), limit, offset)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
}

func (s *Server) requireAuth(r *http.Request) (User, error) {
	return s.authUser(r)
}

func (s *Server) requireAdmin(r *http.Request) (User, error) {
	user, err := s.requireAuth(r)
	if err != nil {
		return User{}, err
	}
	if user.Role != "admin" {
		return User{}, errors.New("仅管理员可执行该操作")
	}
	return user, nil
}

func (s *Server) authUser(r *http.Request) (User, error) {
	token := bearerToken(r)
	if token == "" {
		return User{}, errors.New("请先登录")
	}
	userID, err := s.tokens.Parse(token)
	if err != nil {
		return User{}, err
	}
	return s.repo.FindUserByID(r.Context(), userID)
}

func bearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return ""
	}
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func withCORS(next http.Handler) http.Handler {
	allowed := parseAllowedOrigins(envOrDefault("APP_CORS_ALLOW_ORIGIN", "http://127.0.0.1:5173,http://localhost:5173"))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin != "" {
			if isOriginAllowed(origin, allowed) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
			} else if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusForbidden)
				return
			}
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func withRequestLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := strconv.FormatUint(atomic.AddUint64(&requestSeq, 1), 10)
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		rec.Header().Set("X-Request-Id", reqID)
		next.ServeHTTP(rec, r)
		cost := time.Since(start).Milliseconds()
		log.Printf("request_id=%s method=%s path=%s status=%d cost_ms=%d remote=%s", reqID, r.Method, r.URL.Path, rec.status, cost, requestClientIP(r))
	})
}

func parseAllowedOrigins(raw string) []string {
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}
		result = append(result, value)
	}
	return result
}

func isOriginAllowed(origin string, allowed []string) bool {
	for _, item := range allowed {
		if item == "*" || strings.EqualFold(item, origin) {
			return true
		}
	}
	return false
}

func requestClientIP(r *http.Request) string {
	if fwd := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); fwd != "" {
		parts := strings.Split(fwd, ",")
		if len(parts) > 0 {
			ip := strings.TrimSpace(parts[0])
			if ip != "" {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && strings.TrimSpace(host) != "" {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}
