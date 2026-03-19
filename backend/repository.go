package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

type Repository struct {
	cfg           AppConfig
	mailerFactory func(MailConfig) Mailer
	dingFactory   func(DingTalkConfig) DingTalkSender
}

func NewRepository() (*Repository, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}
	return &Repository{cfg: cfg, mailerFactory: NewMailer, dingFactory: NewDingTalkSender}, nil
}

func (r *Repository) Bootstrap(ctx context.Context, currentUserID int64) (AppState, error) {
	state := AppState{Database: r.cfg.Database}
	if !r.cfg.Database.InitializedAt.IsZero() {
		db, err := r.openDB()
		if err == nil {
			defer db.Close()
			if err := runMigrations(ctx, db, r.cfg.Database.SelectedDriver); err != nil {
				return state, err
			}

			adminExists, authErr := hasAdmin(ctx, db)
			users, userErr := loadUsers(ctx, db, r.cfg.Database.SelectedDriver)
			events, eventErr := loadEvents(ctx, db, r.cfg.Database.SelectedDriver)
			tasks, taskErr := loadTasks(ctx, db)
			logs, logErr := loadLogs(ctx, db)
			groups, groupErr := loadNotificationGroups(ctx, db, r.cfg.Database.SelectedDriver, currentUserID)
			if userErr == nil && eventErr == nil && logErr == nil && taskErr == nil && authErr == nil && groupErr == nil {
				state.Auth.AdminExists = adminExists
				if currentUserID > 0 {
					state.Users = filterUsersByID(users, currentUserID)
					state.Events = filterEventsByUser(events, currentUserID)
					state.Tasks = filterTasksByEvents(tasks, state.Events)
					state.Logs = filterLogsByEvents(logs, state.Events)
					state.NotifyGroups = groups
					mailCfg, _ := loadUserMailConfig(ctx, db, r.cfg.Database.SelectedDriver, currentUserID)
					dingCfg, _ := loadUserDingTalkConfig(ctx, db, r.cfg.Database.SelectedDriver, currentUserID)
					state.Mail = mailCfg.Safe()
					state.DingTalk = dingCfg.Safe()
				} else {
					state.Users = users
					state.Events = events
					state.Tasks = tasks
					state.Logs = logs
				}
				return state, nil
			}
		}
	}
	return state, nil
}

func (r *Repository) ResetDatabaseConfig(removeData bool) error {
	cfg := r.cfg
	if removeData && cfg.Database.SelectedDriver == DriverSQLite && cfg.Database.SQLitePath != "" {
		_ = os.Remove(cfg.Database.SQLitePath)
	}
	cfg.Database = defaultConfig().Database
	if err := saveConfig(cfg); err != nil {
		return err
	}
	r.cfg = cfg
	return nil
}

func (r *Repository) InitDatabase(ctx context.Context, req InitDatabaseRequest) (DatabaseConfig, error) {
	cfg := defaultConfig().Database

	switch req.Driver {
	case DriverSQLite:
		path := req.SQLitePath
		if path == "" {
			path = defaultSQLitePath
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return DatabaseConfig{}, err
		}
		cfg.SelectedDriver = DriverSQLite
		cfg.SQLitePath = path
		cfg.PGPort = 5432
		cfg.PGSSLMode = "disable"
		cfg.InitializedAt = time.Now()
	case DriverPG:
		if req.PGHost == "" || req.PGDatabase == "" || req.PGUser == "" {
			return DatabaseConfig{}, errors.New("postgres 配置不完整")
		}
		cfg.SelectedDriver = DriverPG
		cfg.SQLitePath = defaultSQLitePath
		cfg.PGHost = req.PGHost
		if req.PGPort == 0 {
			cfg.PGPort = 5432
		} else {
			cfg.PGPort = req.PGPort
		}
		cfg.PGDatabase = req.PGDatabase
		cfg.PGUser = req.PGUser
		cfg.PGPassword = req.PGPassword
		if req.PGSSLMode == "" {
			cfg.PGSSLMode = "disable"
		} else {
			cfg.PGSSLMode = req.PGSSLMode
		}
		cfg.InitializedAt = time.Now()
	default:
		return DatabaseConfig{}, errors.New("不支持的数据库类型")
	}

	next := AppConfig{Database: cfg, Mail: r.cfg.Mail, DingTalk: r.cfg.DingTalk, Auth: r.cfg.Auth}
	db, err := openDBFromConfig(next.Database)
	if err != nil {
		return DatabaseConfig{}, err
	}
	defer db.Close()

	if err := pingDB(ctx, db); err != nil {
		return DatabaseConfig{}, err
	}
	if err := runMigrations(ctx, db, cfg.SelectedDriver); err != nil {
		return DatabaseConfig{}, err
	}
	if err := saveConfig(next); err != nil {
		return DatabaseConfig{}, err
	}

	r.cfg = next
	return next.Database.Safe(), nil
}

