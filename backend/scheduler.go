package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (r *Repository) SaveMailConfig(ctx context.Context, userID int64, req SaveMailConfigRequest) (MailConfig, error) {
	db, err := r.openDB()
	if err != nil {
		return MailConfig{}, err
	}
	defer db.Close()

	existing, err := loadUserMailConfig(ctx, db, r.cfg.Database.SelectedDriver, userID)
	if err != nil {
		return MailConfig{}, err
	}

	password := strings.TrimSpace(req.Password)
	if password == "" {
		password = existing.Password
	}

	cfg := MailConfig{
		Enabled:     req.Enabled,
		Host:        req.Host,
		Port:        fallbackInt(req.Port, 587),
		Username:    req.Username,
		Password:    password,
		FromName:    req.FromName,
		FromAddress: req.FromAddress,
		UseTLS:      req.UseTLS,
		UseSSL:      req.UseSSL,
		Initialized: time.Now(),
	}
	if cfg.Host == "" && cfg.Enabled {
		return MailConfig{}, errors.New("SMTP 主机不能为空")
	}
	if cfg.FromAddress == "" && cfg.Enabled {
		return MailConfig{}, errors.New("发件人邮箱不能为空")
	}
	if err := saveUserMailConfig(ctx, db, r.cfg.Database.SelectedDriver, userID, cfg); err != nil {
		return MailConfig{}, err
	}
	if err := syncChannelConfig(ctx, db, r.cfg.Database.SelectedDriver, userID, "email", firstNonEmpty(cfg.FromAddress, req.Username), cfg.Enabled); err != nil {
		return MailConfig{}, err
	}
	return cfg.Safe(), nil
}

func (r *Repository) SendTestMail(ctx context.Context, userID int64, to string) error {
	if strings.TrimSpace(to) == "" {
		return errors.New("测试邮箱不能为空")
	}
	db, err := r.openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	cfg, err := loadUserMailConfig(ctx, db, r.cfg.Database.SelectedDriver, userID)
	if err != nil {
		return err
	}
	mailer := r.mailerFactory(cfg)
	subject := "WaitWhat SMTP 测试邮件"
	body := strings.Join([]string{
		"这是一封测试邮件。",
		"",
		"如果你收到了这封邮件，说明当前 SMTP 配置可以正常发送。",
		"发送时间: " + time.Now().Format("2006-01-02 15:04:05"),
	}, "\n")
	return mailer.Send(to, subject, body)
}

func (r *Repository) SaveDingTalkConfig(ctx context.Context, userID int64, req SaveDingTalkConfigRequest) (DingTalkConfig, error) {
	cfg := DingTalkConfig{
		Enabled:     req.Enabled,
		Webhook:     req.Webhook,
		Secret:      req.Secret,
		UseSign:     req.UseSign,
		Keyword:     req.Keyword,
		Initialized: time.Now(),
	}
	if cfg.Enabled && cfg.Webhook == "" {
		return DingTalkConfig{}, errors.New("钉钉 webhook 不能为空")
	}
	if cfg.Enabled && cfg.UseSign && cfg.Secret == "" {
		return DingTalkConfig{}, errors.New("启用加签时必须填写 secret")
	}
	db, err := r.openDB()
	if err != nil {
		return DingTalkConfig{}, err
	}
	defer db.Close()
	if err := saveUserDingTalkConfig(ctx, db, r.cfg.Database.SelectedDriver, userID, cfg); err != nil {
		return DingTalkConfig{}, err
	}
	if err := syncChannelConfig(ctx, db, r.cfg.Database.SelectedDriver, userID, "dingtalk", cfg.Webhook, cfg.Enabled); err != nil {
		return DingTalkConfig{}, err
	}
	return cfg.Safe(), nil
}

func (r *Repository) SendTestDingTalkWebhook(ctx context.Context, req SendTestDingTalkRequest) error {
	cfg := DingTalkConfig{
		Enabled: true,
		Webhook: strings.TrimSpace(req.Webhook),
		Secret:  strings.TrimSpace(req.Secret),
		UseSign: req.UseSign,
		Keyword: firstNonEmpty(strings.TrimSpace(req.Keyword), "测试"),
	}
	if cfg.Webhook == "" {
		return errors.New("钉钉 webhook 不能为空")
	}
	if cfg.UseSign && cfg.Secret == "" {
		return errors.New("启用加签时必须填写 secret")
	}
	_ = ctx
	sender := r.dingFactory(cfg)
	return sender.Send("WaitWhat 测试消息", "这是一条测试消息，用于验证通知组中的钉钉渠道配置。")
}

