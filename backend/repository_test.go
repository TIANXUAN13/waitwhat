package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type mockMailer struct{}

func (mockMailer) Send(to, subject, body string) error {
	return nil
}

type mockDingSender struct{}

func (mockDingSender) Send(title, body string) error {
	return nil
}

func TestRepositorySQLiteFlow(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("APP_DATA_DIR", tempDir)

	repo, err := NewRepository()
	if err != nil {
		t.Fatalf("new repository: %v", err)
	}

	sqlitePath := filepath.Join(tempDir, "test.sqlite")
	if _, err := repo.InitDatabase(context.Background(), InitDatabaseRequest{
		Driver:     DriverSQLite,
		SQLitePath: sqlitePath,
	}); err != nil {
		t.Fatalf("init database: %v", err)
	}

	if _, err := os.Stat(sqlitePath); err != nil {
		t.Fatalf("sqlite file not created: %v", err)
	}

	state, err := repo.Bootstrap(context.Background(), 0)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if state.Auth.AdminExists {
		t.Fatal("did not expect admin before setup")
	}

	admin, err := repo.SetupAdmin(context.Background(), SetupAdminRequest{
		Username: "admin",
		Password: "secret",
		Email:    "admin@example.com",
		Name:     "Admin",
	})
	if err != nil {
		t.Fatalf("setup admin: %v", err)
	}
	stateAfterAdmin, err := repo.Bootstrap(context.Background(), admin.ID)
	if err != nil {
		t.Fatalf("bootstrap after admin: %v", err)
	}
	if len(stateAfterAdmin.NotifyGroups) == 0 {
		t.Fatal("expected default notify group after admin setup")
	}

	eventAt := time.Now().Add(4 * time.Hour).Format(time.RFC3339)
	event, err := repo.CreateEvent(context.Background(), CreateEventRequest{
		UserID:          admin.ID,
		Title:           "测试事件",
		Content:         "验证 SQLite 创建流程",
		EventAt:         eventAt,
		ReminderEnabled: true,
		ReminderPoints: []ReminderPoint{
			{Label: "提前 30 分钟", OffsetMin: 30},
			{Label: "提前 5 分钟", OffsetMin: 5},
		},
		BoundChannelIDs: []int64{},
		BoundGroupIDs:   []int64{stateAfterAdmin.NotifyGroups[0].ID},
	})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}

	if event.Title != "测试事件" {
		t.Fatalf("unexpected event title: %s", event.Title)
	}
	if len(event.ReminderPoints) != 3 {
		t.Fatalf("expected 3 reminder points (including at-time), got %d", len(event.ReminderPoints))
	}

	refreshed, err := repo.Bootstrap(context.Background(), admin.ID)
	if err != nil {
		t.Fatalf("bootstrap after create: %v", err)
	}
	if len(refreshed.Events) != 1 {
		t.Fatalf("expected created event in bootstrap, got %d events", len(refreshed.Events))
	}
}

