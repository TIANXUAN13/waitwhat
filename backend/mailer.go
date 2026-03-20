package main

import (
	"crypto/tls"
	"errors"
	"fmt"
	"html"
	"mime"
	"net"
	"net/smtp"
	"strings"
	"time"
)

type Mailer interface {
	Send(to, subject, body string) error
}

type SMTPMailer struct {
	cfg MailConfig
}

func NewMailer(cfg MailConfig) Mailer {
	return &SMTPMailer{cfg: cfg}
}

func (m *SMTPMailer) Send(to, subject, body string) error {
	if !m.cfg.Enabled {
		return fmt.Errorf("邮件发送未启用")
	}
	if m.cfg.Host == "" || m.cfg.FromAddress == "" {
		return fmt.Errorf("SMTP 配置不完整")
	}

	address := net.JoinHostPort(m.cfg.Host, fmt.Sprintf("%d", m.cfg.Port))
	auth, err := m.buildAuth()
	if err != nil {
		return err
	}
	msg := buildMessage(m.cfg.FromName, m.cfg.FromAddress, to, subject, body)

	writeMailLog("mail send start host=%s port=%d to=%s from=%s tls=%t ssl=%t", m.cfg.Host, m.cfg.Port, to, m.cfg.FromAddress, m.cfg.UseTLS, m.cfg.UseSSL)

	var primaryErr error
	switch {
	case m.cfg.UseSSL || m.cfg.Port == 465:
		err = m.sendWithImplicitTLS(address, auth, to, msg)
	default:
		if m.cfg.UseTLS {
			writeMailLog("mail using standard smtp sendmail with opportunistic starttls host=%s port=%d", m.cfg.Host, m.cfg.Port)
		}
		err = smtp.SendMail(address, auth, m.cfg.FromAddress, []string{to}, []byte(msg))
	}
	primaryErr = err
	if shouldFallbackToImplicitTLS(m.cfg, err) {
		writeMailLog("mail fallback to implicit tls host=%s port=%d reason=%v", m.cfg.Host, 465, err)
		fallbackErr := m.sendWithImplicitTLS(net.JoinHostPort(m.cfg.Host, "465"), auth, to, msg)
		if fallbackErr != nil {
			err = fmt.Errorf("primary(587)=%v; fallback(465)=%v", primaryErr, fallbackErr)
		} else {
			err = nil
		}
	}
	if err != nil {
		writeMailLog("mail send failed host=%s port=%d to=%s err=%v", m.cfg.Host, m.cfg.Port, to, err)
		return err
	}
	writeMailLog("mail send success host=%s port=%d to=%s", m.cfg.Host, m.cfg.Port, to)
	return nil
}

func (m *SMTPMailer) sendWithImplicitTLS(address string, auth smtp.Auth, to, msg string) error {
	port := portFromAddress(address)
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", address, &tls.Config{
		ServerName: m.cfg.Host,
	})
	if err != nil {
		writeMailLog("mail implicit tls dial failed host=%s port=%s err=%v", m.cfg.Host, port, err)
		return err
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, m.cfg.Host)
	if err != nil {
		writeMailLog("mail implicit tls new client failed host=%s port=%s err=%v", m.cfg.Host, port, err)
		return err
	}
	defer client.Quit()

	if auth != nil {
		if err := client.Auth(auth); err != nil {
			writeMailLog("mail implicit tls auth failed host=%s port=%d username=%s err=%v", m.cfg.Host, m.cfg.Port, m.cfg.Username, err)
			return err
		}
	}
	if err := client.Mail(m.cfg.FromAddress); err != nil {
		writeMailLog("mail implicit tls mail from failed from=%s err=%v", m.cfg.FromAddress, err)
		return err
	}
	if err := client.Rcpt(to); err != nil {
		writeMailLog("mail implicit tls rcpt failed to=%s err=%v", to, err)
		return err
	}

	writer, err := client.Data()
	if err != nil {
		writeMailLog("mail implicit tls data failed to=%s err=%v", to, err)
		return err
	}
	if _, err := writer.Write([]byte(msg)); err != nil {
		_ = writer.Close()
		writeMailLog("mail implicit tls write failed to=%s err=%v", to, err)
		return err
	}
	if err := writer.Close(); err != nil {
		writeMailLog("mail implicit tls close failed to=%s err=%v", to, err)
		return err
	}
	return nil
}

