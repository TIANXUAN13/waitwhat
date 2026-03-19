package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
)

type Server struct {
	repo   *Repository
	tokens *TokenManager
}

func NewServer(repo *Repository) *Server {
	return &Server{repo: repo, tokens: NewTokenManager(repo.cfg.Auth.TokenSecret)}
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/bootstrap", s.handleBootstrap)
	mux.HandleFunc("/api/database/init", s.handleInitDatabase)
	mux.HandleFunc("/api/database/reset", s.handleResetDatabase)
	mux.HandleFunc("/api/auth/setup-admin", s.handleSetupAdmin)
	mux.HandleFunc("/api/auth/login", s.handleLogin)
	mux.HandleFunc("/api/auth/register", s.handleRegister)
	mux.HandleFunc("/api/auth/me", s.handleMe)
	mux.HandleFunc("/api/auth/logout", s.handleLogout)
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

	return withCORS(mux)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true,
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
	user, err := s.repo.Authenticate(r.Context(), req.Username, req.Password)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
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

func (s *Server) requireAuth(r *http.Request) (User, error) {
	return s.authUser(r)
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
	allowOrigin := envOrDefault("APP_CORS_ALLOW_ORIGIN", "*")
	if strings.TrimSpace(allowOrigin) == "" {
		allowOrigin = "*"
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", allowOrigin)
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
