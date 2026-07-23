package gateway

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("gateway record not found")

type Store struct {
	db     *sql.DB
	cipher *credentialCipher
}

func OpenStore(ctx context.Context, databasePath, keyPath string) (*Store, error) {
	if strings.TrimSpace(databasePath) == "" || strings.TrimSpace(keyPath) == "" {
		return nil, fmt.Errorf("gateway database and key paths are required")
	}
	if err := os.MkdirAll(filepath.Dir(databasePath), 0o700); err != nil {
		return nil, fmt.Errorf("create gateway data directory: %w", err)
	}
	cipher, err := loadOrCreateCipher(keyPath)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		return nil, fmt.Errorf("open gateway database: %w", err)
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	for _, pragma := range []string{
		`PRAGMA journal_mode = WAL`,
		`PRAGMA busy_timeout = 5000`,
		`PRAGMA foreign_keys = ON`,
		`PRAGMA synchronous = NORMAL`,
	} {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("configure gateway database: %w", err)
		}
	}
	if err := migrate(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db, cipher: cipher}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) UpsertAccount(ctx context.Context, seed AccountSeed) (Account, bool, error) {
	seed.Provider = strings.ToLower(strings.TrimSpace(seed.Provider))
	if seed.Provider == "" {
		seed.Provider = ProviderBuild
	}
	if seed.Provider != ProviderBuild {
		return Account{}, false, fmt.Errorf("unsupported gateway provider %q", seed.Provider)
	}
	seed.AccessToken = strings.TrimSpace(seed.AccessToken)
	seed.RefreshToken = strings.TrimSpace(seed.RefreshToken)
	if seed.AccessToken == "" && seed.RefreshToken == "" {
		return Account{}, false, fmt.Errorf("access token or refresh token is required")
	}
	sourceKey := accountSourceKey(seed)
	accessToken, err := s.cipher.encrypt(seed.AccessToken)
	if err != nil {
		return Account{}, false, err
	}
	refreshToken, err := s.cipher.encrypt(seed.RefreshToken)
	if err != nil {
		return Account{}, false, err
	}
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Account{}, false, err
	}
	defer tx.Rollback()
	var existingID int64
	lookupErr := tx.QueryRowContext(ctx, `SELECT id FROM gateway_accounts WHERE source_key = ?`, sourceKey).Scan(&existingID)
	created := errors.Is(lookupErr, sql.ErrNoRows)
	if lookupErr != nil && !created {
		return Account{}, false, fmt.Errorf("find gateway account: %w", lookupErr)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO gateway_accounts (
		source_key, provider, name, email, user_id, client_id, access_token, refresh_token,
		expires_at, enabled, auth_status, max_concurrent, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, 8, ?, ?)
	ON CONFLICT(source_key) DO UPDATE SET
		name = excluded.name, email = excluded.email, user_id = excluded.user_id,
		client_id = excluded.client_id, access_token = excluded.access_token,
		refresh_token = excluded.refresh_token, expires_at = excluded.expires_at,
		auth_status = ?, failure_count = 0, cooldown_until = '', updated_at = excluded.updated_at`,
		sourceKey, seed.Provider, strings.TrimSpace(seed.Name), strings.TrimSpace(seed.Email),
		strings.TrimSpace(seed.UserID), strings.TrimSpace(seed.ClientID), accessToken, refreshToken,
		formatTime(seed.ExpiresAt), AuthActive, formatTime(now), formatTime(now), AuthActive)
	if err != nil {
		return Account{}, false, fmt.Errorf("upsert gateway account: %w", err)
	}
	var account Account
	if err := scanAccount(tx.QueryRowContext(ctx, accountSelect+` WHERE source_key = ?`, sourceKey), &account); err != nil {
		return Account{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Account{}, false, fmt.Errorf("commit gateway account: %w", err)
	}
	return account, created, nil
}

func (s *Store) ListAccounts(ctx context.Context) ([]Account, error) {
	rows, err := s.db.QueryContext(ctx, accountSelect+` ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list gateway accounts: %w", err)
	}
	defer rows.Close()
	accounts := make([]Account, 0)
	for rows.Next() {
		var account Account
		if err := scanAccount(rows, &account); err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}
	return accounts, rows.Err()
}

