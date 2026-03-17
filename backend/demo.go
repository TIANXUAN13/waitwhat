package main

import "time"

func demoData(now time.Time) ([]User, []MemoEvent, []NotifyLog) {
	users := []User{
		{
			ID:    1,
			Name:  "Chen",
			Email: "chen@example.com",
			Channels: []NotificationChannel{
				{ID: 1, UserID: 1, Type: "email", Name: "工作邮箱", Target: "chen@example.com", Enabled: true, LastChecked: now.Add(-2 * time.Hour)},
				{ID: 2, UserID: 1, Type: "dingtalk", Name: "项目群机器人", Target: "https://oapi.dingtalk.com/robot/send?access_token=demo", Enabled: true, LastChecked: now.Add(-30 * time.Minute)},
				{ID: 3, UserID: 1, Type: "qq", Name: "QQ 机器人", Target: "Group-1024", Enabled: false, LastChecked: now.Add(-24 * time.Hour)},
			},
		},
	}

	events := []MemoEvent{
		{
			ID:              101,
			UserID:          1,
			Title:           "产品演示会",
			Content:         "准备演示稿、检查提醒链路，提前同步测试账号。",
			EventAt:         now.Add(26 * time.Hour),
			ReminderEnabled: true,
			ReminderPoints: []ReminderPoint{
				{ID: 1, Label: "提前 1 天", OffsetMin: 1440},
				{ID: 2, Label: "提前 2 小时", OffsetMin: 120},
				{ID: 3, Label: "提前 15 分钟", OffsetMin: 15},
			},
			BoundChannelIDs: []int64{1, 2},
			CountdownLabel:  "1天 2小时后开始",
			Status:          "scheduled",
			CreatedAt:       now.Add(-12 * time.Hour),
			UpdatedAt:       now.Add(-20 * time.Minute),
			UpcomingNotifyPlan: []UpcomingNotifyPoint{
				{Label: "提前 1 天", NotifyAt: now.Add(2 * time.Hour), ChannelSummary: "工作邮箱, 项目群机器人"},
				{Label: "提前 2 小时", NotifyAt: now.Add(24 * time.Hour), ChannelSummary: "工作邮箱, 项目群机器人"},
				{Label: "提前 15 分钟", NotifyAt: now.Add(25*time.Hour + 45*time.Minute), ChannelSummary: "工作邮箱, 项目群机器人"},
			},
		},
		{
			ID:              102,
			UserID:          1,
			Title:           "服务器续费提醒",
			Content:         "续费前确认磁盘快照和监控报警设置。",
			EventAt:         now.Add(72 * time.Hour),
			ReminderEnabled: true,
			ReminderPoints: []ReminderPoint{
				{ID: 4, Label: "提前 2 天", OffsetMin: 2880},
				{ID: 5, Label: "提前 6 小时", OffsetMin: 360},
			},
			BoundChannelIDs: []int64{1},
			CountdownLabel:  "2天 23小时后开始",
			Status:          "scheduled",
			CreatedAt:       now.Add(-48 * time.Hour),
			UpdatedAt:       now.Add(-90 * time.Minute),
			UpcomingNotifyPlan: []UpcomingNotifyPoint{
				{Label: "提前 2 天", NotifyAt: now.Add(24 * time.Hour), ChannelSummary: "工作邮箱"},
				{Label: "提前 6 小时", NotifyAt: now.Add(66 * time.Hour), ChannelSummary: "工作邮箱"},
			},
		},
	}

	logs := []NotifyLog{
		{ID: 1001, EventID: 88, ChannelType: "email", ChannelName: "工作邮箱", Status: "success", Message: "已发送提醒邮件", TriggeredAt: now.Add(-6 * time.Hour)},
		{ID: 1002, EventID: 89, ChannelType: "dingtalk", ChannelName: "项目群机器人", Status: "failed", Message: "签名校验失败，等待重新配置", TriggeredAt: now.Add(-90 * time.Minute)},
		{ID: 1003, EventID: 90, ChannelType: "email", ChannelName: "工作邮箱", Status: "success", Message: "预提醒发送成功", TriggeredAt: now.Add(-30 * time.Minute)},
	}

	return users, events, logs
}
