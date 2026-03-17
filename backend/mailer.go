package main

import (
	"crypto/tls"
	"fmt"
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
	if m.cfg.Host == "" || m.cfg.Username == "" || m.cfg.Password == "" || m.cfg.FromAddress == "" {
		return fmt.Errorf("SMTP 配置不完整")
	}

	address := net.JoinHostPort(m.cfg.Host, fmt.Sprintf("%d", m.cfg.Port))
	auth := smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, m.cfg.Host)
	msg := buildMessage(m.cfg.FromName, m.cfg.FromAddress, to, subject, body)

	writeMailLog("mail send start host=%s port=%d to=%s from=%s tls=%t ssl=%t", m.cfg.Host, m.cfg.Port, to, m.cfg.FromAddress, m.cfg.UseTLS, m.cfg.UseSSL)

	var err error
	var primaryErr error
	switch {
	case m.cfg.UseSSL:
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

	if err := client.Auth(auth); err != nil {
		writeMailLog("mail implicit tls auth failed host=%s port=%d username=%s err=%v", m.cfg.Host, m.cfg.Port, m.cfg.Username, err)
		return err
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

func buildMessage(fromName, fromAddress, to, subject, body string) string {
	from := fromAddress
	if strings.TrimSpace(fromName) != "" {
		from = fmt.Sprintf("%s <%s>", fromName, fromAddress)
	}
	lines := []string{
		"From: " + from,
		"To: " + to,
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		body,
	}
	return strings.Join(lines, "\r\n")
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
