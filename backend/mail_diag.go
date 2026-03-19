package main

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/smtp"
	"strings"
	"time"
)

func (r *Repository) DiagnoseMail(ctx context.Context, userID int64, req DiagnoseMailRequest) (MailDiagnoseResult, error) {
	host := strings.TrimSpace(req.Host)
	if host == "" {
		db, err := r.openDB()
		if err != nil {
			return MailDiagnoseResult{}, err
		}
		defer db.Close()
		cfg, err := loadUserMailConfig(ctx, db, r.cfg.Database.SelectedDriver, r.cfg.Auth.TokenSecret, userID)
		if err != nil {
			return MailDiagnoseResult{}, err
		}
		host = strings.TrimSpace(cfg.Host)
	}
	if host == "" {
		return MailDiagnoseResult{}, errors.New("请先填写 SMTP 主机")
	}

	result := MailDiagnoseResult{Host: host}
	result.Steps = append(result.Steps, diagnoseSMTP587(host)...)
	result.Steps = append(result.Steps, diagnoseSMTPS465(host)...)
	return result, nil
}

func diagnoseSMTP587(host string) []MailDiagnoseStep {
	address := net.JoinHostPort(host, "587")
	var steps []MailDiagnoseStep

	start := time.Now()
	conn, err := net.DialTimeout("tcp", address, 5*time.Second)
	steps = append(steps, toStep(587, "smtp", "TCP 连接", start, err))
	if err != nil {
		return steps
	}
	defer conn.Close()

	start = time.Now()
	client, err := smtp.NewClient(conn, host)
	steps = append(steps, toStep(587, "smtp", "SMTP 欢迎握手", start, err))
	if err != nil {
		return steps
	}
	defer client.Quit()

	start = time.Now()
	ok, _ := client.Extension("STARTTLS")
	if !ok {
		steps = append(steps, MailDiagnoseStep{
			Port:      587,
			Mode:      "starttls",
			Step:      "STARTTLS 能力检查",
			OK:        false,
			LatencyMS: time.Since(start).Milliseconds(),
			Error:     "服务器未声明 STARTTLS 扩展",
		})
		return steps
	}
	steps = append(steps, MailDiagnoseStep{
		Port:      587,
		Mode:      "starttls",
		Step:      "STARTTLS 能力检查",
		OK:        true,
		LatencyMS: time.Since(start).Milliseconds(),
	})

	start = time.Now()
	err = client.StartTLS(tlsConfigForHost(host))
	steps = append(steps, toStep(587, "starttls", "TLS 升级握手", start, err))
	return steps
}

func diagnoseSMTPS465(host string) []MailDiagnoseStep {
	address := net.JoinHostPort(host, "465")
	var steps []MailDiagnoseStep

	start := time.Now()
	conn, err := tlsDial(address, host)
	steps = append(steps, toStep(465, "implicit_tls", "TLS 直连握手", start, err))
	if err != nil {
		return steps
	}
	defer conn.Close()

	start = time.Now()
	client, err := smtp.NewClient(conn, host)
	steps = append(steps, toStep(465, "implicit_tls", "SMTP 欢迎握手", start, err))
	if err != nil {
		return steps
	}
	_ = client.Quit()
	return steps
}

func tlsDial(address, host string) (net.Conn, error) {
	return tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", address, tlsConfigForHost(host))
}

func tlsConfigForHost(host string) *tls.Config {
	return &tls.Config{ServerName: host}
}

func toStep(port int, mode, step string, start time.Time, err error) MailDiagnoseStep {
	entry := MailDiagnoseStep{
		Port:      port,
		Mode:      mode,
		Step:      step,
		OK:        err == nil,
		LatencyMS: time.Since(start).Milliseconds(),
	}
	if err != nil {
		entry.Error = err.Error()
	}
	return entry
}
