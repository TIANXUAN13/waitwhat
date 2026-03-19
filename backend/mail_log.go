package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	mailLogFile    = "mail.log"
	mailLogMaxSize = 10 * 1024 * 1024
	mailLogBackup  = "mail.log.1"
)

var mailLoggerMu sync.Mutex

func writeMailLog(format string, args ...any) {
	mailLoggerMu.Lock()
	defer mailLoggerMu.Unlock()

	logDir := filepath.Join(dataDir(), "logs")
	logPath := filepath.Join(logDir, mailLogFile)
	backupPath := filepath.Join(logDir, mailLogBackup)
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return
	}

	if info, err := os.Stat(logPath); err == nil && info.Size() >= mailLogMaxSize {
		_ = os.Remove(backupPath)
		_ = os.Rename(logPath, backupPath)
	}

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()

	line := fmt.Sprintf("%s %s\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, args...))
	_, _ = f.WriteString(line)
}

func dataDir() string {
	if dir := os.Getenv("APP_DATA_DIR"); dir != "" {
		return dir
	}
	return "./data"
}