func TestDispatchDueRemindersWithMockMailer(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("APP_DATA_DIR", tempDir)

	repo, err := NewRepository()
	if err != nil {
		t.Fatalf("new repository: %v", err)
	}
	repo.mailerFactory = func(MailConfig) Mailer { return mockMailer{} }
	repo.dingFactory = func(DingTalkConfig) DingTalkSender { return mockDingSender{} }

	sqlitePath := filepath.Join(tempDir, "dispatch.sqlite")
	if _, err := repo.InitDatabase(context.Background(), InitDatabaseRequest{
		Driver:     DriverSQLite,
		SQLitePath: sqlitePath,
	}); err != nil {
		t.Fatalf("init database: %v", err)
	}

	admin, err := repo.SetupAdmin(context.Background(), SetupAdminRequest{
		Username: "admin",
		Password: "secret",
		Email:    "admin@example.com",
		Name:     "Admin",
	})
	if err != nil {
		t.Fatalf("setup admin: %v", err)
	}
	stateAfterAdmin, err := repo.Bootstrap(context.Background(), admin.ID)
	if err != nil {
		t.Fatalf("bootstrap after admin: %v", err)
	}
	if len(stateAfterAdmin.NotifyGroups) == 0 {
		t.Fatal("expected default notify group after admin setup")
	}

	if _, err := repo.SaveMailConfig(context.Background(), admin.ID, SaveMailConfigRequest{
		Enabled:     true,
		Host:        "smtp.example.com",
		Port:        587,
		Username:    "tester@example.com",
		Password:    "secret",
		FromName:    "WaitWhat Memo",
		FromAddress: "tester@example.com",
		UseTLS:      false,
	}); err != nil {
		t.Fatalf("save mail config: %v", err)
	}
	if _, err := repo.SaveDingTalkConfig(context.Background(), admin.ID, SaveDingTalkConfigRequest{
		Enabled: true,
		Webhook: "https://oapi.dingtalk.com/robot/send?access_token=test",
		UseSign: true,
		Secret:  "secret",
		Keyword: "提醒",
	}); err != nil {
		t.Fatalf("save dingtalk config: %v", err)
	}
	_, err = repo.CreateEvent(context.Background(), CreateEventRequest{
		UserID:          admin.ID,
		Title:           "立即提醒事件",
		Content:         "测试调度器发送邮件",
		EventAt:         time.Now().Add(2 * time.Minute).Format(time.RFC3339),
		ReminderEnabled: true,
		ReminderPoints: []ReminderPoint{
			{Label: "提前 5 分钟", OffsetMin: 5},
		},
		BoundChannelIDs: []int64{},
		BoundGroupIDs:   []int64{stateAfterAdmin.NotifyGroups[0].ID},
	})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}

	result, err := repo.DispatchDueReminders(context.Background())
	if err != nil {
		t.Fatalf("dispatch reminders: %v", err)
	}
	if result.Sent < 1 {
		t.Fatalf("expected sent reminders, got %+v", result)
	}

	state, err := repo.Bootstrap(context.Background(), admin.ID)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	found := false
	for _, logItem := range state.Logs {
		if logItem.Message == "提醒邮件发送成功" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected success notify log after dispatch")
	}
}

func TestSendTestMailWithMockMailer(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("APP_DATA_DIR", tempDir)

	repo, err := NewRepository()
	if err != nil {
		t.Fatalf("new repository: %v", err)
	}
	repo.mailerFactory = func(MailConfig) Mailer { return mockMailer{} }

	sqlitePath := filepath.Join(tempDir, "mail.sqlite")
	if _, err := repo.InitDatabase(context.Background(), InitDatabaseRequest{
		Driver:     DriverSQLite,
		SQLitePath: sqlitePath,
	}); err != nil {
		t.Fatalf("init database: %v", err)
	}

	admin, err := repo.SetupAdmin(context.Background(), SetupAdminRequest{
		Username: "admin",
		Password: "secret",
		Email:    "admin@example.com",
		Name:     "Admin",
	})
	if err != nil {
		t.Fatalf("setup admin: %v", err)
	}

	if _, err := repo.SaveMailConfig(context.Background(), admin.ID, SaveMailConfigRequest{
		Enabled:     true,
		Host:        "smtp.example.com",
		Port:        587,
		Username:    "tester@example.com",
		Password:    "secret",
		FromName:    "WaitWhat Memo",
		FromAddress: "tester@example.com",
		UseTLS:      false,
	}); err != nil {
		t.Fatalf("save mail config: %v", err)
	}

	if err := repo.SendTestMail(context.Background(), admin.ID, "target@example.com"); err != nil {
		t.Fatalf("send test mail: %v", err)
	}
}

func TestCORSAllowsAuthorizationHeader(t *testing.T) {
	repo, err := NewRepository()
	if err != nil {
		t.Fatalf("new repository: %v", err)
	}

	req := httptest.NewRequest(http.MethodOptions, "/api/auth/me", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	req.Header.Set("Access-Control-Request-Headers", "authorization,content-type")

	recorder := httptest.NewRecorder()
	NewServer(repo).routes().ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for preflight, got %d", recorder.Code)
	}

	allowedHeaders := recorder.Header().Get("Access-Control-Allow-Headers")
	if !strings.Contains(strings.ToLower(allowedHeaders), "authorization") {
		t.Fatalf("expected authorization in allow headers, got %q", allowedHeaders)
	}
}