func (r *Repository) DispatchDueReminders(ctx context.Context) (ReminderDispatchResult, error) {
	if r.cfg.Database.SelectedDriver != DriverSQLite {
		return ReminderDispatchResult{}, errors.New("当前仅实现 SQLite 提醒调度验证")
	}

	db, err := r.openDB()
	if err != nil {
		return ReminderDispatchResult{}, err
	}
	defer db.Close()

	if err := r.enqueueDueTasks(ctx, db); err != nil {
		return ReminderDispatchResult{}, err
	}
	return r.runPendingTasks(ctx, db)
}

func (r *Repository) enqueueDueTasks(ctx context.Context, db *sql.DB) error {
	events, err := loadEvents(ctx, db, r.cfg.Database.SelectedDriver)
	if err != nil {
		return err
	}
	users, err := loadUsers(ctx, db, r.cfg.Database.SelectedDriver)
	if err != nil {
		return err
	}
	groupMap := make(map[int64][]NotificationGroup)
	for _, user := range users {
		groups, gErr := loadNotificationGroups(ctx, db, r.cfg.Database.SelectedDriver, user.ID)
		if gErr == nil {
			groupMap[user.ID] = groups
		}
	}
	userMap := make(map[int64]User, len(users))
	for _, user := range users {
		userMap[user.ID] = user
	}

	now := time.Now()
	for _, event := range events {
		if !event.ReminderEnabled {
			continue
		}
		user, ok := userMap[event.UserID]
		if !ok {
			continue
		}
		for _, point := range event.ReminderPoints {
			notifyAt := event.EventAt.Add(-time.Duration(point.OffsetMin) * time.Minute)
			if notifyAt.After(now) {
				continue
			}
			for _, group := range groupMap[user.ID] {
				if !group.Enabled || !containsInt64(event.BoundGroupIDs, group.ID) {
					continue
				}
				for _, member := range group.Members {
					if !member.Enabled {
						continue
					}
					if err := ensureReminderTask(ctx, db, r.cfg.Database.SelectedDriver, event.ID, point.ID, member.ID, member.Type, notifyAt); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func (r *Repository) runPendingTasks(ctx context.Context, db *sql.DB) (ReminderDispatchResult, error) {
	tasks, err := loadPendingTasks(ctx, db)
	if err != nil {
		return ReminderDispatchResult{}, err
	}
	if len(tasks) == 0 {
		return ReminderDispatchResult{}, nil
	}

	events, err := loadEvents(ctx, db, r.cfg.Database.SelectedDriver)
	if err != nil {
		return ReminderDispatchResult{}, err
	}
	users, err := loadUsers(ctx, db, r.cfg.Database.SelectedDriver)
	if err != nil {
		return ReminderDispatchResult{}, err
	}
	groupMap := make(map[int64][]NotificationGroup)
	for _, user := range users {
		groups, gErr := loadNotificationGroups(ctx, db, r.cfg.Database.SelectedDriver, user.ID)
		if gErr == nil {
			groupMap[user.ID] = groups
		}
	}

	eventMap := make(map[int64]MemoEvent, len(events))
	for _, event := range events {
		eventMap[event.ID] = event
	}
	userMap := make(map[int64]User, len(users))
	for _, user := range users {
		userMap[user.ID] = user
	}

	result := ReminderDispatchResult{}
	for _, task := range tasks {
		result.Triggered++
		event, ok := eventMap[task.EventID]
		if !ok {
			result.Skipped++
			_ = markTaskDone(ctx, db, r.cfg.Database.SelectedDriver, task.ID, "skipped", "未找到对应事件")
			continue
		}
		user, ok := userMap[event.UserID]
		if !ok {
			result.Skipped++
			_ = markTaskDone(ctx, db, r.cfg.Database.SelectedDriver, task.ID, "skipped", "未找到事件所属用户")
			continue
		}

		point, ok := findReminderPoint(event.ReminderPoints, task.ReminderID)
		if !ok {
			result.Skipped++
			_ = markTaskDone(ctx, db, r.cfg.Database.SelectedDriver, task.ID, "skipped", "未找到提醒点")
			continue
		}

		member, groupName, ok := findGroupMember(groupMap[user.ID], task.ChannelID)
		if !ok {
			result.Skipped++
			_ = markTaskDone(ctx, db, r.cfg.Database.SelectedDriver, task.ID, "skipped", "未找到通知组成员")
			continue
		}

		logEntry := NotifyLog{
			EventID:     event.ID,
			ReminderID:  point.ID,
			ChannelType: member.Type,
			ChannelName: firstNonEmpty(groupName+"/"+member.Label, member.Label, groupName),
			TriggeredAt: time.Now(),
		}

		var sendErr error
		switch member.Type {
		case "email":
			sendErr = r.sendEventEmail(ctx, db, member.Target, user, event, point)
		case "dingtalk_webhook":
			sendErr = r.sendEventDingTalkWebhook(member, event, point)
		default:
			logEntry.Status = "skipped"
			logEntry.Message = "该渠道暂未接入自动发送"
		}

		if sendErr != nil {
			logEntry.Status = "failed"
			logEntry.Message = sendErr.Error()
			result.Failed++
			if err := markTaskDone(ctx, db, r.cfg.Database.SelectedDriver, task.ID, "failed", sendErr.Error()); err != nil {
				return result, err
			}
		} else if logEntry.Status == "" {
			logEntry.Status = "success"
			if member.Type == "email" {
				logEntry.Message = "提醒邮件发送成功"
			} else if member.Type == "dingtalk_webhook" {
				logEntry.Message = "钉钉机器人发送成功"
			}
			result.Sent++
			if err := markTaskDone(ctx, db, r.cfg.Database.SelectedDriver, task.ID, "sent", ""); err != nil {
				return result, err
			}
		} else {
			result.Skipped++
			if err := markTaskDone(ctx, db, r.cfg.Database.SelectedDriver, task.ID, "skipped", logEntry.Message); err != nil {
				return result, err
			}
		}

		if err := insertNotifyLog(ctx, db, r.cfg.Database.SelectedDriver, logEntry); err != nil {
			return result, err
		}
	}
	return result, nil
}

func (r *Repository) sendEventEmail(ctx context.Context, db *sql.DB, to string, user User, event MemoEvent, point ReminderPoint) error {
	cfg, err := loadUserMailConfig(ctx, db, r.cfg.Database.SelectedDriver, user.ID)
	if err != nil {
		return err
	}
	mailer := r.mailerFactory(cfg)
	subject := fmt.Sprintf("提醒: %s (%s)", event.Title, point.Label)
	body := strings.Join([]string{
		fmt.Sprintf("你好，%s。", user.Name),
		"",
		fmt.Sprintf("事件: %s", event.Title),
		fmt.Sprintf("内容: %s", event.Content),
		fmt.Sprintf("事件时间: %s", event.EventAt.Format("2006-01-02 15:04:05")),
		fmt.Sprintf("当前提醒: %s", point.Label),
		"",
		"这是 WaitWhat 自动发送的提醒邮件。",
	}, "\n")
	return mailer.Send(to, subject, body)
}

func (r *Repository) sendEventDingTalk(ctx context.Context, db *sql.DB, user User, event MemoEvent, point ReminderPoint) error {
	cfg, err := loadUserDingTalkConfig(ctx, db, r.cfg.Database.SelectedDriver, user.ID)
	if err != nil {
		return err
	}
	sender := r.dingFactory(cfg)
	title := fmt.Sprintf("提醒: %s", event.Title)
	body := strings.Join([]string{
		fmt.Sprintf("负责人: %s", user.Name),
		fmt.Sprintf("提醒节点: %s", point.Label),
		fmt.Sprintf("事件时间: %s", event.EventAt.Format("2006-01-02 15:04:05")),
		fmt.Sprintf("内容: %s", event.Content),
	}, "\n\n")
	return sender.Send(title, body)
}

func (r *Repository) sendEventDingTalkWebhook(member NotificationGroupMember, event MemoEvent, point ReminderPoint) error {
	cfg := DingTalkConfig{
		Enabled: true,
		Webhook: member.Target,
		Secret:  member.Secret,
		UseSign: member.UseSign,
		Keyword: firstNonEmpty(strings.TrimSpace(member.Keyword), "提醒"),
	}
	sender := r.dingFactory(cfg)
	title := fmt.Sprintf("提醒: %s", event.Title)
	body := strings.Join([]string{
		fmt.Sprintf("提醒节点: %s", point.Label),
		fmt.Sprintf("事件时间: %s", event.EventAt.Format("2006-01-02 15:04:05")),
		fmt.Sprintf("内容: %s", event.Content),
	}, "\n\n")
	return sender.Send(title, body)
}

func saveUserMailConfig(ctx context.Context, db *sql.DB, driver DatabaseDriver, userID int64, cfg MailConfig) error {
	_, err := execWithDriver(ctx, db, driver,
		`INSERT INTO user_mail_settings (user_id, enabled, host, port, username, password, from_name, from_address, use_tls, use_ssl, initialized)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(user_id) DO UPDATE SET enabled=excluded.enabled, host=excluded.host, port=excluded.port, username=excluded.username, password=excluded.password, from_name=excluded.from_name, from_address=excluded.from_address, use_tls=excluded.use_tls, use_ssl=excluded.use_ssl, initialized=excluded.initialized`,
		`INSERT INTO user_mail_settings (user_id, enabled, host, port, username, password, from_name, from_address, use_tls, use_ssl, initialized)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		 ON CONFLICT (user_id) DO UPDATE SET enabled=EXCLUDED.enabled, host=EXCLUDED.host, port=EXCLUDED.port, username=EXCLUDED.username, password=EXCLUDED.password, from_name=EXCLUDED.from_name, from_address=EXCLUDED.from_address, use_tls=EXCLUDED.use_tls, use_ssl=EXCLUDED.use_ssl, initialized=EXCLUDED.initialized`,
		userID, boolToInt(cfg.Enabled), cfg.Host, cfg.Port, cfg.Username, cfg.Password, cfg.FromName, cfg.FromAddress, boolToInt(cfg.UseTLS), boolToInt(cfg.UseSSL), cfg.Initialized.Format(time.RFC3339),
	)
	return err
}

func loadUserMailConfig(ctx context.Context, db *sql.DB, driver DatabaseDriver, userID int64) (MailConfig, error) {
	query := `SELECT enabled, host, port, username, password, from_name, from_address, use_tls, use_ssl, initialized FROM user_mail_settings WHERE user_id = ?`
	if driver == DriverPG {
		query = `SELECT enabled, host, port, username, password, from_name, from_address, use_tls, use_ssl, initialized FROM user_mail_settings WHERE user_id = $1`
	}
	var cfg MailConfig
	var enabled, useTLS, useSSL int
	var initialized string
	if err := db.QueryRowContext(ctx, query, userID).Scan(&enabled, &cfg.Host, &cfg.Port, &cfg.Username, &cfg.Password, &cfg.FromName, &cfg.FromAddress, &useTLS, &useSSL, &initialized); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return MailConfig{Port: 587}, nil
		}
		return MailConfig{}, err
	}
	cfg.Enabled = enabled == 1
	cfg.UseTLS = useTLS == 1
	cfg.UseSSL = useSSL == 1
	cfg.Initialized, _ = time.Parse(time.RFC3339, initialized)
	return cfg, nil
}

func saveUserDingTalkConfig(ctx context.Context, db *sql.DB, driver DatabaseDriver, userID int64, cfg DingTalkConfig) error {
	_, err := execWithDriver(ctx, db, driver,
		`INSERT INTO user_dingtalk_settings (user_id, enabled, webhook, secret, use_sign, keyword, initialized)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(user_id) DO UPDATE SET enabled=excluded.enabled, webhook=excluded.webhook, secret=excluded.secret, use_sign=excluded.use_sign, keyword=excluded.keyword, initialized=excluded.initialized`,
		`INSERT INTO user_dingtalk_settings (user_id, enabled, webhook, secret, use_sign, keyword, initialized)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)
		 ON CONFLICT (user_id) DO UPDATE SET enabled=EXCLUDED.enabled, webhook=EXCLUDED.webhook, secret=EXCLUDED.secret, use_sign=EXCLUDED.use_sign, keyword=EXCLUDED.keyword, initialized=EXCLUDED.initialized`,
		userID, boolToInt(cfg.Enabled), cfg.Webhook, cfg.Secret, boolToInt(cfg.UseSign), cfg.Keyword, cfg.Initialized.Format(time.RFC3339),
	)
	return err
}

func syncChannelConfig(ctx context.Context, db *sql.DB, driver DatabaseDriver, userID int64, channelType, target string, enabled bool) error {
	_, err := execWithDriver(ctx, db, driver,
		`UPDATE notification_channels SET target = ?, enabled = ?, last_checked = ? WHERE user_id = ? AND type = ?`,
		`UPDATE notification_channels SET target = $1, enabled = $2, last_checked = $3 WHERE user_id = $4 AND type = $5`,
		target, boolToInt(enabled), time.Now().Format(time.RFC3339), userID, channelType,
	)
	return err
}

func loadUserDingTalkConfig(ctx context.Context, db *sql.DB, driver DatabaseDriver, userID int64) (DingTalkConfig, error) {
	query := `SELECT enabled, webhook, secret, use_sign, keyword, initialized FROM user_dingtalk_settings WHERE user_id = ?`
	if driver == DriverPG {
		query = `SELECT enabled, webhook, secret, use_sign, keyword, initialized FROM user_dingtalk_settings WHERE user_id = $1`
	}
	var cfg DingTalkConfig
	var enabled, useSign int
	var initialized string
	if err := db.QueryRowContext(ctx, query, userID).Scan(&enabled, &cfg.Webhook, &cfg.Secret, &useSign, &cfg.Keyword, &initialized); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return DingTalkConfig{}, nil
		}
		return DingTalkConfig{}, err
	}
	cfg.Enabled = enabled == 1
	cfg.UseSign = useSign == 1
	cfg.Initialized, _ = time.Parse(time.RFC3339, initialized)
	return cfg, nil
}

func loadPendingTasks(ctx context.Context, db *sql.DB) ([]ReminderTask, error) {
	rows, err := db.QueryContext(ctx, "SELECT id, event_id, reminder_id, channel_id, channel_type, status, scheduled_at, triggered_at, last_error FROM reminder_tasks WHERE status = 'pending' ORDER BY scheduled_at ASC, id ASC")
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

func ensureReminderTask(ctx context.Context, db *sql.DB, driver DatabaseDriver, eventID, reminderID, channelID int64, channelType string, scheduledAt time.Time) error {
	_, err := execWithDriver(ctx, db, driver,
		`INSERT OR IGNORE INTO reminder_tasks (event_id, reminder_id, channel_id, channel_type, status, scheduled_at, triggered_at, last_error)
		 VALUES (?, ?, ?, ?, 'pending', ?, '', '')`,
		`INSERT INTO reminder_tasks (event_id, reminder_id, channel_id, channel_type, status, scheduled_at, triggered_at, last_error)
		 VALUES ($1, $2, $3, $4, 'pending', $5, '', '')
		 ON CONFLICT (event_id, reminder_id, channel_id) DO NOTHING`,
		eventID, reminderID, channelID, channelType, scheduledAt.Format(time.RFC3339),
	)
	return err
}

func markTaskDone(ctx context.Context, db *sql.DB, driver DatabaseDriver, taskID int64, status, lastError string) error {
	_, err := execWithDriver(ctx, db, driver,
		`UPDATE reminder_tasks SET status = ?, triggered_at = ?, last_error = ? WHERE id = ?`,
		`UPDATE reminder_tasks SET status = $1, triggered_at = $2, last_error = $3 WHERE id = $4`,
		status, time.Now().Format(time.RFC3339), lastError, taskID,
	)
	return err
}

func insertNotifyLog(ctx context.Context, db *sql.DB, driver DatabaseDriver, entry NotifyLog) error {
	_, err := execWithDriver(ctx, db, driver,
		`INSERT INTO notification_logs (event_id, reminder_id, channel_type, channel_name, status, message, triggered_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		`INSERT INTO notification_logs (event_id, reminder_id, channel_type, channel_name, status, message, triggered_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		entry.EventID, entry.ReminderID, entry.ChannelType, entry.ChannelName, entry.Status, entry.Message, entry.TriggeredAt.Format(time.RFC3339),
	)
	return err
}

func fallbackInt(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}

func containsInt64(list []int64, target int64) bool {
	for _, item := range list {
		if item == target {
			return true
		}
	}
	return false
}

func findReminderPoint(points []ReminderPoint, id int64) (ReminderPoint, bool) {
	for _, point := range points {
		if point.ID == id {
			return point, true
		}
	}
	return ReminderPoint{}, false
}

func findChannel(channels []NotificationChannel, id int64) (NotificationChannel, bool) {
	for _, channel := range channels {
		if channel.ID == id {
			return channel, true
		}
	}
	return NotificationChannel{}, false
}

func findGroupMember(groups []NotificationGroup, memberID int64) (NotificationGroupMember, string, bool) {
	for _, group := range groups {
		for _, member := range group.Members {
			if member.ID == memberID {
				return member, group.Name, true
			}
		}
	}
	return NotificationGroupMember{}, "", false
}
