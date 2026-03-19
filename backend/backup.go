package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

func (r *Repository) ExportBackup(ctx context.Context) (BackupPayload, error) {
	db, err := r.openDB()
	if err != nil {
		return BackupPayload{}, err
	}
	defer db.Close()

	payload := BackupPayload{
		Version:        1,
		ExportedAt:     time.Now().Format(time.RFC3339),
		LoginMaxFailed: r.cfg.Auth.LoginLimitMaxFail,
		LoginWindowSec: r.cfg.Auth.LoginLimitWindow,
	}

	if payload.Users, err = exportUsers(ctx, db, r.cfg.Database.SelectedDriver); err != nil {
		return BackupPayload{}, err
	}
	if payload.NotificationChannels, err = exportChannels(ctx, db, r.cfg.Database.SelectedDriver); err != nil {
		return BackupPayload{}, err
	}
	if payload.MemoEvents, err = exportEvents(ctx, db, r.cfg.Database.SelectedDriver); err != nil {
		return BackupPayload{}, err
	}
	if payload.ReminderPoints, err = exportReminderPoints(ctx, db, r.cfg.Database.SelectedDriver); err != nil {
		return BackupPayload{}, err
	}
	if payload.NotificationLogs, err = exportNotificationLogs(ctx, db, r.cfg.Database.SelectedDriver); err != nil {
		return BackupPayload{}, err
	}
	if payload.ReminderTasks, err = exportReminderTasks(ctx, db, r.cfg.Database.SelectedDriver); err != nil {
		return BackupPayload{}, err
	}
	if payload.NotificationGroups, err = exportNotificationGroups(ctx, db, r.cfg.Database.SelectedDriver); err != nil {
		return BackupPayload{}, err
	}
	if payload.NotificationGroupMembers, err = exportNotificationGroupMembers(ctx, db, r.cfg.Database.SelectedDriver); err != nil {
		return BackupPayload{}, err
	}
	if payload.UserMailSettings, err = exportUserMailSettings(ctx, db, r.cfg.Database.SelectedDriver); err != nil {
		return BackupPayload{}, err
	}
	if payload.UserDingTalkSettings, err = exportUserDingTalkSettings(ctx, db, r.cfg.Database.SelectedDriver); err != nil {
		return BackupPayload{}, err
	}
	return payload, nil
}

