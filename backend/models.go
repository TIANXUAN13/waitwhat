package main

import (
	"strings"
	"time"
)

type DatabaseDriver string

const (
	DriverSQLite DatabaseDriver = "sqlite"
	DriverPG     DatabaseDriver = "postgres"
)

type AppState struct {
	Database     DatabaseConfig      `json:"database"`
	Mail         MailConfig          `json:"mail"`
	DingTalk     DingTalkConfig      `json:"dingTalk"`
	Auth         AuthState           `json:"auth"`
	Users        []User              `json:"users"`
	Events       []MemoEvent         `json:"events"`
	Tasks        []ReminderTask      `json:"tasks"`
	Logs         []NotifyLog         `json:"logs"`
	NotifyGroups []NotificationGroup `json:"notifyGroups"`
}

type AuthState struct {
	AdminExists    bool  `json:"adminExists"`
	CurrentUser    *User `json:"currentUser"`
	LoginMaxFailed int   `json:"loginMaxFailed"`
	LoginWindowSec int   `json:"loginWindowSec"`
}

type DatabaseConfig struct {
	SelectedDriver DatabaseDriver `json:"selectedDriver"`
	SQLitePath     string         `json:"sqlitePath"`
	PGHost         string         `json:"pgHost"`
	PGPort         int            `json:"pgPort"`
	PGDatabase     string         `json:"pgDatabase"`
	PGUser         string         `json:"pgUser"`
	PGPassword     string         `json:"-"`
	PGSSLMode      string         `json:"pgSslMode"`
	InitializedAt  time.Time      `json:"initializedAt"`
}

func (cfg DatabaseConfig) Safe() DatabaseConfig {
	cfg.PGPassword = ""
	return cfg
}

type User struct {
	ID       int64                 `json:"id"`
	Username string                `json:"username"`
	Name     string                `json:"name"`
	Email    string                `json:"email"`
	Role     string                `json:"role"`
	Channels []NotificationChannel `json:"channels"`
}

type NotificationChannel struct {
	ID          int64     `json:"id"`
	UserID      int64     `json:"userId"`
	Type        string    `json:"type"`
	Name        string    `json:"name"`
	Target      string    `json:"target"`
	Enabled     bool      `json:"enabled"`
	LastChecked time.Time `json:"lastChecked"`
}

type NotificationGroup struct {
	ID      int64                     `json:"id"`
	UserID  int64                     `json:"userId"`
	Name    string                    `json:"name"`
	Enabled bool                      `json:"enabled"`
	Members []NotificationGroupMember `json:"members"`
}

type NotificationGroupMember struct {
	ID      int64  `json:"id"`
	GroupID int64  `json:"groupId"`
	Type    string `json:"type"`
	Label   string `json:"label"`
	Target  string `json:"target"`
	Secret  string `json:"-"`
	Keyword string `json:"keyword"`
	UseSign bool   `json:"useSign"`
	Enabled bool   `json:"enabled"`
}

type MailConfig struct {
	Enabled     bool      `json:"enabled"`
	Host        string    `json:"host"`
	Port        int       `json:"port"`
	Username    string    `json:"username"`
	Password    string    `json:"-"`
	HasPassword bool      `json:"hasPassword"`
	FromName    string    `json:"fromName"`
	FromAddress string    `json:"fromAddress"`
	UseTLS      bool      `json:"useTls"`
	UseSSL      bool      `json:"useSsl"`
	Initialized time.Time `json:"initialized"`
}

func (cfg MailConfig) Safe() MailConfig {
	cfg.HasPassword = strings.TrimSpace(cfg.Password) != ""
	cfg.Password = ""
	return cfg
}

type DingTalkConfig struct {
	Enabled     bool      `json:"enabled"`
	Webhook     string    `json:"webhook"`
	Secret      string    `json:"-"`
	UseSign     bool      `json:"useSign"`
	Keyword     string    `json:"keyword"`
	Initialized time.Time `json:"initialized"`
}

func (cfg DingTalkConfig) Safe() DingTalkConfig {
	cfg.Secret = ""
	return cfg
}

type ReminderPoint struct {
	ID        int64  `json:"id"`
	Label     string `json:"label"`
	OffsetMin int    `json:"offsetMin"`
}

type MemoEvent struct {
	ID                 int64                 `json:"id"`
	UserID             int64                 `json:"userId"`
	Title              string                `json:"title"`
	Content            string                `json:"content"`
	EventAt            time.Time             `json:"eventAt"`
	ReminderEnabled    bool                  `json:"reminderEnabled"`
	RecurrenceType     string                `json:"recurrenceType"`
	RecurrenceExpr     string                `json:"recurrenceExpr"`
	ReminderPoints     []ReminderPoint       `json:"reminderPoints"`
	BoundChannelIDs    []int64               `json:"boundChannelIds"`
	BoundGroupIDs      []int64               `json:"boundGroupIds"`
	CountdownLabel     string                `json:"countdownLabel"`
	Status             string                `json:"status"`
	CreatedAt          time.Time             `json:"createdAt"`
	UpdatedAt          time.Time             `json:"updatedAt"`
	UpcomingNotifyPlan []UpcomingNotifyPoint `json:"upcomingNotifyPlan"`
}

