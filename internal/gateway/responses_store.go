package gateway

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

func (s *Store) SaveResponse(ctx context.Context, id string, apiKeyID int64, payload []byte) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `INSERT INTO gateway_responses (id, api_key_id, payload, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET payload = excluded.payload,
		api_key_id = excluded.api_key_id, expires_at = excluded.expires_at`, id, apiKeyID, payload,
		formatTime(now), formatTime(now.Add(7*24*time.Hour)))
	if err != nil {
		return fmt.Errorf("save response: %w", err)
	}
	return nil
}

func (s *Store) GetResponse(ctx context.Context, id string, apiKeyID int64) ([]byte, error) {
	var payload []byte
	err := s.db.QueryRowContext(ctx, `SELECT payload FROM gateway_responses
		WHERE id = ? AND api_key_id = ? AND expires_at > ?`, id, apiKeyID, formatTime(time.Now().UTC())).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get response: %w", err)
	}
	return payload, nil
}

func (s *Store) DeleteResponse(ctx context.Context, id string, apiKeyID int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM gateway_responses WHERE id = ? AND api_key_id = ?`, id, apiKeyID)
	if err != nil {
		return fmt.Errorf("delete response: %w", err)
	}
	return requireChanged(result)
}
