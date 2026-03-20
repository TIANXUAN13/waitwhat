package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

func (r *Repository) SaveMailConfig(ctx context.Context, userID int64, req SaveMailConfigRequest) (MailConfig, error) {
	db, err := r.openDB()
	if err != nil {
		return MailConfig{}, err
	}
	defer db.Close()

	existing, err := loadUserMailConfig(ctx, db, r.cfg.Database.SelectedDriver, r.cfg.Auth.TokenSecret, userID)
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
	if err := saveUserMailConfig(ctx, db, r.cfg.Database.SelectedDriver, r.cfg.Auth.TokenSecret, userID, cfg); err != nil {
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
	cfg, err := loadUserMailConfig(ctx, db, r.cfg.Database.SelectedDriver, r.cfg.Auth.TokenSecret, userID)
	if err != nil {
		return err
	}
	mailer := r.mailerFactory(cfg)
	subject := "WaitWhat SMTP 测试邮件"
	body := strings.Join([]string{
		"这是一封测试邮件。",
		"",
		"如果你收到了这封邮件，说明当前 SMTP 配置可以正常发送。",
		"发送时间: " + formatTestMailTime(),
	}, "\n")
	return mailer.Send(to, subject, body)
}

func formatTestMailTime() string {
	timezone := strings.TrimSpace(os.Getenv("APP_TIMEZONE"))
	if timezone == "" {
		timezone = "Asia/Shanghai"
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.Local
	}
	return time.Now().In(loc).Format("2006-01-02 15:04:05 -07:00 MST")
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
	if err := saveUserDingTalkConfig(ctx, db, r.cfg.Database.SelectedDriver, r.cfg.Auth.TokenSecret, userID, cfg); err != nil {
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
		if isExpiredOneTimeEvent(event, now) {
			continue
		}
		user, ok := userMap[event.UserID]
		if !ok {
			continue
		}
		for _, point := range event.ReminderPoints {
			notifyAt, ok := computeNotifyAt(event, point, now)
			if !ok {
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
	now := time.Now()
	for _, task := range tasks {
		claimed, err := claimTaskForProcessing(ctx, db, r.cfg.Database.SelectedDriver, task.ID)
		if err != nil {
			return result, err
		}
		if !claimed {
			continue
		}
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
		if isExpiredOneTimeEvent(event, now) {
			result.Skipped++
			_ = markTaskDone(ctx, db, r.cfg.Database.SelectedDriver, task.ID, "skipped", "一次性事件已过期，跳过推送")
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
			if task.RetryCount < task.MaxRetries {
				nextAt := time.Now().Add(nextRetryDelay(task.RetryCount + 1))
				logEntry.Status = "retrying"
				logEntry.Message = fmt.Sprintf("发送失败，将重试(%d/%d)：%v", task.RetryCount+1, task.MaxRetries, sendErr)
				result.Retried++
				if err := markTaskRetry(ctx, db, r.cfg.Database.SelectedDriver, task.ID, task.RetryCount+1, nextAt, sendErr.Error()); err != nil {
					return result, err
				}
			} else {
				logEntry.Status = "failed"
				logEntry.Message = sendErr.Error()
				result.Failed++
				if err := markTaskDone(ctx, db, r.cfg.Database.SelectedDriver, task.ID, "failed", sendErr.Error()); err != nil {
					return result, err
				}
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
	cfg, err := loadUserMailConfig(ctx, db, r.cfg.Database.SelectedDriver, r.cfg.Auth.TokenSecret, user.ID)
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
	cfg, err := loadUserDingTalkConfig(ctx, db, r.cfg.Database.SelectedDriver, r.cfg.Auth.TokenSecret, user.ID)
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

func saveUserMailConfig(ctx context.Context, db *sql.DB, driver DatabaseDriver, masterSecret string, userID int64, cfg MailConfig) error {
	encryptedPassword, err := encryptSecret(cfg.Password, masterSecret)
	if err != nil {
		return err
	}
	_, err = execWithDriver(ctx, db, driver,
		`INSERT INTO user_mail_settings (user_id, enabled, host, port, username, password, from_name, from_address, use_tls, use_ssl, initialized)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(user_id) DO UPDATE SET enabled=excluded.enabled, host=excluded.host, port=excluded.port, username=excluded.username, password=excluded.password, from_name=excluded.from_name, from_address=excluded.from_address, use_tls=excluded.use_tls, use_ssl=excluded.use_ssl, initialized=excluded.initialized`,
		`INSERT INTO user_mail_settings (user_id, enabled, host, port, username, password, from_name, from_address, use_tls, use_ssl, initialized)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		 ON CONFLICT (user_id) DO UPDATE SET enabled=EXCLUDED.enabled, host=EXCLUDED.host, port=EXCLUDED.port, username=EXCLUDED.username, password=EXCLUDED.password, from_name=EXCLUDED.from_name, from_address=EXCLUDED.from_address, use_tls=EXCLUDED.use_tls, use_ssl=EXCLUDED.use_ssl, initialized=EXCLUDED.initialized`,
		userID, boolToInt(cfg.Enabled), cfg.Host, cfg.Port, cfg.Username, encryptedPassword, cfg.FromName, cfg.FromAddress, boolToInt(cfg.UseTLS), boolToInt(cfg.UseSSL), cfg.Initialized.Format(time.RFC3339),
	)
	return err
}

func loadUserMailConfig(ctx context.Context, db *sql.DB, driver DatabaseDriver, masterSecret string, userID int64) (MailConfig, error) {
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
	decrypted, err := decryptSecret(cfg.Password, masterSecret)
	if err != nil {
		return MailConfig{}, err
	}
	cfg.Password = decrypted
	cfg.Enabled = enabled == 1
	cfg.UseTLS = useTLS == 1
	cfg.UseSSL = useSSL == 1
	cfg.Initialized, _ = time.Parse(time.RFC3339, initialized)
	return cfg, nil
}

func saveUserDingTalkConfig(ctx context.Context, db *sql.DB, driver DatabaseDriver, masterSecret string, userID int64, cfg DingTalkConfig) error {
	encryptedSecret, err := encryptSecret(cfg.Secret, masterSecret)
	if err != nil {
		return err
	}
	_, err = execWithDriver(ctx, db, driver,
		`INSERT INTO user_dingtalk_settings (user_id, enabled, webhook, secret, use_sign, keyword, initialized)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(user_id) DO UPDATE SET enabled=excluded.enabled, webhook=excluded.webhook, secret=excluded.secret, use_sign=excluded.use_sign, keyword=excluded.keyword, initialized=excluded.initialized`,
		`INSERT INTO user_dingtalk_settings (user_id, enabled, webhook, secret, use_sign, keyword, initialized)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)
		 ON CONFLICT (user_id) DO UPDATE SET enabled=EXCLUDED.enabled, webhook=EXCLUDED.webhook, secret=EXCLUDED.secret, use_sign=EXCLUDED.use_sign, keyword=EXCLUDED.keyword, initialized=EXCLUDED.initialized`,
		userID, boolToInt(cfg.Enabled), cfg.Webhook, encryptedSecret, boolToInt(cfg.UseSign), cfg.Keyword, cfg.Initialized.Format(time.RFC3339),
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

func loadUserDingTalkConfig(ctx context.Context, db *sql.DB, driver DatabaseDriver, masterSecret string, userID int64) (DingTalkConfig, error) {
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
	decrypted, err := decryptSecret(cfg.Secret, masterSecret)
	if err != nil {
		return DingTalkConfig{}, err
	}
	cfg.Secret = decrypted
	cfg.Enabled = enabled == 1
	cfg.UseSign = useSign == 1
	cfg.Initialized, _ = time.Parse(time.RFC3339, initialized)
	return cfg, nil
}

func loadPendingTasks(ctx context.Context, db *sql.DB) ([]ReminderTask, error) {
	now := time.Now().Format(time.RFC3339)
	rows, err := db.QueryContext(ctx, "SELECT id, event_id, reminder_id, channel_id, channel_type, status, scheduled_at, triggered_at, last_error, retry_count, max_retries FROM reminder_tasks WHERE status = 'pending' AND scheduled_at <= ? ORDER BY scheduled_at ASC, id ASC", now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []ReminderTask
	for rows.Next() {
		var task ReminderTask
		var scheduledAt, triggeredAt string
		if err := rows.Scan(&task.ID, &task.EventID, &task.ReminderID, &task.ChannelID, &task.ChannelType, &task.Status, &scheduledAt, &triggeredAt, &task.LastError, &task.RetryCount, &task.MaxRetries); err != nil {
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
	query := `SELECT id, scheduled_at FROM reminder_tasks WHERE event_id = ? AND reminder_id = ? AND channel_id = ?`
	if driver == DriverPG {
		query = `SELECT id, scheduled_at FROM reminder_tasks WHERE event_id = $1 AND reminder_id = $2 AND channel_id = $3`
	}
	var taskID int64
	var existingScheduled string
	err := db.QueryRowContext(ctx, query, eventID, reminderID, channelID).Scan(&taskID, &existingScheduled)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		_, insertErr := execWithDriver(ctx, db, driver,
			`INSERT INTO reminder_tasks (event_id, reminder_id, channel_id, channel_type, status, scheduled_at, triggered_at, last_error, retry_count, max_retries)
			 VALUES (?, ?, ?, ?, 'pending', ?, '', '', 0, 5)`,
			`INSERT INTO reminder_tasks (event_id, reminder_id, channel_id, channel_type, status, scheduled_at, triggered_at, last_error, retry_count, max_retries)
			 VALUES ($1, $2, $3, $4, 'pending', $5, '', '', 0, 5)`,
			eventID, reminderID, channelID, channelType, scheduledAt.Format(time.RFC3339),
		)
		return insertErr
	}
	existingAt, _ := time.Parse(time.RFC3339, existingScheduled)
	if !scheduledAt.After(existingAt) {
		return nil
	}
	_, updateErr := execWithDriver(ctx, db, driver,
		`UPDATE reminder_tasks SET status = 'pending', scheduled_at = ?, triggered_at = '', last_error = '', retry_count = 0, max_retries = 5 WHERE id = ?`,
		`UPDATE reminder_tasks SET status = 'pending', scheduled_at = $1, triggered_at = '', last_error = '', retry_count = 0, max_retries = 5 WHERE id = $2`,
		scheduledAt.Format(time.RFC3339), taskID,
	)
	return updateErr
}

func markTaskDone(ctx context.Context, db *sql.DB, driver DatabaseDriver, taskID int64, status, lastError string) error {
	_, err := execWithDriver(ctx, db, driver,
		`UPDATE reminder_tasks SET status = ?, triggered_at = ?, last_error = ? WHERE id = ?`,
		`UPDATE reminder_tasks SET status = $1, triggered_at = $2, last_error = $3 WHERE id = $4`,
		status, time.Now().Format(time.RFC3339), lastError, taskID,
	)
	return err
}

func claimTaskForProcessing(ctx context.Context, db *sql.DB, driver DatabaseDriver, taskID int64) (bool, error) {
	res, err := execWithDriver(ctx, db, driver,
		`UPDATE reminder_tasks SET status = 'processing' WHERE id = ? AND status = 'pending'`,
		`UPDATE reminder_tasks SET status = 'processing' WHERE id = $1 AND status = 'pending'`,
		taskID,
	)
	if err != nil {
		return false, err
	}
	affected, _ := res.RowsAffected()
	return affected > 0, nil
}

func markTaskRetry(ctx context.Context, db *sql.DB, driver DatabaseDriver, taskID int64, retryCount int, nextAt time.Time, lastError string) error {
	_, err := execWithDriver(ctx, db, driver,
		`UPDATE reminder_tasks SET status = 'pending', retry_count = ?, scheduled_at = ?, triggered_at = ?, last_error = ? WHERE id = ?`,
		`UPDATE reminder_tasks SET status = 'pending', retry_count = $1, scheduled_at = $2, triggered_at = $3, last_error = $4 WHERE id = $5`,
		retryCount, nextAt.Format(time.RFC3339), time.Now().Format(time.RFC3339), lastError, taskID,
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

func nextRetryDelay(retryCount int) time.Duration {
	if retryCount < 1 {
		retryCount = 1
	}
	seconds := 30.0 * math.Pow(2, float64(retryCount-1))
	if seconds > 1800 {
		seconds = 1800
	}
	return time.Duration(seconds) * time.Second
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

func computeNotifyAt(event MemoEvent, point ReminderPoint, now time.Time) (time.Time, bool) {
	offset := time.Duration(point.OffsetMin) * time.Minute
	switch strings.ToLower(event.RecurrenceType) {
	case "", "once":
		notifyAt := event.EventAt.Add(-offset)
		return notifyAt, !notifyAt.After(now)
	case "daily":
		base := latestDailyOccurrence(event.EventAt, now.Add(offset))
		notifyAt := base.Add(-offset)
		return notifyAt, !notifyAt.After(now)
	case "workday":
		base := latestWorkdayOccurrence(event.EventAt, now.Add(offset))
		notifyAt := base.Add(-offset)
		return notifyAt, !notifyAt.After(now)
	case "cron":
		base, ok := latestCronOccurrence(event.RecurrenceExpr, now.Add(offset), 72*time.Hour)
		if !ok {
			return time.Time{}, false
		}
		notifyAt := base.Add(-offset)
		return notifyAt, !notifyAt.After(now)
	default:
		notifyAt := event.EventAt.Add(-offset)
		return notifyAt, !notifyAt.After(now)
	}
}

func isExpiredOneTimeEvent(event MemoEvent, now time.Time) bool {
	recurrence := strings.ToLower(strings.TrimSpace(event.RecurrenceType))
	if recurrence == "" || recurrence == "once" {
		// Allow short scheduling delay (cron tick / queue latency) so "到点提醒" is not dropped.
		const grace = 10 * time.Minute
		return event.EventAt.Add(grace).Before(now)
	}
	return false
}

func latestDailyOccurrence(anchor, now time.Time) time.Time {
	y, m, d := now.Date()
	hh, mm, ss := anchor.Clock()
	candidate := time.Date(y, m, d, hh, mm, ss, 0, now.Location())
	if candidate.After(now) {
		candidate = candidate.AddDate(0, 0, -1)
	}
	return candidate
}

func latestWorkdayOccurrence(anchor, now time.Time) time.Time {
	candidate := latestDailyOccurrence(anchor, now)
	for candidate.Weekday() == time.Saturday || candidate.Weekday() == time.Sunday {
		candidate = candidate.AddDate(0, 0, -1)
	}
	return candidate
}

func latestCronOccurrence(expr string, now time.Time, lookback time.Duration) (time.Time, bool) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return time.Time{}, false
	}
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return time.Time{}, false
	}
	start := now.Add(-lookback).Truncate(time.Minute)
	for t := now.Truncate(time.Minute); !t.Before(start); t = t.Add(-time.Minute) {
		if cronMatch(fields, t) {
			return t, true
		}
	}
	return time.Time{}, false
}

func cronMatch(fields []string, t time.Time) bool {
	return cronFieldMatch(fields[0], t.Minute(), 0, 59) &&
		cronFieldMatch(fields[1], t.Hour(), 0, 23) &&
		cronFieldMatch(fields[2], t.Day(), 1, 31) &&
		cronFieldMatch(fields[3], int(t.Month()), 1, 12) &&
		cronFieldMatch(normalizeCronWeekday(fields[4]), int(t.Weekday()), 0, 6)
}

func normalizeCronWeekday(field string) string {
	return strings.ReplaceAll(field, "7", "0")
}

func cronFieldMatch(pattern string, value, min, max int) bool {
	parts := strings.Split(pattern, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "*" {
			return true
		}
		if strings.HasPrefix(part, "*/") {
			step, err := strconv.Atoi(strings.TrimPrefix(part, "*/"))
			if err == nil && step > 0 && value%step == 0 {
				return true
			}
			continue
		}
		if strings.Contains(part, "-") {
			rangeParts := strings.SplitN(part, "-", 2)
			start, err1 := strconv.Atoi(rangeParts[0])
			end, err2 := strconv.Atoi(rangeParts[1])
			if err1 == nil && err2 == nil && value >= start && value <= end {
				return true
			}
			continue
		}
		num, err := strconv.Atoi(part)
		if err == nil && num >= min && num <= max && value == num {
			return true
		}
	}
	return false
}
