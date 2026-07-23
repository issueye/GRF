package gateway

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
)

const apiKeyPrefix = "grf_"

func (s *Store) CreateAPIKey(ctx context.Context, name string) (APIKey, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return APIKey{}, "", fmt.Errorf("API key name is required")
	}
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return APIKey{}, "", fmt.Errorf("generate API key: %w", err)
	}
	secret := apiKeyPrefix + base64.RawURLEncoding.EncodeToString(random)
	hash := sha256.Sum256([]byte(secret))
	encryptedSecret, err := s.cipher.encrypt(secret)
	if err != nil {
		return APIKey{}, "", fmt.Errorf("encrypt API key: %w", err)
	}
	prefix := secret
	if len(prefix) > 12 {
		prefix = prefix[:12]
	}
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `INSERT INTO gateway_api_keys
		(name, prefix, secret_hash, secret_ciphertext, enabled, created_at) VALUES (?, ?, ?, ?, 1, ?)`,
		name, prefix, hash[:], encryptedSecret, formatTime(now))
	if err != nil {
		return APIKey{}, "", fmt.Errorf("create API key: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return APIKey{}, "", fmt.Errorf("read API key id: %w", err)
	}
	return APIKey{ID: id, Name: name, Prefix: prefix, Enabled: true, CreatedAt: now, HasSecret: true}, secret, nil
}

func (s *Store) ListAPIKeys(ctx context.Context) ([]APIKey, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, prefix, enabled, created_at, last_used_at,
		CASE WHEN secret_ciphertext <> '' THEN 1 ELSE 0 END
		FROM gateway_api_keys ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list API keys: %w", err)
	}
	defer rows.Close()
	keys := make([]APIKey, 0)
	for rows.Next() {
		var key APIKey
		var enabled, hasSecret int
		var createdAt, lastUsedAt string
		if err := rows.Scan(&key.ID, &key.Name, &key.Prefix, &enabled, &createdAt, &lastUsedAt, &hasSecret); err != nil {
			return nil, fmt.Errorf("scan API key: %w", err)
		}
		key.Enabled = enabled != 0
		key.CreatedAt = parseTime(createdAt)
		key.LastUsedAt = parseOptionalTime(lastUsedAt)
		key.HasSecret = hasSecret != 0
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func (s *Store) GetAPIKeySecret(ctx context.Context, id int64) (string, error) {
	var encryptedSecret string
	err := s.db.QueryRowContext(ctx, `SELECT secret_ciphertext FROM gateway_api_keys WHERE id = ?`, id).Scan(&encryptedSecret)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && encryptedSecret == "") {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("get API key secret: %w", err)
	}
	secret, err := s.cipher.decrypt(encryptedSecret)
	if err != nil {
		return "", fmt.Errorf("decrypt API key secret: %w", err)
	}
	return secret, nil
}

func (s *Store) VerifyAPIKey(ctx context.Context, secret string) (APIKey, error) {
	if !strings.HasPrefix(secret, apiKeyPrefix) {
		return APIKey{}, ErrNotFound
	}
	hash := sha256.Sum256([]byte(secret))
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, prefix, secret_hash, enabled, created_at, last_used_at
		FROM gateway_api_keys WHERE prefix = ?`, visibleKeyPrefix(secret))
	if err != nil {
		return APIKey{}, fmt.Errorf("verify API key: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key APIKey
		var storedHash []byte
		var enabled int
		var createdAt, lastUsedAt string
		if err := rows.Scan(&key.ID, &key.Name, &key.Prefix, &storedHash, &enabled, &createdAt, &lastUsedAt); err != nil {
			return APIKey{}, fmt.Errorf("scan API key verification: %w", err)
		}
		if subtle.ConstantTimeCompare(storedHash, hash[:]) != 1 {
			continue
		}
		if enabled == 0 {
			return APIKey{}, ErrNotFound
		}
		now := time.Now().UTC()
		if _, err := s.db.ExecContext(ctx, `UPDATE gateway_api_keys SET last_used_at = ? WHERE id = ?`, formatTime(now), key.ID); err != nil {
			return APIKey{}, fmt.Errorf("update API key use: %w", err)
		}
		key.Enabled = true
		key.CreatedAt = parseTime(createdAt)
		key.LastUsedAt = &now
		return key, nil
	}
	if err := rows.Err(); err != nil {
		return APIKey{}, err
	}
	return APIKey{}, ErrNotFound
}

func (s *Store) SetAPIKeyEnabled(ctx context.Context, id int64, enabled bool) error {
	value := 0
	if enabled {
		value = 1
	}
	result, err := s.db.ExecContext(ctx, `UPDATE gateway_api_keys SET enabled = ? WHERE id = ?`, value, id)
	if err != nil {
		return fmt.Errorf("update API key: %w", err)
	}
	return requireChanged(result)
}

func (s *Store) DeleteAPIKey(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM gateway_api_keys WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete API key: %w", err)
	}
	return requireChanged(result)
}

func visibleKeyPrefix(secret string) string {
	if len(secret) <= 12 {
		return secret
	}
	return secret[:12]
}

func requireChanged(result sql.Result) error {
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return ErrNotFound
	}
	return nil
}

func isNotFound(err error) bool {
	return errors.Is(err, ErrNotFound) || errors.Is(err, sql.ErrNoRows)
}