func (s *Store) UpdateAccount(ctx context.Context, id int64, name string, enabled bool, maxConcurrent int) error {
	name = strings.TrimSpace(name)
	if maxConcurrent < 1 || maxConcurrent > 64 {
		return fmt.Errorf("max concurrent must be between 1 and 64")
	}
	enabledValue := 0
	if enabled {
		enabledValue = 1
	}
	result, err := s.db.ExecContext(ctx, `UPDATE gateway_accounts SET name = ?, enabled = ?,
		max_concurrent = ?, updated_at = ? WHERE id = ?`, name, enabledValue, maxConcurrent,
		formatTime(time.Now().UTC()), id)
	if err != nil {
		return fmt.Errorf("update gateway account: %w", err)
	}
	return requireChanged(result)
}

func (s *Store) DeleteAccount(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM gateway_accounts WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete gateway account: %w", err)
	}
	return requireChanged(result)
}

func (s *Store) GetCredential(ctx context.Context, id int64) (Credential, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, provider, name, email, user_id, enabled, auth_status,
		max_concurrent, failure_count, cooldown_until, last_used_at, observed_model,
		expires_at, created_at, updated_at, source_key, client_id, access_token, refresh_token
		FROM gateway_accounts WHERE id = ?`, id)
	var credential Credential
	var enabled int
	var expiresAt, cooldownUntil, lastUsedAt, createdAt, updatedAt string
	var encryptedAccess, encryptedRefresh string
	err := row.Scan(
		&credential.ID, &credential.Provider, &credential.Name, &credential.Email, &credential.UserID,
		&enabled, &credential.AuthStatus, &credential.MaxConcurrent, &credential.FailureCount,
		&cooldownUntil, &lastUsedAt, &credential.ObservedModel, &expiresAt, &createdAt, &updatedAt,
		&credential.SourceKey, &credential.ClientID, &encryptedAccess, &encryptedRefresh,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Credential{}, ErrNotFound
	}
	if err != nil {
		return Credential{}, fmt.Errorf("get gateway credential: %w", err)
	}
	credential.Enabled = enabled != 0
	credential.ExpiresAt = parseOptionalTime(expiresAt)
	credential.CooldownUntil = parseOptionalTime(cooldownUntil)
	credential.LastUsedAt = parseOptionalTime(lastUsedAt)
	credential.CreatedAt = parseTime(createdAt)
	credential.UpdatedAt = parseTime(updatedAt)
	credential.AccessToken, err = s.cipher.decrypt(encryptedAccess)
	if err != nil {
		return Credential{}, err
	}
	credential.RefreshToken, err = s.cipher.decrypt(encryptedRefresh)
	if err != nil {
		return Credential{}, err
	}
	return credential, nil
}

func (s *Store) UpdateCredentialTokens(ctx context.Context, id int64, accessToken, refreshToken string, expiresAt time.Time) error {
	encryptedAccess, err := s.cipher.encrypt(strings.TrimSpace(accessToken))
	if err != nil {
		return err
	}
	encryptedRefresh, err := s.cipher.encrypt(strings.TrimSpace(refreshToken))
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE gateway_accounts SET access_token = ?, refresh_token = ?,
		expires_at = ?, auth_status = ?, failure_count = 0, cooldown_until = '', updated_at = ? WHERE id = ?`,
		encryptedAccess, encryptedRefresh, formatTime(expiresAt), AuthActive, formatTime(time.Now().UTC()), id)
	if err != nil {
		return fmt.Errorf("update gateway credential tokens: %w", err)
	}
	return requireChanged(result)
}