type UpcomingNotifyPoint struct {
	Label          string    `json:"label"`
	NotifyAt       time.Time `json:"notifyAt"`
	ChannelSummary string    `json:"channelSummary"`
}

type NotifyLog struct {
	ID          int64     `json:"id"`
	EventID     int64     `json:"eventId"`
	ReminderID  int64     `json:"reminderId"`
	ChannelType string    `json:"channelType"`
	ChannelName string    `json:"channelName"`
	Status      string    `json:"status"`
	Message     string    `json:"message"`
	TriggeredAt time.Time `json:"triggeredAt"`
}

type ReminderTask struct {
	ID          int64     `json:"id"`
	EventID     int64     `json:"eventId"`
	ReminderID  int64     `json:"reminderId"`
	ChannelID   int64     `json:"channelId"`
	ChannelType string    `json:"channelType"`
	Status      string    `json:"status"`
	ScheduledAt time.Time `json:"scheduledAt"`
	TriggeredAt time.Time `json:"triggeredAt"`
	LastError   string    `json:"lastError"`
	RetryCount  int       `json:"retryCount"`
	MaxRetries  int       `json:"maxRetries"`
}

type AdminAuditLog struct {
	ID             int64     `json:"id"`
	ActorUserID    int64     `json:"actorUserId"`
	ActorUsername  string    `json:"actorUsername"`
	Action         string    `json:"action"`
	TargetUserID   int64     `json:"targetUserId"`
	TargetUsername string    `json:"targetUsername"`
	Detail         string    `json:"detail"`
	CreatedAt      time.Time `json:"createdAt"`
}

type InitDatabaseRequest struct {
	Driver     DatabaseDriver `json:"driver"`
	SQLitePath string         `json:"sqlitePath"`
	PGHost     string         `json:"pgHost"`
	PGPort     int            `json:"pgPort"`
	PGDatabase string         `json:"pgDatabase"`
	PGUser     string         `json:"pgUser"`
	PGPassword string         `json:"pgPassword"`
	PGSSLMode  string         `json:"pgSslMode"`
}

type CreateEventRequest struct {
	UserID          int64           `json:"userId"`
	Title           string          `json:"title"`
	Content         string          `json:"content"`
	EventAt         string          `json:"eventAt"`
	ReminderEnabled bool            `json:"reminderEnabled"`
	RecurrenceType  string          `json:"recurrenceType"`
	RecurrenceExpr  string          `json:"recurrenceExpr"`
	ReminderPoints  []ReminderPoint `json:"reminderPoints"`
	BoundChannelIDs []int64         `json:"boundChannelIds"`
	BoundGroupIDs   []int64         `json:"boundGroupIds"`
}

type UpdateEventRequest = CreateEventRequest

type SaveMailConfigRequest struct {
	Enabled     bool   `json:"enabled"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	FromName    string `json:"fromName"`
	FromAddress string `json:"fromAddress"`
	UseTLS      bool   `json:"useTls"`
	UseSSL      bool   `json:"useSsl"`
}

type SendTestMailRequest struct {
	To string `json:"to"`
}

type DiagnoseMailRequest struct {
	Host string `json:"host"`
}

type MailDiagnoseStep struct {
	Port      int    `json:"port"`
	Mode      string `json:"mode"`
	Step      string `json:"step"`
	OK        bool   `json:"ok"`
	LatencyMS int64  `json:"latencyMs"`
	Error     string `json:"error,omitempty"`
}

type MailDiagnoseResult struct {
	Host  string             `json:"host"`
	Steps []MailDiagnoseStep `json:"steps"`
}

type ReminderDispatchResult struct {
	Triggered int `json:"triggered"`
	Sent      int `json:"sent"`
	Failed    int `json:"failed"`
	Skipped   int `json:"skipped"`
	Retried   int `json:"retried"`
}

type SaveDingTalkConfigRequest struct {
	Enabled bool   `json:"enabled"`
	Webhook string `json:"webhook"`
	Secret  string `json:"secret"`
	UseSign bool   `json:"useSign"`
	Keyword string `json:"keyword"`
}

type SendTestDingTalkRequest struct {
	Webhook string `json:"webhook"`
	Secret  string `json:"secret"`
	Keyword string `json:"keyword"`
	UseSign bool   `json:"useSign"`
}

type SaveNotificationGroupRequest struct {
	ID      int64                            `json:"id"`
	Name    string                           `json:"name"`
	Enabled bool                             `json:"enabled"`
	Members []SaveNotificationGroupMemberReq `json:"members"`
}

type SaveNotificationGroupMemberReq struct {
	Type    string `json:"type"`
	Label   string `json:"label"`
	Target  string `json:"target"`
	Secret  string `json:"secret"`
	Keyword string `json:"keyword"`
	UseSign bool   `json:"useSign"`
	Enabled bool   `json:"enabled"`
}

type SetupAdminRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Email    string `json:"email"`
	Name     string `json:"name"`
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type RegisterRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Email    string `json:"email"`
	Name     string `json:"name"`
}

type AdminUpdateUserPasswordRequest struct {
	Password string `json:"password"`
}

type AdminUpdateLoginPolicyRequest struct {
	LoginMaxFailed int `json:"loginMaxFailed"`
	LoginWindowSec int `json:"loginWindowSec"`
}

type AuthResponse struct {
	Token string `json:"token"`
	User  User   `json:"user"`
}

type BackupUser struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	Name         string `json:"name"`
	Email        string `json:"email"`
	Role         string `json:"role"`
	PasswordHash string `json:"passwordHash"`
	CreatedAt    string `json:"createdAt"`
}

type BackupNotificationChannel struct {
	ID          int64  `json:"id"`
	UserID      int64  `json:"userId"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	Target      string `json:"target"`
	Enabled     int    `json:"enabled"`
	LastChecked string `json:"lastChecked"`
}