func (m *SMTPMailer) buildAuth() (smtp.Auth, error) {
	username := strings.TrimSpace(m.cfg.Username)
	password := strings.TrimSpace(m.cfg.Password)
	if password == "" {
		return nil, nil
	}
	if username == "" {
		username = strings.TrimSpace(m.cfg.FromAddress)
	}
	if username == "" {
		return nil, errors.New("已填写授权码时，账号或发件人邮箱至少填写一个")
	}
	return smtp.PlainAuth("", username, password, m.cfg.Host), nil
}

func buildMessage(fromName, fromAddress, to, subject, body string) string {
	from := fromAddress
	if strings.TrimSpace(fromName) != "" {
		from = fmt.Sprintf("%s <%s>", fromName, fromAddress)
	}
	encodedSubject := mime.QEncoding.Encode("UTF-8", subject)
	boundary := fmt.Sprintf("waitwhat-%d", time.Now().UnixNano())
	plainBody := body
	htmlBody := buildHTMLMailBody(subject, body)
	lines := []string{
		"From: " + from,
		"To: " + to,
		"Subject: " + encodedSubject,
		"MIME-Version: 1.0",
		fmt.Sprintf("Content-Type: multipart/alternative; boundary=%q", boundary),
		"",
		"--" + boundary,
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: 8bit",
		"",
		plainBody,
		"",
		"--" + boundary,
		"Content-Type: text/html; charset=UTF-8",
		"Content-Transfer-Encoding: 8bit",
		"",
		htmlBody,
		"",
		"--" + boundary + "--",
	}
	return strings.Join(lines, "\r\n")
}

func buildHTMLMailBody(subject, body string) string {
	safeSubject := html.EscapeString(subject)
	safeBody := html.EscapeString(body)
	safeBody = strings.ReplaceAll(safeBody, "\n", "<br>")
	return fmt.Sprintf(`<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width,initial-scale=1" />
  <title>%s</title>
</head>
<body style="margin:0;padding:0;background:#f3f8f7;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,'PingFang SC','Hiragino Sans GB','Microsoft YaHei',sans-serif;color:#173c39;">
  <div style="max-width:620px;margin:24px auto;padding:0 12px;">
    <div style="background:#ffffff;border:1px solid #d9ece8;border-radius:16px;overflow:hidden;box-shadow:0 6px 18px rgba(23,60,57,0.08);">
      <div style="padding:18px 20px;background:linear-gradient(135deg,#1fa58b 0%%,#1b7d6f 100%%);color:#fff;">
        <div style="font-size:12px;letter-spacing:1.5px;opacity:.9;">WAITWHAT MAIL</div>
        <div style="font-size:20px;font-weight:700;margin-top:6px;">%s</div>
      </div>
      <div style="padding:20px;line-height:1.75;font-size:15px;color:#1f3f3b;">
        %s
      </div>
      <div style="padding:12px 20px;border-top:1px solid #e9f3f1;font-size:12px;color:#5f7f7a;background:#fbfefe;">
        此邮件由 WaitWhat 自动发送，请勿直接回复。
      </div>
    </div>
  </div>
</body>
</html>`, safeSubject, safeSubject, safeBody)
}

func shouldFallbackToImplicitTLS(cfg MailConfig, err error) bool {
	if err == nil {
		return false
	}
	if cfg.UseSSL || !cfg.UseTLS || cfg.Port != 587 {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "eof")
}

func portFromAddress(address string) string {
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return "unknown"
	}
	return port
}
