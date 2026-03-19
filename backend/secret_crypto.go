package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
)

const secretPrefix = "enc:v1:"

func encryptSecret(plainText, masterSecret string) (string, error) {
	plainText = strings.TrimSpace(plainText)
	if plainText == "" {
		return "", nil
	}
	if strings.TrimSpace(masterSecret) == "" {
		return "", errors.New("缺少加密主密钥")
	}
	key := sha256.Sum256([]byte(masterSecret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	cipherText := gcm.Seal(nil, nonce, []byte(plainText), nil)
	return secretPrefix + base64.RawURLEncoding.EncodeToString(nonce) + ":" + base64.RawURLEncoding.EncodeToString(cipherText), nil
}

func decryptSecret(rawText, masterSecret string) (string, error) {
	rawText = strings.TrimSpace(rawText)
	if rawText == "" {
		return "", nil
	}
	if !strings.HasPrefix(rawText, secretPrefix) {
		// 兼容历史明文数据
		return rawText, nil
	}
	if strings.TrimSpace(masterSecret) == "" {
		return "", errors.New("缺少解密主密钥")
	}

	payload := strings.TrimPrefix(rawText, secretPrefix)
	parts := strings.SplitN(payload, ":", 2)
	if len(parts) != 2 {
		return "", errors.New("密文格式不正确")
	}
	nonce, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", err
	}
	cipherText, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", err
	}
	key := sha256.Sum256([]byte(masterSecret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	plain, err := gcm.Open(nil, nonce, cipherText, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
