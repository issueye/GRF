package gateway

import (
	"context"
	"database/sql"
	"fmt"
)

const schemaVersion = 3

var schemaV1 = []string{
	`CREATE TABLE IF NOT EXISTS gateway_accounts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		source_key TEXT NOT NULL UNIQUE,
		provider TEXT NOT NULL,
		name TEXT NOT NULL DEFAULT '',
		email TEXT NOT NULL DEFAULT '',
		user_id TEXT NOT NULL DEFAULT '',
		client_id TEXT NOT NULL DEFAULT '',
		access_token TEXT NOT NULL DEFAULT '',
		refresh_token TEXT NOT NULL DEFAULT '',
		expires_at TEXT NOT NULL DEFAULT '',
		enabled INTEGER NOT NULL DEFAULT 1,
		auth_status TEXT NOT NULL DEFAULT 'active',
		max_concurrent INTEGER NOT NULL DEFAULT 8,
		failure_count INTEGER NOT NULL DEFAULT 0,
		cooldown_until TEXT NOT NULL DEFAULT '',
		last_used_at TEXT NOT NULL DEFAULT '',
		observed_model TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS idx_gateway_accounts_routing
		ON gateway_accounts(provider, enabled, auth_status, cooldown_until, last_used_at)`,
	`CREATE TABLE IF NOT EXISTS gateway_api_keys (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		prefix TEXT NOT NULL,
		secret_hash BLOB NOT NULL UNIQUE,
		enabled INTEGER NOT NULL DEFAULT 1,
		created_at TEXT NOT NULL,
		last_used_at TEXT NOT NULL DEFAULT ''
	)`,
	`CREATE INDEX IF NOT EXISTS idx_gateway_api_keys_prefix ON gateway_api_keys(prefix)`,
	`CREATE TABLE IF NOT EXISTS gateway_models (
		id TEXT PRIMARY KEY,
		upstream_model TEXT NOT NULL,
		capabilities TEXT NOT NULL DEFAULT '',
		enabled INTEGER NOT NULL DEFAULT 1,
		updated_at TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS gateway_responses (
		id TEXT PRIMARY KEY,
		api_key_id INTEGER NOT NULL,
		payload BLOB NOT NULL,
		created_at TEXT NOT NULL,
		expires_at TEXT NOT NULL,
		FOREIGN KEY(api_key_id) REFERENCES gateway_api_keys(id) ON DELETE CASCADE
	)`,
	`CREATE TABLE IF NOT EXISTS gateway_settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`,
}

var schemaV2 = []string{
	`ALTER TABLE gateway_api_keys ADD COLUMN secret_ciphertext TEXT NOT NULL DEFAULT ''`,
}

var schemaV3 = []string{
	`ALTER TABLE gateway_accounts ADD COLUMN health_status TEXT NOT NULL DEFAULT 'unknown'`,
	`ALTER TABLE gateway_accounts ADD COLUMN last_checked_at TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE gateway_accounts ADD COLUMN health_latency_ms INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE gateway_accounts ADD COLUMN health_error TEXT NOT NULL DEFAULT ''`,
}

func migrate(ctx context.Context, db *sql.DB) error {
	var version int
	if err := db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("read gateway schema version: %w", err)
	}
	if version > schemaVersion {
		return fmt.Errorf("gateway database schema %d is newer than supported schema %d", version, schemaVersion)
	}
	if version == schemaVersion {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin gateway migration: %w", err)
	}
	defer tx.Rollback()
	migrations := []struct {
		version    int
		statements []string
	}{{1, schemaV1}, {2, schemaV2}, {3, schemaV3}}
	for _, migration := range migrations {
		if version >= migration.version {
			continue
		}
		for _, statement := range migration.statements {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("apply gateway schema v%d: %w", migration.version, err)
			}
		}
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, schemaVersion)); err != nil {
		return fmt.Errorf("write gateway schema version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit gateway migration: %w", err)
	}
	return nil
}
