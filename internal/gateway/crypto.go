package gateway

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const credentialKeySize = 32

type credentialCipher struct {
	aead cipher.AEAD
}

func loadOrCreateCipher(path string) (*credentialCipher, error) {
	key, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, fmt.Errorf("create gateway key directory: %w", err)
		}
		key = make([]byte, credentialKeySize)
		if _, err := io.ReadFull(rand.Reader, key); err != nil {
			return nil, fmt.Errorf("generate gateway credential key: %w", err)
		}
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, key, 0o600); err != nil {
			return nil, fmt.Errorf("write gateway credential key: %w", err)
		}
		if err := os.Rename(tmp, path); err != nil {
			_ = os.Remove(tmp)
			return nil, fmt.Errorf("install gateway credential key: %w", err)
		}
		_ = os.Chmod(path, 0o600)
	} else if err != nil {
		return nil, fmt.Errorf("read gateway credential key: %w", err)
	}
	if len(key) != credentialKeySize {
		return nil, fmt.Errorf("gateway credential key must contain %d bytes", credentialKeySize)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create gateway credential cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create gateway credential AEAD: %w", err)
	}
	return &credentialCipher{aead: aead}, nil
}

func (c *credentialCipher) encrypt(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate credential nonce: %w", err)
	}
	sealed := c.aead.Seal(nonce, nonce, []byte(value), nil)
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

func (c *credentialCipher) decrypt(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return "", fmt.Errorf("decode encrypted credential: %w", err)
	}
	nonceSize := c.aead.NonceSize()
	if len(raw) <= nonceSize {
		return "", fmt.Errorf("encrypted credential is truncated")
	}
	plain, err := c.aead.Open(nil, raw[:nonceSize], raw[nonceSize:], nil)
	if err != nil {
		return "", fmt.Errorf("decrypt credential: %w", err)
	}
	return string(plain), nil
}
