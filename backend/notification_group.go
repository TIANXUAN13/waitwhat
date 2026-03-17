package main

import (
	"context"
	"errors"
	"strings"
)

func (r *Repository) SaveNotificationGroup(ctx context.Context, userID int64, req SaveNotificationGroupRequest) (NotificationGroup, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return NotificationGroup{}, errors.New("通知组名称不能为空")
	}
	if len(req.Members) == 0 {
		return NotificationGroup{}, errors.New("通知组至少需要一个成员")
	}

	db, err := r.openDB()
	if err != nil {
		return NotificationGroup{}, err
	}
	defer db.Close()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return NotificationGroup{}, err
	}
	defer tx.Rollback()

	groupID := req.ID
	if groupID > 0 {
		res, err := execWithDriver(ctx, tx, r.cfg.Database.SelectedDriver,
			`UPDATE notification_groups SET name = ?, enabled = ? WHERE id = ? AND user_id = ?`,
			`UPDATE notification_groups SET name = $1, enabled = $2 WHERE id = $3 AND user_id = $4`,
			name, boolToInt(req.Enabled), groupID, userID,
		)
		if err != nil {
			return NotificationGroup{}, err
		}
		affected, _ := res.RowsAffected()
		if affected == 0 {
			return NotificationGroup{}, errors.New("通知组不存在或无权修改")
		}
		if _, err := execWithDriver(ctx, tx, r.cfg.Database.SelectedDriver,
			`DELETE FROM notification_group_members WHERE group_id = ?`,
			`DELETE FROM notification_group_members WHERE group_id = $1`,
			groupID,
		); err != nil {
			return NotificationGroup{}, err
		}
	} else {
		res, err := execWithDriver(ctx, tx, r.cfg.Database.SelectedDriver,
			`INSERT INTO notification_groups (user_id, name, enabled) VALUES (?, ?, ?)`,
			`INSERT INTO notification_groups (user_id, name, enabled) VALUES ($1, $2, $3)`,
			userID, name, boolToInt(req.Enabled),
		)
		if err != nil {
			return NotificationGroup{}, err
		}
		groupID, _ = res.LastInsertId()
		if groupID == 0 {
			query := "SELECT id FROM notification_groups WHERE user_id = ? AND name = ? ORDER BY id DESC LIMIT 1"
			args := []any{userID, name}
			if r.cfg.Database.SelectedDriver == DriverPG {
				query = "SELECT id FROM notification_groups WHERE user_id = $1 AND name = $2 ORDER BY id DESC LIMIT 1"
			}
			if err := tx.QueryRowContext(ctx, query, args...).Scan(&groupID); err != nil {
				return NotificationGroup{}, err
			}
		}
	}

	for _, member := range req.Members {
		memberType := strings.TrimSpace(member.Type)
		target := strings.TrimSpace(member.Target)
		label := firstNonEmpty(member.Label, target, memberType)
		if memberType == "" || target == "" {
			return NotificationGroup{}, errors.New("通知组成员类型和目标不能为空")
		}
		if memberType != "email" && memberType != "dingtalk_webhook" {
			return NotificationGroup{}, errors.New("通知组成员类型不支持")
		}
		if _, err := execWithDriver(ctx, tx, r.cfg.Database.SelectedDriver,
			`INSERT INTO notification_group_members (group_id, type, label, target, secret, use_sign, enabled) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			`INSERT INTO notification_group_members (group_id, type, label, target, secret, use_sign, enabled) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			groupID, memberType, label, target, member.Secret, boolToInt(member.UseSign), boolToInt(member.Enabled),
		); err != nil {
			return NotificationGroup{}, err
		}
	}

	if _, err := execWithDriver(ctx, tx, r.cfg.Database.SelectedDriver,
		`DELETE FROM reminder_tasks WHERE event_id IN (SELECT id FROM memo_events WHERE user_id = ?)`,
		`DELETE FROM reminder_tasks WHERE event_id IN (SELECT id FROM memo_events WHERE user_id = $1)`,
		userID,
	); err != nil {
		return NotificationGroup{}, err
	}

	if err := tx.Commit(); err != nil {
		return NotificationGroup{}, err
	}

	groups, err := loadNotificationGroups(ctx, db, r.cfg.Database.SelectedDriver, userID)
	if err != nil {
		return NotificationGroup{}, err
	}
	for _, group := range groups {
		if group.ID == groupID {
			return group, nil
		}
	}
	return NotificationGroup{}, errors.New("保存后读取通知组失败")
}

func (r *Repository) DeleteNotificationGroup(ctx context.Context, userID, groupID int64) error {
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

	res, err := execWithDriver(ctx, tx, r.cfg.Database.SelectedDriver,
		`DELETE FROM notification_groups WHERE id = ? AND user_id = ?`,
		`DELETE FROM notification_groups WHERE id = $1 AND user_id = $2`,
		groupID, userID,
	)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return errors.New("通知组不存在或无权删除")
	}

	if _, err := execWithDriver(ctx, tx, r.cfg.Database.SelectedDriver,
		`DELETE FROM notification_group_members WHERE group_id = ?`,
		`DELETE FROM notification_group_members WHERE group_id = $1`,
		groupID,
	); err != nil {
		return err
	}
	if _, err := execWithDriver(ctx, tx, r.cfg.Database.SelectedDriver,
		`DELETE FROM reminder_tasks WHERE event_id IN (SELECT id FROM memo_events WHERE user_id = ?)`,
		`DELETE FROM reminder_tasks WHERE event_id IN (SELECT id FROM memo_events WHERE user_id = $1)`,
		userID,
	); err != nil {
		return err
	}
	return tx.Commit()
}