func (r *Repository) CreateEvent(ctx context.Context, req CreateEventRequest) (MemoEvent, error) {
	db, err := r.openDB()
	if err != nil {
		return MemoEvent{}, err
	}
	defer db.Close()

	if err := pingDB(ctx, db); err != nil {
		return MemoEvent{}, err
	}

	eventAt, err := time.Parse(time.RFC3339, req.EventAt)
	if err != nil {
		return MemoEvent{}, errors.New("事件时间格式不正确")
	}

	now := time.Now()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return MemoEvent{}, err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
		INSERT INTO memo_events (user_id, title, content, event_at, reminder_enabled, bound_channel_ids, bound_group_ids, countdown_label, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		req.UserID, req.Title, req.Content, eventAt.Format(time.RFC3339), boolToInt(req.ReminderEnabled), encodeInt64List(req.BoundChannelIDs), encodeInt64List(req.BoundGroupIDs),
		"", "scheduled", now.Format(time.RFC3339), now.Format(time.RFC3339),
	)
	if err != nil && r.cfg.Database.SelectedDriver == DriverPG {
		result, err = tx.ExecContext(ctx, `
			INSERT INTO memo_events (user_id, title, content, event_at, reminder_enabled, bound_channel_ids, bound_group_ids, countdown_label, status, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
			req.UserID, req.Title, req.Content, eventAt.Format(time.RFC3339), boolToInt(req.ReminderEnabled), encodeInt64List(req.BoundChannelIDs), encodeInt64List(req.BoundGroupIDs),
			"", "scheduled", now.Format(time.RFC3339), now.Format(time.RFC3339),
		)
	}
	if err != nil {
		return MemoEvent{}, err
	}

	eventID, err := result.LastInsertId()
	if err != nil {
		var scannedID int64
		row := tx.QueryRowContext(ctx, "SELECT currval(pg_get_serial_sequence('memo_events','id'))")
		if scanErr := row.Scan(&scannedID); scanErr == nil {
			eventID = scannedID
		}
	}

	for _, point := range normalizeReminderPoints(req.ReminderPoints) {
		if _, err := execWithDriver(ctx, tx, r.cfg.Database.SelectedDriver, `
			INSERT INTO reminder_points (event_id, label, offset_min)
			VALUES (?, ?, ?)`,
			`INSERT INTO reminder_points (event_id, label, offset_min) VALUES ($1, $2, $3)`,
			eventID, point.Label, point.OffsetMin,
		); err != nil {
			return MemoEvent{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return MemoEvent{}, err
	}

	events, err := loadEvents(ctx, db, r.cfg.Database.SelectedDriver)
	if err != nil {
		return MemoEvent{}, err
	}
	for _, event := range events {
		if event.ID == eventID {
			return event, nil
		}
	}
	return MemoEvent{}, errors.New("事件创建后读取失败")
}

func (r *Repository) UpdateEvent(ctx context.Context, eventID, userID int64, req UpdateEventRequest) (MemoEvent, error) {
	db, err := r.openDB()
	if err != nil {
		return MemoEvent{}, err
	}
	defer db.Close()

	eventAt, err := time.Parse(time.RFC3339, req.EventAt)
	if err != nil {
		return MemoEvent{}, errors.New("事件时间格式不正确")
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return MemoEvent{}, err
	}
	defer tx.Rollback()

	res, err := execWithDriver(ctx, tx, r.cfg.Database.SelectedDriver,
		`UPDATE memo_events SET title = ?, content = ?, event_at = ?, reminder_enabled = ?, bound_channel_ids = ?, bound_group_ids = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		`UPDATE memo_events SET title = $1, content = $2, event_at = $3, reminder_enabled = $4, bound_channel_ids = $5, bound_group_ids = $6, updated_at = $7 WHERE id = $8 AND user_id = $9`,
		req.Title, req.Content, eventAt.Format(time.RFC3339), boolToInt(req.ReminderEnabled), encodeInt64List(req.BoundChannelIDs), encodeInt64List(req.BoundGroupIDs), time.Now().Format(time.RFC3339), eventID, userID,
	)
	if err != nil {
		return MemoEvent{}, err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return MemoEvent{}, errors.New("事件不存在或无权修改")
	}

	if _, err := execWithDriver(ctx, tx, r.cfg.Database.SelectedDriver,
		`DELETE FROM reminder_points WHERE event_id = ?`,
		`DELETE FROM reminder_points WHERE event_id = $1`,
		eventID,
	); err != nil {
		return MemoEvent{}, err
	}
	for _, point := range normalizeReminderPoints(req.ReminderPoints) {
		if _, err := execWithDriver(ctx, tx, r.cfg.Database.SelectedDriver,
			`INSERT INTO reminder_points (event_id, label, offset_min) VALUES (?, ?, ?)`,
			`INSERT INTO reminder_points (event_id, label, offset_min) VALUES ($1, $2, $3)`,
			eventID, point.Label, point.OffsetMin,
		); err != nil {
			return MemoEvent{}, err
		}
	}

	if _, err := execWithDriver(ctx, tx, r.cfg.Database.SelectedDriver,
		`DELETE FROM reminder_tasks WHERE event_id = ?`,
		`DELETE FROM reminder_tasks WHERE event_id = $1`,
		eventID,
	); err != nil {
		return MemoEvent{}, err
	}

	if err := tx.Commit(); err != nil {
		return MemoEvent{}, err
	}

	events, err := loadEvents(ctx, db, r.cfg.Database.SelectedDriver)
	if err != nil {
		return MemoEvent{}, err
	}
	for _, event := range events {
		if event.ID == eventID && event.UserID == userID {
			return event, nil
		}
	}
	return MemoEvent{}, errors.New("更新后读取事件失败")
}

func (r *Repository) DeleteEvent(ctx context.Context, eventID, userID int64) error {
	db, err := r.openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := execWithDriver(ctx, tx, r.cfg.Database.SelectedDriver,
		`DELETE FROM reminder_points WHERE event_id = ?`,
		`DELETE FROM reminder_points WHERE event_id = $1`,
		eventID,
	); err != nil {
		return err
	}
	if _, err := execWithDriver(ctx, tx, r.cfg.Database.SelectedDriver,
		`DELETE FROM reminder_tasks WHERE event_id = ?`,
		`DELETE FROM reminder_tasks WHERE event_id = $1`,
		eventID,
	); err != nil {
		return err
	}
	if _, err := execWithDriver(ctx, tx, r.cfg.Database.SelectedDriver,
		`DELETE FROM notification_logs WHERE event_id = ?`,
		`DELETE FROM notification_logs WHERE event_id = $1`,
		eventID,
	); err != nil {
		return err
	}
	res, err := execWithDriver(ctx, tx, r.cfg.Database.SelectedDriver,
		`DELETE FROM memo_events WHERE id = ? AND user_id = ?`,
		`DELETE FROM memo_events WHERE id = $1 AND user_id = $2`,
		eventID, userID,
	)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return errors.New("事件不存在或无权删除")
	}
	return tx.Commit()
}