func (s *Store) MarkAccountUsed(ctx context.Context, id int64, model string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE gateway_accounts SET last_used_at = ?, observed_model = ?, updated_at = ? WHERE id = ?`,
		formatTime(time.Now().UTC()), strings.TrimSpace(model), formatTime(time.Now().UTC()), id)
	if err != nil {
		return fmt.Errorf("mark gateway account used: %w", err)
	}
	return requireChanged(result)
}

func (s *Store) MarkAccountFailure(ctx context.Context, id int64, permanent bool, cooldown time.Duration) error {
	authStatus := AuthActive
	if permanent {
		authStatus = AuthRequired
	}
	cooldownUntil := ""
	if cooldown > 0 {
		cooldownUntil = formatTime(time.Now().UTC().Add(cooldown))
	}
	result, err := s.db.ExecContext(ctx, `UPDATE gateway_accounts SET failure_count = failure_count + 1,
		auth_status = ?, cooldown_until = ?, updated_at = ? WHERE id = ?`,
		authStatus, cooldownUntil, formatTime(time.Now().UTC()), id)
	if err != nil {
		return fmt.Errorf("mark gateway account failure: %w", err)
	}
	return requireChanged(result)
}

func (s *Store) UpdateAccountHealth(ctx context.Context, id int64, healthy bool, latency time.Duration, healthErr string, authFailed bool) error {
	status := HealthFailed
	authStatus := AuthActive
	if healthy {
		status = HealthHealthy
	} else if authFailed {
		authStatus = AuthRequired
	}
	healthErr = strings.Join(strings.Fields(healthErr), " ")
	if len(healthErr) > 512 {
		healthErr = healthErr[:512]
	}
	latencyMS := latency.Milliseconds()
	if latencyMS < 0 {
		latencyMS = 0
	}
	result, err := s.db.ExecContext(ctx, `UPDATE gateway_accounts SET health_status = ?,
		last_checked_at = ?, health_latency_ms = ?, health_error = ?, auth_status = ?, updated_at = ? WHERE id = ?`,
		status, formatTime(time.Now().UTC()), latencyMS, healthErr, authStatus, formatTime(time.Now().UTC()), id)
	if err != nil {
		return fmt.Errorf("update gateway account health: %w", err)
	}
	return requireChanged(result)
}

const accountSelect = `SELECT id, provider, name, email, user_id, enabled, auth_status,
	max_concurrent, failure_count, cooldown_until, last_used_at, observed_model,
	health_status, last_checked_at, health_latency_ms, health_error,
	expires_at, created_at, updated_at FROM gateway_accounts`

type rowScanner interface{ Scan(...any) error }

func scanAccount(row rowScanner, account *Account) error {
	var enabled int
	var expiresAt, cooldownUntil, lastUsedAt, lastCheckedAt, createdAt, updatedAt string
	if err := row.Scan(
		&account.ID, &account.Provider, &account.Name, &account.Email, &account.UserID,
		&enabled, &account.AuthStatus, &account.MaxConcurrent, &account.FailureCount,
		&cooldownUntil, &lastUsedAt, &account.ObservedModel,
		&account.HealthStatus, &lastCheckedAt, &account.HealthLatency, &account.HealthError,
		&expiresAt, &createdAt, &updatedAt,
	); err != nil {
		return fmt.Errorf("scan gateway account: %w", err)
	}
	account.Enabled = enabled != 0
	account.ExpiresAt = parseOptionalTime(expiresAt)
	account.CooldownUntil = parseOptionalTime(cooldownUntil)
	account.LastUsedAt = parseOptionalTime(lastUsedAt)
	account.LastCheckedAt = parseOptionalTime(lastCheckedAt)
	account.CreatedAt = parseTime(createdAt)
	account.UpdatedAt = parseTime(updatedAt)
	return nil
}

func accountSourceKey(seed AccountSeed) string {
	identity := strings.TrimSpace(seed.UserID)
	if identity == "" {
		identity = strings.ToLower(strings.TrimSpace(seed.Email))
	}
	if identity == "" {
		identity = strings.TrimSpace(seed.RefreshToken)
	}
	if identity == "" {
		identity = strings.TrimSpace(seed.AccessToken)
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{seed.Provider, strings.TrimSpace(seed.ClientID), identity}, "|")))
	return fmt.Sprintf("import:%x", sum[:])
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, value)
	return parsed.UTC()
}

func parseOptionalTime(value string) *time.Time {
	if value == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil
	}
	parsed = parsed.UTC()
	return &parsed
}