func (r *Repository) ImportBackup(ctx context.Context, payload BackupPayload) error {
	if payload.Version <= 0 {
		return errors.New("备份版本不正确")
	}
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

	deleteQueries := []struct {
		sqlite string
		pg     string
	}{
		{`DELETE FROM notification_group_members`, `DELETE FROM notification_group_members`},
		{`DELETE FROM notification_groups`, `DELETE FROM notification_groups`},
		{`DELETE FROM notification_logs`, `DELETE FROM notification_logs`},
		{`DELETE FROM reminder_tasks`, `DELETE FROM reminder_tasks`},
		{`DELETE FROM reminder_points`, `DELETE FROM reminder_points`},
		{`DELETE FROM memo_events`, `DELETE FROM memo_events`},
		{`DELETE FROM notification_channels`, `DELETE FROM notification_channels`},
		{`DELETE FROM user_mail_settings`, `DELETE FROM user_mail_settings`},
		{`DELETE FROM user_dingtalk_settings`, `DELETE FROM user_dingtalk_settings`},
		{`DELETE FROM admin_audit_logs`, `DELETE FROM admin_audit_logs`},
		{`DELETE FROM users`, `DELETE FROM users`},
	}
	for _, q := range deleteQueries {
		if _, err := execWithDriver(ctx, tx, r.cfg.Database.SelectedDriver, q.sqlite, q.pg); err != nil {
			return err
		}
	}

	for _, row := range payload.Users {
		if _, err := execWithDriver(ctx, tx, r.cfg.Database.SelectedDriver,
			`INSERT INTO users (id, username, name, email, role, password_hash, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			`INSERT INTO users (id, username, name, email, role, password_hash, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			row.ID, row.Username, row.Name, row.Email, row.Role, row.PasswordHash, row.CreatedAt,
		); err != nil {
			return err
		}
	}
	for _, row := range payload.NotificationChannels {
		if _, err := execWithDriver(ctx, tx, r.cfg.Database.SelectedDriver,
			`INSERT INTO notification_channels (id, user_id, type, name, target, enabled, last_checked) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			`INSERT INTO notification_channels (id, user_id, type, name, target, enabled, last_checked) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			row.ID, row.UserID, row.Type, row.Name, row.Target, row.Enabled, row.LastChecked,
		); err != nil {
			return err
		}
	}
	for _, row := range payload.MemoEvents {
		if _, err := execWithDriver(ctx, tx, r.cfg.Database.SelectedDriver,
			`INSERT INTO memo_events (id, user_id, title, content, event_at, reminder_enabled, recurrence_type, recurrence_expr, bound_channel_ids, bound_group_ids, countdown_label, status, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			`INSERT INTO memo_events (id, user_id, title, content, event_at, reminder_enabled, recurrence_type, recurrence_expr, bound_channel_ids, bound_group_ids, countdown_label, status, created_at, updated_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
			row.ID, row.UserID, row.Title, row.Content, row.EventAt, row.ReminderEnabled, row.RecurrenceType, row.RecurrenceExpr, row.BoundChannelIDs, row.BoundGroupIDs, row.CountdownLabel, row.Status, row.CreatedAt, row.UpdatedAt,
		); err != nil {
			return err
		}
	}
	for _, row := range payload.ReminderPoints {
		if _, err := execWithDriver(ctx, tx, r.cfg.Database.SelectedDriver,
			`INSERT INTO reminder_points (id, event_id, label, offset_min) VALUES (?, ?, ?, ?)`,
			`INSERT INTO reminder_points (id, event_id, label, offset_min) VALUES ($1, $2, $3, $4)`,
			row.ID, row.EventID, row.Label, row.OffsetMin,
		); err != nil {
			return err
		}
	}
	for _, row := range payload.NotificationLogs {
		if _, err := execWithDriver(ctx, tx, r.cfg.Database.SelectedDriver,
			`INSERT INTO notification_logs (id, event_id, reminder_id, channel_type, channel_name, status, message, triggered_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			`INSERT INTO notification_logs (id, event_id, reminder_id, channel_type, channel_name, status, message, triggered_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			row.ID, row.EventID, row.ReminderID, row.ChannelType, row.ChannelName, row.Status, row.Message, row.TriggeredAt,
		); err != nil {
			return err
		}
	}
	for _, row := range payload.ReminderTasks {
		if _, err := execWithDriver(ctx, tx, r.cfg.Database.SelectedDriver,
			`INSERT INTO reminder_tasks (id, event_id, reminder_id, channel_id, channel_type, status, scheduled_at, triggered_at, last_error, retry_count, max_retries)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			`INSERT INTO reminder_tasks (id, event_id, reminder_id, channel_id, channel_type, status, scheduled_at, triggered_at, last_error, retry_count, max_retries)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
			row.ID, row.EventID, row.ReminderID, row.ChannelID, row.ChannelType, row.Status, row.ScheduledAt, row.TriggeredAt, row.LastError, row.RetryCount, row.MaxRetries,
		); err != nil {
			return err
		}
	}
	for _, row := range payload.NotificationGroups {
		if _, err := execWithDriver(ctx, tx, r.cfg.Database.SelectedDriver,
			`INSERT INTO notification_groups (id, user_id, name, enabled) VALUES (?, ?, ?, ?)`,
			`INSERT INTO notification_groups (id, user_id, name, enabled) VALUES ($1, $2, $3, $4)`,
			row.ID, row.UserID, row.Name, row.Enabled,
		); err != nil {
			return err
		}
	}
	for _, row := range payload.NotificationGroupMembers {
		if _, err := execWithDriver(ctx, tx, r.cfg.Database.SelectedDriver,
			`INSERT INTO notification_group_members (id, group_id, type, label, target, secret, keyword, use_sign, enabled) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			`INSERT INTO notification_group_members (id, group_id, type, label, target, secret, keyword, use_sign, enabled) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			row.ID, row.GroupID, row.Type, row.Label, row.Target, row.Secret, row.Keyword, row.UseSign, row.Enabled,
		); err != nil {
			return err
		}
	}
	for _, row := range payload.UserMailSettings {
		if _, err := execWithDriver(ctx, tx, r.cfg.Database.SelectedDriver,
			`INSERT INTO user_mail_settings (user_id, enabled, host, port, username, password, from_name, from_address, use_tls, use_ssl, initialized) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			`INSERT INTO user_mail_settings (user_id, enabled, host, port, username, password, from_name, from_address, use_tls, use_ssl, initialized) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
			row.UserID, row.Enabled, row.Host, row.Port, row.Username, row.Password, row.FromName, row.FromAddress, row.UseTLS, row.UseSSL, row.Initialized,
		); err != nil {
			return err
		}
	}
	for _, row := range payload.UserDingTalkSettings {
		if _, err := execWithDriver(ctx, tx, r.cfg.Database.SelectedDriver,
			`INSERT INTO user_dingtalk_settings (user_id, enabled, webhook, secret, use_sign, keyword, initialized) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			`INSERT INTO user_dingtalk_settings (user_id, enabled, webhook, secret, use_sign, keyword, initialized) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			row.UserID, row.Enabled, row.Webhook, row.Secret, row.UseSign, row.Keyword, row.Initialized,
		); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	cfg := r.cfg
	if payload.LoginMaxFailed >= 1 && payload.LoginMaxFailed <= 20 {
		cfg.Auth.LoginLimitMaxFail = payload.LoginMaxFailed
	}
	if payload.LoginWindowSec >= 30 && payload.LoginWindowSec <= 3600 {
		cfg.Auth.LoginLimitWindow = payload.LoginWindowSec
	}
	if err := saveConfig(cfg); err != nil {
		return fmt.Errorf("导入成功但保存登录限流失败: %w", err)
	}
	r.cfg = cfg
	return nil
}

func exportUsers(ctx context.Context, db *sql.DB, driver DatabaseDriver) ([]BackupUser, error) {
	query := `SELECT id, username, name, email, role, password_hash, created_at FROM users ORDER BY id`
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []BackupUser{}
	for rows.Next() {
		var row BackupUser
		if err := rows.Scan(&row.ID, &row.Username, &row.Name, &row.Email, &row.Role, &row.PasswordHash, &row.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, row)
	}
	return items, rows.Err()
}

func exportChannels(ctx context.Context, db *sql.DB, driver DatabaseDriver) ([]BackupNotificationChannel, error) {
	_ = driver
	rows, err := db.QueryContext(ctx, `SELECT id, user_id, type, name, target, enabled, last_checked FROM notification_channels ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []BackupNotificationChannel{}
	for rows.Next() {
		var row BackupNotificationChannel
		if err := rows.Scan(&row.ID, &row.UserID, &row.Type, &row.Name, &row.Target, &row.Enabled, &row.LastChecked); err != nil {
			return nil, err
		}
		items = append(items, row)
	}
	return items, rows.Err()
}

func exportEvents(ctx context.Context, db *sql.DB, driver DatabaseDriver) ([]BackupMemoEvent, error) {
	_ = driver
	rows, err := db.QueryContext(ctx, `SELECT id, user_id, title, content, event_at, reminder_enabled, recurrence_type, recurrence_expr, bound_channel_ids, bound_group_ids, countdown_label, status, created_at, updated_at FROM memo_events ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []BackupMemoEvent{}
	for rows.Next() {
		var row BackupMemoEvent
		if err := rows.Scan(&row.ID, &row.UserID, &row.Title, &row.Content, &row.EventAt, &row.ReminderEnabled, &row.RecurrenceType, &row.RecurrenceExpr, &row.BoundChannelIDs, &row.BoundGroupIDs, &row.CountdownLabel, &row.Status, &row.CreatedAt, &row.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, row)
	}
	return items, rows.Err()
}

func exportReminderPoints(ctx context.Context, db *sql.DB, driver DatabaseDriver) ([]BackupReminderPoint, error) {
	_ = driver
	rows, err := db.QueryContext(ctx, `SELECT id, event_id, label, offset_min FROM reminder_points ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []BackupReminderPoint{}
	for rows.Next() {
		var row BackupReminderPoint
		if err := rows.Scan(&row.ID, &row.EventID, &row.Label, &row.OffsetMin); err != nil {
			return nil, err
		}
		items = append(items, row)
	}
	return items, rows.Err()
}

func exportNotificationLogs(ctx context.Context, db *sql.DB, driver DatabaseDriver) ([]BackupNotificationLog, error) {
	_ = driver
	rows, err := db.QueryContext(ctx, `SELECT id, event_id, reminder_id, channel_type, channel_name, status, message, triggered_at FROM notification_logs ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []BackupNotificationLog{}
	for rows.Next() {
		var row BackupNotificationLog
		if err := rows.Scan(&row.ID, &row.EventID, &row.ReminderID, &row.ChannelType, &row.ChannelName, &row.Status, &row.Message, &row.TriggeredAt); err != nil {
			return nil, err
		}
		items = append(items, row)
	}
	return items, rows.Err()
}

func exportReminderTasks(ctx context.Context, db *sql.DB, driver DatabaseDriver) ([]BackupReminderTask, error) {
	_ = driver
	rows, err := db.QueryContext(ctx, `SELECT id, event_id, reminder_id, channel_id, channel_type, status, scheduled_at, triggered_at, last_error, retry_count, max_retries FROM reminder_tasks ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []BackupReminderTask{}
	for rows.Next() {
		var row BackupReminderTask
		if err := rows.Scan(&row.ID, &row.EventID, &row.ReminderID, &row.ChannelID, &row.ChannelType, &row.Status, &row.ScheduledAt, &row.TriggeredAt, &row.LastError, &row.RetryCount, &row.MaxRetries); err != nil {
			return nil, err
		}
		items = append(items, row)
	}
	return items, rows.Err()
}

func exportNotificationGroups(ctx context.Context, db *sql.DB, driver DatabaseDriver) ([]BackupNotificationGroup, error) {
	_ = driver
	rows, err := db.QueryContext(ctx, `SELECT id, user_id, name, enabled FROM notification_groups ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []BackupNotificationGroup{}
	for rows.Next() {
		var row BackupNotificationGroup
		if err := rows.Scan(&row.ID, &row.UserID, &row.Name, &row.Enabled); err != nil {
			return nil, err
		}
		items = append(items, row)
	}
	return items, rows.Err()
}

func exportNotificationGroupMembers(ctx context.Context, db *sql.DB, driver DatabaseDriver) ([]BackupNotificationGroupMember, error) {
	_ = driver
	rows, err := db.QueryContext(ctx, `SELECT id, group_id, type, label, target, secret, keyword, use_sign, enabled FROM notification_group_members ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []BackupNotificationGroupMember{}
	for rows.Next() {
		var row BackupNotificationGroupMember
		if err := rows.Scan(&row.ID, &row.GroupID, &row.Type, &row.Label, &row.Target, &row.Secret, &row.Keyword, &row.UseSign, &row.Enabled); err != nil {
			return nil, err
		}
		items = append(items, row)
	}
	return items, rows.Err()
}

func exportUserMailSettings(ctx context.Context, db *sql.DB, driver DatabaseDriver) ([]BackupUserMailSetting, error) {
	_ = driver
	rows, err := db.QueryContext(ctx, `SELECT user_id, enabled, host, port, username, password, from_name, from_address, use_tls, use_ssl, initialized FROM user_mail_settings ORDER BY user_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []BackupUserMailSetting{}
	for rows.Next() {
		var row BackupUserMailSetting
		if err := rows.Scan(&row.UserID, &row.Enabled, &row.Host, &row.Port, &row.Username, &row.Password, &row.FromName, &row.FromAddress, &row.UseTLS, &row.UseSSL, &row.Initialized); err != nil {
			return nil, err
		}
		items = append(items, row)
	}
	return items, rows.Err()
}

func exportUserDingTalkSettings(ctx context.Context, db *sql.DB, driver DatabaseDriver) ([]BackupUserDingTalkSetting, error) {
	_ = driver
	rows, err := db.QueryContext(ctx, `SELECT user_id, enabled, webhook, secret, use_sign, keyword, initialized FROM user_dingtalk_settings ORDER BY user_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []BackupUserDingTalkSetting{}
	for rows.Next() {
		var row BackupUserDingTalkSetting
		if err := rows.Scan(&row.UserID, &row.Enabled, &row.Webhook, &row.Secret, &row.UseSign, &row.Keyword, &row.Initialized); err != nil {
			return nil, err
		}
		items = append(items, row)
	}
	return items, rows.Err()
}