type BackupMemoEvent struct {
	ID              int64  `json:"id"`
	UserID          int64  `json:"userId"`
	Title           string `json:"title"`
	Content         string `json:"content"`
	EventAt         string `json:"eventAt"`
	ReminderEnabled int    `json:"reminderEnabled"`
	RecurrenceType  string `json:"recurrenceType"`
	RecurrenceExpr  string `json:"recurrenceExpr"`
	BoundChannelIDs string `json:"boundChannelIds"`
	BoundGroupIDs   string `json:"boundGroupIds"`
	CountdownLabel  string `json:"countdownLabel"`
	Status          string `json:"status"`
	CreatedAt       string `json:"createdAt"`
	UpdatedAt       string `json:"updatedAt"`
}

type BackupReminderPoint struct {
	ID        int64  `json:"id"`
	EventID   int64  `json:"eventId"`
	Label     string `json:"label"`
	OffsetMin int    `json:"offsetMin"`
}

type BackupNotificationLog struct {
	ID          int64  `json:"id"`
	EventID     int64  `json:"eventId"`
	ReminderID  int64  `json:"reminderId"`
	ChannelType string `json:"channelType"`
	ChannelName string `json:"channelName"`
	Status      string `json:"status"`
	Message     string `json:"message"`
	TriggeredAt string `json:"triggeredAt"`
}

type BackupReminderTask struct {
	ID          int64  `json:"id"`
	EventID     int64  `json:"eventId"`
	ReminderID  int64  `json:"reminderId"`
	ChannelID   int64  `json:"channelId"`
	ChannelType string `json:"channelType"`
	Status      string `json:"status"`
	ScheduledAt string `json:"scheduledAt"`
	TriggeredAt string `json:"triggeredAt"`
	LastError   string `json:"lastError"`
	RetryCount  int    `json:"retryCount"`
	MaxRetries  int    `json:"maxRetries"`
}

type BackupNotificationGroup struct {
	ID      int64  `json:"id"`
	UserID  int64  `json:"userId"`
	Name    string `json:"name"`
	Enabled int    `json:"enabled"`
}

type BackupNotificationGroupMember struct {
	ID      int64  `json:"id"`
	GroupID int64  `json:"groupId"`
	Type    string `json:"type"`
	Label   string `json:"label"`
	Target  string `json:"target"`
	Secret  string `json:"secret"`
	Keyword string `json:"keyword"`
	UseSign int    `json:"useSign"`
	Enabled int    `json:"enabled"`
}

type BackupUserMailSetting struct {
	UserID      int64  `json:"userId"`
	Enabled     int    `json:"enabled"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	FromName    string `json:"fromName"`
	FromAddress string `json:"fromAddress"`
	UseTLS      int    `json:"useTls"`
	UseSSL      int    `json:"useSsl"`
	Initialized string `json:"initialized"`
}

type BackupUserDingTalkSetting struct {
	UserID      int64  `json:"userId"`
	Enabled     int    `json:"enabled"`
	Webhook     string `json:"webhook"`
	Secret      string `json:"secret"`
	UseSign     int    `json:"useSign"`
	Keyword     string `json:"keyword"`
	Initialized string `json:"initialized"`
}

type BackupPayload struct {
	Version    int    `json:"version"`
	ExportedAt string `json:"exportedAt"`

	LoginMaxFailed int `json:"loginMaxFailed"`
	LoginWindowSec int `json:"loginWindowSec"`

	Users                    []BackupUser                    `json:"users"`
	NotificationChannels     []BackupNotificationChannel     `json:"notificationChannels"`
	MemoEvents               []BackupMemoEvent               `json:"memoEvents"`
	ReminderPoints           []BackupReminderPoint           `json:"reminderPoints"`
	NotificationLogs         []BackupNotificationLog         `json:"notificationLogs"`
	ReminderTasks            []BackupReminderTask            `json:"reminderTasks"`
	NotificationGroups       []BackupNotificationGroup       `json:"notificationGroups"`
	NotificationGroupMembers []BackupNotificationGroupMember `json:"notificationGroupMembers"`
	UserMailSettings         []BackupUserMailSetting         `json:"userMailSettings"`
	UserDingTalkSettings     []BackupUserDingTalkSetting     `json:"userDingTalkSettings"`
}