func (r *Repository) openDB() (*sql.DB, error) {
	return openDBFromConfig(r.cfg.Database)
}

func openDBFromConfig(cfg DatabaseConfig) (*sql.DB, error) {
	switch cfg.SelectedDriver {
	case DriverSQLite:
		path := cfg.SQLitePath
		if path == "" {
			path = defaultSQLitePath
		}
		return sql.Open("sqlite", path)
	case DriverPG:
		if cfg.PGHost == "" {
			return nil, errors.New("postgres 配置未初始化")
		}
		dsn := fmt.Sprintf(
			"host=%s port=%d dbname=%s user=%s password=%s sslmode=%s",
			cfg.PGHost, cfg.PGPort, cfg.PGDatabase, cfg.PGUser, cfg.PGPassword, cfg.PGSSLMode,
		)
		return sql.Open("pgx", dsn)
	default:
		return nil, errors.New("不支持的数据库类型")
	}
}

func pingDB(ctx context.Context, db *sql.DB) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return db.PingContext(ctx)
}

func runMigrations(ctx context.Context, db *sql.DB, driver DatabaseDriver) error {
	stmts := sqliteSchema
	if driver == DriverPG {
		stmts = postgresSchema
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			if strings.Contains(err.Error(), "duplicate column name") || strings.Contains(err.Error(), "already exists") {
				continue
			}
			return err
		}
	}
	return nil
}

