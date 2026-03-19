package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
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
	iterations := 120000
	derived := pbkdf2SHA256([]byte(password), salt, iterations, 32)
	return fmt.Sprintf("pbkdf2$sha256$%d$%s$%s", iterations, hex.EncodeToString(salt), hex.EncodeToString(derived)), nil
}

func verifyPassword(stored, password string) bool {
	ok, _ := verifyPasswordWithScheme(stored, password)
	return ok
}

func needsPasswordRehash(stored string) bool {
	_, scheme := verifyPasswordWithScheme(stored, "dummy-never-match")
	return scheme != "pbkdf2"
}

func verifyPasswordWithScheme(stored, password string) (bool, string) {
	if strings.HasPrefix(stored, "pbkdf2$sha256$") {
		parts := strings.Split(stored, "$")
		if len(parts) != 5 {
			return false, "pbkdf2"
		}
		iterations, err := strconv.Atoi(parts[2])
		if err != nil || iterations < 10000 {
			return false, "pbkdf2"
		}
		salt, err := hex.DecodeString(parts[3])
		if err != nil {
			return false, "pbkdf2"
		}
		expected, err := hex.DecodeString(parts[4])
		if err != nil {
			return false, "pbkdf2"
		}
		derived := pbkdf2SHA256([]byte(password), salt, iterations, len(expected))
		return subtle.ConstantTimeCompare(expected, derived) == 1, "pbkdf2"
	}

	// Legacy format: saltHex:sha256Hex
	parts := strings.Split(stored, ":")
	if len(parts) != 2 {
		return false, "unknown"
	}
	salt, err := hex.DecodeString(parts[0])
	if err != nil {
		return false, "legacy"
	}
	expected := parts[1]
	sum := sha256.Sum256(append(salt, []byte(password)...))
	return subtle.ConstantTimeCompare([]byte(expected), []byte(hex.EncodeToString(sum[:]))) == 1, "legacy"
}

func pbkdf2SHA256(password, salt []byte, iterations, keyLen int) []byte {
	if iterations <= 0 {
		iterations = 1
	}
	hLen := 32
	numBlocks := (keyLen + hLen - 1) / hLen
	out := make([]byte, 0, numBlocks*hLen)

	for block := 1; block <= numBlocks; block++ {
		u := pbkdf2F(password, salt, iterations, block)
		out = append(out, u...)
	}
	return out[:keyLen]
}

func pbkdf2F(password, salt []byte, iterations, blockIndex int) []byte {
	mac := hmac.New(sha256.New, password)
	mac.Write(salt)
	mac.Write([]byte{
		byte(blockIndex >> 24),
		byte(blockIndex >> 16),
		byte(blockIndex >> 8),
		byte(blockIndex),
	})
	u := mac.Sum(nil)
	t := make([]byte, len(u))
	copy(t, u)

	for i := 2; i <= iterations; i++ {
		mac = hmac.New(sha256.New, password)
		mac.Write(u)
		u = mac.Sum(nil)
		for j := range t {
			t[j] ^= u[j]
		}
	}
	return t
}
