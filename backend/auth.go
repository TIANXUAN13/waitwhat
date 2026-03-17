package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type TokenManager struct {
	secret []byte
}

func NewTokenManager(secret string) *TokenManager {
	return &TokenManager{secret: []byte(secret)}
}

func (m *TokenManager) Create(userID int64) (string, error) {
	expiresAt := time.Now().Add(30 * 24 * time.Hour).Unix()
	payload := fmt.Sprintf("%d:%d", userID, expiresAt)
	signature := m.sign(payload)
	raw := payload + ":" + signature
	return base64.RawURLEncoding.EncodeToString([]byte(raw)), nil
}

func (m *TokenManager) Parse(token string) (int64, error) {
	data, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return 0, errors.New("登录已失效，请重新登录")
	}
	parts := strings.Split(string(data), ":")
	if len(parts) != 3 {
		return 0, errors.New("登录已失效，请重新登录")
	}
	payload := parts[0] + ":" + parts[1]
	if !hmac.Equal([]byte(parts[2]), []byte(m.sign(payload))) {
		return 0, errors.New("登录已失效，请重新登录")
	}
	expiresAt, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || time.Now().Unix() > expiresAt {
		return 0, errors.New("登录已失效，请重新登录")
	}
	userID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, errors.New("登录已失效，请重新登录")
	}
	return userID, nil
}

func (m *TokenManager) sign(payload string) string {
	mac := hmac.New(sha256.New, m.secret)
	_, _ = mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

func hashPassword(password string) (string, error) {
	if strings.TrimSpace(password) == "" {
		return "", errors.New("密码不能为空")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	sum := sha256.Sum256(append(salt, []byte(password)...))
	return fmt.Sprintf("%s:%s", hex.EncodeToString(salt), hex.EncodeToString(sum[:])), nil
}

func verifyPassword(stored, password string) bool {
	parts := strings.Split(stored, ":")
	if len(parts) != 2 {
		return false
	}
	salt, err := hex.DecodeString(parts[0])
	if err != nil {
		return false
	}
	expected := parts[1]
	sum := sha256.Sum256(append(salt, []byte(password)...))
	return expected == hex.EncodeToString(sum[:])
}
