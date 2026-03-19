package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const defaultSQLitePath = "./data/waitwhat.sqlite"

type AppConfig struct {
	Database DatabaseConfig `json:"database"`
	Mail     MailConfig     `json:"mail"`
	DingTalk DingTalkConfig `json:"dingTalk"`
	Auth     AuthConfig     `json:"auth"`
}

type AuthConfig struct {
	TokenSecret       string `json:"tokenSecret"`
	LoginLimitMaxFail int    `json:"loginLimitMaxFail"`
	LoginLimitWindow  int    `json:"loginLimitWindowSeconds"`
}

func defaultConfig() AppConfig {
	return AppConfig{
		Database: DatabaseConfig{
			SelectedDriver: DriverSQLite,
			SQLitePath:     defaultSQLitePath,
			PGPort:         5432,
			PGSSLMode:      "disable",
		},
		Mail: MailConfig{
			Port: 587,
		},
		Auth: AuthConfig{
			TokenSecret:       randomSecret(),
			LoginLimitMaxFail: 5,
			LoginLimitWindow:  600,
		},
	}
}

func configFilePath() string {
	dataDir := os.Getenv("APP_DATA_DIR")
	if dataDir == "" {
		dataDir = "./data"
	}
	return filepath.Join(dataDir, "app-config.json")
}

func ensureParentDir(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0o755)
}

func loadConfig() (AppConfig, error) {
	path := configFilePath()
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return defaultConfig(), nil
		}
		return AppConfig{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return AppConfig{}, err
	}

	cfg := defaultConfig()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return AppConfig{}, err
	}
	if cfg.Database.SQLitePath == "" {
		cfg.Database.SQLitePath = defaultSQLitePath
	}
	// Backward-compat: older config may have no initializedAt while DB file already exists.
	if cfg.Database.InitializedAt.IsZero() && cfg.Database.SelectedDriver == DriverSQLite {
		sqlitePath := cfg.Database.SQLitePath
		if info, err := os.Stat(sqlitePath); err == nil && !info.IsDir() {
			cfg.Database.InitializedAt = info.ModTime()
		} else if _, err := os.Stat(defaultSQLitePath); err == nil {
			cfg.Database.InitializedAt = time.Now()
		}
	}
	if cfg.Auth.TokenSecret == "" {
		cfg.Auth.TokenSecret = randomSecret()
	}
	if cfg.Auth.LoginLimitMaxFail <= 0 {
		cfg.Auth.LoginLimitMaxFail = 5
	}
	if cfg.Auth.LoginLimitWindow <= 0 {
		cfg.Auth.LoginLimitWindow = 600
	}
	return cfg, nil
}

func saveConfig(cfg AppConfig) error {
	path := configFilePath()
	if err := ensureParentDir(path); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func randomSecret() string {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "waitwhat-fallback-secret"
	}
	return hex.EncodeToString(buf)
}
