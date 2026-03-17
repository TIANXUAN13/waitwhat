package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type DingTalkSender interface {
	Send(title, body string) error
}

type WebhookDingTalkSender struct {
	cfg DingTalkConfig
}

func NewDingTalkSender(cfg DingTalkConfig) DingTalkSender {
	return &WebhookDingTalkSender{cfg: cfg}
}

func (s *WebhookDingTalkSender) Send(title, body string) error {
	if !s.cfg.Enabled {
		return fmt.Errorf("钉钉机器人未启用")
	}
	if s.cfg.Webhook == "" {
		return fmt.Errorf("钉钉 webhook 不能为空")
	}

	webhookURL := s.cfg.Webhook
	if s.cfg.UseSign {
		signedURL, err := buildSignedWebhook(s.cfg.Webhook, s.cfg.Secret)
		if err != nil {
			return err
		}
		webhookURL = signedURL
	}

	text := body
	if s.cfg.Keyword != "" {
		text = s.cfg.Keyword + "\n" + text
	}

	payload := map[string]any{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"title": title,
			"text":  "### " + title + "\n\n" + text,
		},
	}

	data, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, webhookURL, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("钉钉返回状态异常: %s", resp.Status)
	}
	return nil
}

func buildSignedWebhook(webhook, secret string) (string, error) {
	if secret == "" {
		return "", fmt.Errorf("启用加签时必须填写 secret")
	}
	ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
	stringToSign := ts + "\n" + secret
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(stringToSign))
	sign := url.QueryEscape(base64.StdEncoding.EncodeToString(mac.Sum(nil)))

	u, err := url.Parse(webhook)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("timestamp", ts)
	q.Set("sign", sign)
	u.RawQuery = q.Encode()
	return u.String(), nil
}