func loadUsers(ctx context.Context, db *sql.DB, driver DatabaseDriver) ([]User, error) {
	rows, err := db.QueryContext(ctx, "SELECT id, username, name, email, role FROM users ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var user User
		if err := rows.Scan(&user.ID, &user.Username, &user.Name, &user.Email, &user.Role); err != nil {
			return nil, err
		}
		user.Channels, err = loadChannels(ctx, db, driver, user.ID)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

func loadChannels(ctx context.Context, db *sql.DB, driver DatabaseDriver, userID int64) ([]NotificationChannel, error) {
	query := "SELECT id, user_id, type, name, target, enabled, last_checked FROM notification_channels WHERE user_id = ? ORDER BY id"
	if driver == DriverPG {
		query = "SELECT id, user_id, type, name, target, enabled, last_checked FROM notification_channels WHERE user_id = $1 ORDER BY id"
	}
	rows, err := db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var channels []NotificationChannel
	for rows.Next() {
		var channel NotificationChannel
		var enabled int
		var checkedAt string
		if err := rows.Scan(&channel.ID, &channel.UserID, &channel.Type, &channel.Name, &channel.Target, &enabled, &checkedAt); err != nil {
			return nil, err
		}
		channel.Enabled = enabled == 1
		channel.LastChecked, _ = time.Parse(time.RFC3339, checkedAt)
		channels = append(channels, channel)
	}
	return channels, rows.Err()
}

func loadEvents(ctx context.Context, db *sql.DB, driver DatabaseDriver) ([]MemoEvent, error) {
	rows, err := db.QueryContext(ctx, "SELECT id, user_id, title, content, event_at, reminder_enabled, bound_channel_ids, bound_group_ids, countdown_label, status, created_at, updated_at FROM memo_events ORDER BY event_at")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []MemoEvent
	for rows.Next() {
		var event MemoEvent
		var enabled int
		var eventAt, createdAt, updatedAt, boundIDs, boundGroupIDs string
		if err := rows.Scan(&event.ID, &event.UserID, &event.Title, &event.Content, &eventAt, &enabled, &boundIDs, &boundGroupIDs, &event.CountdownLabel, &event.Status, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		event.ReminderEnabled = enabled == 1
		event.EventAt, _ = time.Parse(time.RFC3339, eventAt)
		event.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		event.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		event.BoundChannelIDs = decodeInt64List(boundIDs)
		event.BoundGroupIDs = decodeInt64List(boundGroupIDs)
		event.ReminderPoints, err = loadReminderPoints(ctx, db, driver, event.ID)
		if err != nil {
			return nil, err
		}
		event.CountdownLabel = buildCountdownLabel(event.EventAt, time.Now())
		event.UpcomingNotifyPlan = buildNotifyPlan(event, db, driver)
		events = append(events, event)
	}
	return events, rows.Err()
}

func loadReminderPoints(ctx context.Context, db *sql.DB, driver DatabaseDriver, eventID int64) ([]ReminderPoint, error) {
	query := "SELECT id, label, offset_min FROM reminder_points WHERE event_id = ? ORDER BY offset_min DESC, id"
	if driver == DriverPG {
		query = "SELECT id, label, offset_min FROM reminder_points WHERE event_id = $1 ORDER BY offset_min DESC, id"
	}
	rows, err := db.QueryContext(ctx, query, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var points []ReminderPoint
	for rows.Next() {
		var point ReminderPoint
		if err := rows.Scan(&point.ID, &point.Label, &point.OffsetMin); err != nil {
			return nil, err
		}
		points = append(points, point)
	}
	return points, rows.Err()
}

func loadLogs(ctx context.Context, db *sql.DB) ([]NotifyLog, error) {
	rows, err := db.QueryContext(ctx, "SELECT id, event_id, reminder_id, channel_type, channel_name, status, message, triggered_at FROM notification_logs ORDER BY triggered_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []NotifyLog
	for rows.Next() {
		var log NotifyLog
		var triggeredAt string
		if err := rows.Scan(&log.ID, &log.EventID, &log.ReminderID, &log.ChannelType, &log.ChannelName, &log.Status, &log.Message, &triggeredAt); err != nil {
			return nil, err
		}
		log.TriggeredAt, _ = time.Parse(time.RFC3339, triggeredAt)
		logs = append(logs, log)
	}
	return logs, rows.Err()
}

func loadTasks(ctx context.Context, db *sql.DB) ([]ReminderTask, error) {
	rows, err := db.QueryContext(ctx, "SELECT id, event_id, reminder_id, channel_id, channel_type, status, scheduled_at, triggered_at, last_error FROM reminder_tasks ORDER BY scheduled_at DESC, id DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []ReminderTask
	for rows.Next() {
		var task ReminderTask
		var scheduledAt, triggeredAt string
		if err := rows.Scan(&task.ID, &task.EventID, &task.ReminderID, &task.ChannelID, &task.ChannelType, &task.Status, &scheduledAt, &triggeredAt, &task.LastError); err != nil {
			return nil, err
		}
		task.ScheduledAt, _ = time.Parse(time.RFC3339, scheduledAt)
		if triggeredAt != "" {
			task.TriggeredAt, _ = time.Parse(time.RFC3339, triggeredAt)
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func hasAdmin(ctx context.Context, db *sql.DB) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE role = 'admin'").Scan(&count)
	return count > 0, err
}

func (r *Repository) HasAdmin(ctx context.Context) (bool, error) {
	db, err := r.openDB()
	if err != nil {
		return false, err
	}
	defer db.Close()
	return hasAdmin(ctx, db)
}

func (r *Repository) SetupAdmin(ctx context.Context, req SetupAdminRequest) (User, error) {
	db, err := r.openDB()
	if err != nil {
		return User{}, err
	}
	defer db.Close()

	ok, err := hasAdmin(ctx, db)
	if err != nil {
		return User{}, err
	}
	if ok {
		return User{}, errors.New("管理员已存在")
	}
	return r.insertUser(ctx, db, req.Username, req.Password, req.Email, firstNonEmpty(req.Name, req.Username), "admin")
}

func (r *Repository) Register(ctx context.Context, req RegisterRequest) (User, error) {
	db, err := r.openDB()
	if err != nil {
		return User{}, err
	}
	defer db.Close()

	ok, err := hasAdmin(ctx, db)
	if err != nil {
		return User{}, err
	}
	if !ok {
		return User{}, errors.New("请先创建管理员账号")
	}
	return r.insertUser(ctx, db, req.Username, req.Password, req.Email, firstNonEmpty(req.Name, req.Username), "user")
}

func (r *Repository) insertUser(ctx context.Context, db *sql.DB, username, password, email, name, role string) (User, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return User{}, errors.New("用户名不能为空")
	}
	if strings.TrimSpace(password) == "" {
		return User{}, errors.New("密码不能为空")
	}
	exists, err := userExists(ctx, db, username)
	if err != nil {
		return User{}, err
	}
	if exists {
		return User{}, errors.New("用户名已存在")
	}

	hash, err := hashPassword(password)
	if err != nil {
		return User{}, err
	}
	now := time.Now().Format(time.RFC3339)
	res, err := execWithDriver(ctx, db, r.cfg.Database.SelectedDriver,
		`INSERT INTO users (username, name, email, role, password_hash, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		`INSERT INTO users (username, name, email, role, password_hash, created_at) VALUES ($1, $2, $3, $4, $5, $6)`,
		username, name, email, role, hash, now,
	)
	if err != nil {
		return User{}, err
	}

	userID, _ := res.LastInsertId()
	if userID == 0 {
		_ = db.QueryRowContext(ctx, "SELECT id FROM users WHERE username = ?", username).Scan(&userID)
		if r.cfg.Database.SelectedDriver == DriverPG {
			_ = db.QueryRowContext(ctx, "SELECT id FROM users WHERE username = $1", username).Scan(&userID)
		}
	}

	if err := createDefaultChannels(ctx, db, r.cfg.Database.SelectedDriver, userID, email, r.cfg.DingTalk); err != nil {
		return User{}, err
	}
	if err := createDefaultNotifyGroup(ctx, db, r.cfg.Database.SelectedDriver, userID, email); err != nil {
		return User{}, err
	}
	return r.FindUserByID(ctx, userID)
}

func userExists(ctx context.Context, db *sql.DB, username string) (bool, error) {
	var count int
	query := "SELECT COUNT(*) FROM users WHERE username = ?"
	args := []any{username}
	if strings.Contains(fmt.Sprintf("%T", db.Driver()), "pgx") {
		query = "SELECT COUNT(*) FROM users WHERE username = $1"
	}
	err := db.QueryRowContext(ctx, query, args...).Scan(&count)
	return count > 0, err
}

func createDefaultChannels(ctx context.Context, db *sql.DB, driver DatabaseDriver, userID int64, email string, ding DingTalkConfig) error {
	entries := []struct {
		Type    string
		Name    string
		Target  string
		Enabled bool
	}{
		{Type: "email", Name: "工作邮箱", Target: email, Enabled: strings.TrimSpace(email) != ""},
		{Type: "dingtalk", Name: "项目群机器人", Target: "", Enabled: false},
	}
	for _, item := range entries {
		if _, err := execWithDriver(ctx, db, driver,
			`INSERT INTO notification_channels (user_id, type, name, target, enabled, last_checked) VALUES (?, ?, ?, ?, ?, ?)`,
			`INSERT INTO notification_channels (user_id, type, name, target, enabled, last_checked) VALUES ($1, $2, $3, $4, $5, $6)`,
			userID, item.Type, item.Name, item.Target, boolToInt(item.Enabled), time.Now().Format(time.RFC3339),
		); err != nil {
			return err
		}
	}
	return nil
}

func createDefaultNotifyGroup(ctx context.Context, db *sql.DB, driver DatabaseDriver, userID int64, email string) error {
	groupName := "默认通知组"
	res, err := execWithDriver(ctx, db, driver,
		`INSERT INTO notification_groups (user_id, name, enabled) VALUES (?, ?, 1)`,
		`INSERT INTO notification_groups (user_id, name, enabled) VALUES ($1, $2, 1)`,
		userID, groupName,
	)
	if err != nil {
		return err
	}
	groupID, _ := res.LastInsertId()
	if groupID == 0 {
		query := "SELECT id FROM notification_groups WHERE user_id = ? AND name = ? ORDER BY id DESC LIMIT 1"
		args := []any{userID, groupName}
		if driver == DriverPG {
			query = "SELECT id FROM notification_groups WHERE user_id = $1 AND name = $2 ORDER BY id DESC LIMIT 1"
		}
		if err := db.QueryRowContext(ctx, query, args...).Scan(&groupID); err != nil {
			return err
		}
	}
	if strings.TrimSpace(email) != "" {
		if _, err := execWithDriver(ctx, db, driver,
			`INSERT INTO notification_group_members (group_id, type, label, target, secret, use_sign, enabled) VALUES (?, 'email', '默认邮箱', ?, '', 0, 1)`,
			`INSERT INTO notification_group_members (group_id, type, label, target, secret, use_sign, enabled) VALUES ($1, 'email', '默认邮箱', $2, '', 0, 1)`,
			groupID, email,
		); err != nil {
			return err
		}
	}
	return nil
}

func filterUsersByID(users []User, userID int64) []User {
	for _, user := range users {
		if user.ID == userID {
			return []User{user}
		}
	}
	return nil
}

func filterEventsByUser(events []MemoEvent, userID int64) []MemoEvent {
	var filtered []MemoEvent
	for _, event := range events {
		if event.UserID == userID {
			filtered = append(filtered, event)
		}
	}
	return filtered
}

func filterTasksByEvents(tasks []ReminderTask, events []MemoEvent) []ReminderTask {
	eventIDs := make(map[int64]struct{}, len(events))
	for _, event := range events {
		eventIDs[event.ID] = struct{}{}
	}
	var filtered []ReminderTask
	for _, task := range tasks {
		if _, ok := eventIDs[task.EventID]; ok {
			filtered = append(filtered, task)
		}
	}
	return filtered
}

func filterLogsByEvents(logs []NotifyLog, events []MemoEvent) []NotifyLog {
	eventIDs := make(map[int64]struct{}, len(events))
	for _, event := range events {
		eventIDs[event.ID] = struct{}{}
	}
	var filtered []NotifyLog
	for _, log := range logs {
		if _, ok := eventIDs[log.EventID]; ok {
			filtered = append(filtered, log)
		}
	}
	return filtered
}

func (r *Repository) Authenticate(ctx context.Context, username, password string) (User, error) {
	db, err := r.openDB()
	if err != nil {
		return User{}, err
	}
	defer db.Close()

	query := "SELECT id, username, name, email, role, password_hash FROM users WHERE username = ?"
	args := []any{username}
	if r.cfg.Database.SelectedDriver == DriverPG {
		query = "SELECT id, username, name, email, role, password_hash FROM users WHERE username = $1"
	}
	var user User
	var hash string
	if err := db.QueryRowContext(ctx, query, args...).Scan(&user.ID, &user.Username, &user.Name, &user.Email, &user.Role, &hash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, errors.New("用户名或密码错误")
		}
		return User{}, err
	}
	if !verifyPassword(hash, password) {
		return User{}, errors.New("用户名或密码错误")
	}
	user.Channels, err = loadChannels(ctx, db, r.cfg.Database.SelectedDriver, user.ID)
	if err != nil {
		return User{}, err
	}
	return user, nil
}

func (r *Repository) FindUserByID(ctx context.Context, id int64) (User, error) {
	db, err := r.openDB()
	if err != nil {
		return User{}, err
	}
	defer db.Close()
	query := "SELECT id, username, name, email, role FROM users WHERE id = ?"
	args := []any{id}
	if r.cfg.Database.SelectedDriver == DriverPG {
		query = "SELECT id, username, name, email, role FROM users WHERE id = $1"
	}
	var user User
	if err := db.QueryRowContext(ctx, query, args...).Scan(&user.ID, &user.Username, &user.Name, &user.Email, &user.Role); err != nil {
		return User{}, err
	}
	user.Channels, err = loadChannels(ctx, db, r.cfg.Database.SelectedDriver, user.ID)
	if err != nil {
		return User{}, err
	}
	return user, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func buildNotifyPlan(event MemoEvent, db *sql.DB, driver DatabaseDriver) []UpcomingNotifyPoint {
	var plans []UpcomingNotifyPoint
	groupNames := map[int64]string{}
	groups, err := loadNotificationGroups(context.Background(), db, driver, event.UserID)
	if err == nil {
		for _, group := range groups {
			groupNames[group.ID] = group.Name
		}
	}
	var picked []string
	for _, id := range event.BoundGroupIDs {
		if name, ok := groupNames[id]; ok {
			picked = append(picked, name)
		}
	}
	summary := strings.Join(picked, ", ")
	if summary == "" {
		summary = "未选择通知组"
	}
	for _, point := range event.ReminderPoints {
		plans = append(plans, UpcomingNotifyPoint{
			Label:          point.Label,
			NotifyAt:       event.EventAt.Add(-time.Duration(point.OffsetMin) * time.Minute),
			ChannelSummary: summary,
		})
	}
	return plans
}

func loadNotificationGroups(ctx context.Context, db *sql.DB, driver DatabaseDriver, userID int64) ([]NotificationGroup, error) {
	if userID <= 0 {
		return nil, nil
	}
	query := `SELECT id, user_id, name, enabled FROM notification_groups WHERE user_id = ? ORDER BY id`
	if driver == DriverPG {
		query = `SELECT id, user_id, name, enabled FROM notification_groups WHERE user_id = $1 ORDER BY id`
	}
	rows, err := db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []NotificationGroup
	for rows.Next() {
		var item NotificationGroup
		var enabled int
		if err := rows.Scan(&item.ID, &item.UserID, &item.Name, &enabled); err != nil {
			return nil, err
		}
		item.Enabled = enabled == 1
		item.Members, err = loadNotificationGroupMembers(ctx, db, driver, item.ID)
		if err != nil {
			return nil, err
		}
		groups = append(groups, item)
	}
	return groups, rows.Err()
}

func loadNotificationGroupMembers(ctx context.Context, db *sql.DB, driver DatabaseDriver, groupID int64) ([]NotificationGroupMember, error) {
	query := `SELECT id, group_id, type, label, target, secret, keyword, use_sign, enabled FROM notification_group_members WHERE group_id = ? ORDER BY id`
	if driver == DriverPG {
		query = `SELECT id, group_id, type, label, target, secret, keyword, use_sign, enabled FROM notification_group_members WHERE group_id = $1 ORDER BY id`
	}
	rows, err := db.QueryContext(ctx, query, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []NotificationGroupMember
	for rows.Next() {
		var item NotificationGroupMember
		var useSign, enabled int
		if err := rows.Scan(&item.ID, &item.GroupID, &item.Type, &item.Label, &item.Target, &item.Secret, &item.Keyword, &useSign, &enabled); err != nil {
			return nil, err
		}
		item.UseSign = useSign == 1
		item.Enabled = enabled == 1
		item.Secret = ""
		members = append(members, item)
	}
	return members, rows.Err()
}

func execWithDriver(ctx context.Context, execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, driver DatabaseDriver, sqliteQuery, pgQuery string, args ...any) (sql.Result, error) {
	if driver == DriverPG {
		return execer.ExecContext(ctx, pgQuery, args...)
	}
	return execer.ExecContext(ctx, sqliteQuery, args...)
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func encodeInt64List(ids []int64) string {
	data, _ := json.Marshal(ids)
	return string(data)
}

func decodeInt64List(raw string) []int64 {
	if raw == "" {
		return nil
	}
	var ids []int64
	_ = json.Unmarshal([]byte(raw), &ids)
	return ids
}

func normalizeReminderPoints(points []ReminderPoint) []ReminderPoint {
	if len(points) == 0 {
		return []ReminderPoint{{Label: "到点提醒", OffsetMin: 0}}
	}
	seen := map[int]struct{}{}
	var normalized []ReminderPoint
	for _, point := range points {
		if _, ok := seen[point.OffsetMin]; ok {
			continue
		}
		seen[point.OffsetMin] = struct{}{}
		normalized = append(normalized, point)
	}
	if _, ok := seen[0]; !ok {
		normalized = append(normalized, ReminderPoint{Label: "到点提醒", OffsetMin: 0})
	}
	return normalized
}

func buildCountdownLabel(eventAt, now time.Time) string {
	if eventAt.Before(now) {
		return "已过期"
	}
	diff := eventAt.Sub(now)
	days := int(diff.Hours()) / 24
	hours := int(diff.Hours()) % 24
	minutes := int(diff.Minutes()) % 60
	if days > 0 {
		return fmt.Sprintf("%d天 %d小时后开始", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("%d小时 %d分钟后开始", hours, minutes)
	}
	return fmt.Sprintf("%d分钟后开始", minutes)
}

var sqliteSchema = []string{
	`CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT NOT NULL UNIQUE DEFAULT '',
		name TEXT NOT NULL,
		email TEXT NOT NULL DEFAULT '',
		role TEXT NOT NULL DEFAULT 'user',
		password_hash TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT ''
	)`,
	`ALTER TABLE users ADD COLUMN username TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'user'`,
	`ALTER TABLE users ADD COLUMN password_hash TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE users ADD COLUMN created_at TEXT NOT NULL DEFAULT ''`,
	`CREATE TABLE IF NOT EXISTS notification_channels (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		type TEXT NOT NULL,
		name TEXT NOT NULL,
		target TEXT NOT NULL,
		enabled INTEGER NOT NULL DEFAULT 1,
		last_checked TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS memo_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		title TEXT NOT NULL,
		content TEXT NOT NULL,
		event_at TEXT NOT NULL,
		reminder_enabled INTEGER NOT NULL DEFAULT 1,
		bound_channel_ids TEXT NOT NULL DEFAULT '[]',
		bound_group_ids TEXT NOT NULL DEFAULT '[]',
		countdown_label TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'scheduled',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`,
	`ALTER TABLE memo_events ADD COLUMN bound_group_ids TEXT NOT NULL DEFAULT '[]'`,
	`CREATE TABLE IF NOT EXISTS reminder_points (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		event_id INTEGER NOT NULL,
		label TEXT NOT NULL,
		offset_min INTEGER NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS notification_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		event_id INTEGER NOT NULL,
		reminder_id INTEGER NOT NULL DEFAULT 0,
		channel_type TEXT NOT NULL,
		channel_name TEXT NOT NULL,
		status TEXT NOT NULL,
		message TEXT NOT NULL,
		triggered_at TEXT NOT NULL
	)`,
	`ALTER TABLE notification_logs ADD COLUMN reminder_id INTEGER NOT NULL DEFAULT 0`,
	`CREATE TABLE IF NOT EXISTS reminder_tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		event_id INTEGER NOT NULL,
		reminder_id INTEGER NOT NULL,
		channel_id INTEGER NOT NULL,
		channel_type TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending',
		scheduled_at TEXT NOT NULL,
		triggered_at TEXT NOT NULL DEFAULT '',
		last_error TEXT NOT NULL DEFAULT '',
		UNIQUE(event_id, reminder_id, channel_id)
	)`,
	`CREATE TABLE IF NOT EXISTS user_mail_settings (
		user_id INTEGER PRIMARY KEY,
		enabled INTEGER NOT NULL DEFAULT 0,
		host TEXT NOT NULL DEFAULT '',
		port INTEGER NOT NULL DEFAULT 587,
		username TEXT NOT NULL DEFAULT '',
		password TEXT NOT NULL DEFAULT '',
		from_name TEXT NOT NULL DEFAULT '',
		from_address TEXT NOT NULL DEFAULT '',
		use_tls INTEGER NOT NULL DEFAULT 1,
		use_ssl INTEGER NOT NULL DEFAULT 0,
		initialized TEXT NOT NULL DEFAULT ''
	)`,
	`ALTER TABLE user_mail_settings ADD COLUMN use_ssl INTEGER NOT NULL DEFAULT 0`,
	`CREATE TABLE IF NOT EXISTS user_dingtalk_settings (
		user_id INTEGER PRIMARY KEY,
		enabled INTEGER NOT NULL DEFAULT 0,
		webhook TEXT NOT NULL DEFAULT '',
		secret TEXT NOT NULL DEFAULT '',
		use_sign INTEGER NOT NULL DEFAULT 0,
		keyword TEXT NOT NULL DEFAULT '',
		initialized TEXT NOT NULL DEFAULT ''
	)`,
	`CREATE TABLE IF NOT EXISTS notification_groups (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		enabled INTEGER NOT NULL DEFAULT 1
	)`,
	`CREATE TABLE IF NOT EXISTS notification_group_members (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		group_id INTEGER NOT NULL,
		type TEXT NOT NULL,
		label TEXT NOT NULL,
		target TEXT NOT NULL,
		secret TEXT NOT NULL DEFAULT '',
		keyword TEXT NOT NULL DEFAULT '',
		use_sign INTEGER NOT NULL DEFAULT 0,
		enabled INTEGER NOT NULL DEFAULT 1
	)`,
	`ALTER TABLE notification_group_members ADD COLUMN keyword TEXT NOT NULL DEFAULT ''`,
}

var postgresSchema = []string{
	`CREATE TABLE IF NOT EXISTS users (
		id BIGSERIAL PRIMARY KEY,
		username TEXT NOT NULL UNIQUE,
		name TEXT NOT NULL,
		email TEXT NOT NULL DEFAULT '',
		role TEXT NOT NULL DEFAULT 'user',
		password_hash TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT ''
	)`,
	`ALTER TABLE users ADD COLUMN IF NOT EXISTS username TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE users ADD COLUMN IF NOT EXISTS role TEXT NOT NULL DEFAULT 'user'`,
	`ALTER TABLE users ADD COLUMN IF NOT EXISTS password_hash TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE users ADD COLUMN IF NOT EXISTS created_at TEXT NOT NULL DEFAULT ''`,
	`CREATE TABLE IF NOT EXISTS notification_channels (
		id BIGSERIAL PRIMARY KEY,
		user_id BIGINT NOT NULL,
		type TEXT NOT NULL,
		name TEXT NOT NULL,
		target TEXT NOT NULL,
		enabled INTEGER NOT NULL DEFAULT 1,
		last_checked TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS memo_events (
		id BIGSERIAL PRIMARY KEY,
		user_id BIGINT NOT NULL,
		title TEXT NOT NULL,
		content TEXT NOT NULL,
		event_at TEXT NOT NULL,
		reminder_enabled INTEGER NOT NULL DEFAULT 1,
		bound_channel_ids TEXT NOT NULL DEFAULT '[]',
		bound_group_ids TEXT NOT NULL DEFAULT '[]',
		countdown_label TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'scheduled',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`,
	`ALTER TABLE memo_events ADD COLUMN IF NOT EXISTS bound_group_ids TEXT NOT NULL DEFAULT '[]'`,
	`CREATE TABLE IF NOT EXISTS reminder_points (
		id BIGSERIAL PRIMARY KEY,
		event_id BIGINT NOT NULL,
		label TEXT NOT NULL,
		offset_min INTEGER NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS notification_logs (
		id BIGSERIAL PRIMARY KEY,
		event_id BIGINT NOT NULL,
		reminder_id BIGINT NOT NULL DEFAULT 0,
		channel_type TEXT NOT NULL,
		channel_name TEXT NOT NULL,
		status TEXT NOT NULL,
		message TEXT NOT NULL,
		triggered_at TEXT NOT NULL
	)`,
	`ALTER TABLE notification_logs ADD COLUMN IF NOT EXISTS reminder_id BIGINT NOT NULL DEFAULT 0`,
	`CREATE TABLE IF NOT EXISTS reminder_tasks (
		id BIGSERIAL PRIMARY KEY,
		event_id BIGINT NOT NULL,
		reminder_id BIGINT NOT NULL,
		channel_id BIGINT NOT NULL,
		channel_type TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending',
		scheduled_at TEXT NOT NULL,
		triggered_at TEXT NOT NULL DEFAULT '',
		last_error TEXT NOT NULL DEFAULT '',
		UNIQUE(event_id, reminder_id, channel_id)
	)`,
	`CREATE TABLE IF NOT EXISTS user_mail_settings (
		user_id BIGINT PRIMARY KEY,
		enabled INTEGER NOT NULL DEFAULT 0,
		host TEXT NOT NULL DEFAULT '',
		port INTEGER NOT NULL DEFAULT 587,
		username TEXT NOT NULL DEFAULT '',
		password TEXT NOT NULL DEFAULT '',
		from_name TEXT NOT NULL DEFAULT '',
		from_address TEXT NOT NULL DEFAULT '',
		use_tls INTEGER NOT NULL DEFAULT 1,
		use_ssl INTEGER NOT NULL DEFAULT 0,
		initialized TEXT NOT NULL DEFAULT ''
	)`,
	`ALTER TABLE user_mail_settings ADD COLUMN IF NOT EXISTS use_ssl INTEGER NOT NULL DEFAULT 0`,
	`CREATE TABLE IF NOT EXISTS user_dingtalk_settings (
		user_id BIGINT PRIMARY KEY,
		enabled INTEGER NOT NULL DEFAULT 0,
		webhook TEXT NOT NULL DEFAULT '',
		secret TEXT NOT NULL DEFAULT '',
		use_sign INTEGER NOT NULL DEFAULT 0,
		keyword TEXT NOT NULL DEFAULT '',
		initialized TEXT NOT NULL DEFAULT ''
	)`,
	`CREATE TABLE IF NOT EXISTS notification_groups (
		id BIGSERIAL PRIMARY KEY,
		user_id BIGINT NOT NULL,
		name TEXT NOT NULL,
		enabled INTEGER NOT NULL DEFAULT 1
	)`,
	`CREATE TABLE IF NOT EXISTS notification_group_members (
		id BIGSERIAL PRIMARY KEY,
		group_id BIGINT NOT NULL,
		type TEXT NOT NULL,
		label TEXT NOT NULL,
		target TEXT NOT NULL,
		secret TEXT NOT NULL DEFAULT '',
		keyword TEXT NOT NULL DEFAULT '',
		use_sign INTEGER NOT NULL DEFAULT 0,
		enabled INTEGER NOT NULL DEFAULT 1
	)`,
	`ALTER TABLE notification_group_members ADD COLUMN IF NOT EXISTS keyword TEXT NOT NULL DEFAULT ''`,
}
