package gateway

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	store, err := OpenStore(context.Background(), filepath.Join(dir, "gateway.db"), filepath.Join(dir, "credential.key"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestStoreMigratesAndUpsertsEncryptedAccount(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	expires := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	account, created, err := store.UpsertAccount(ctx, AccountSeed{
		Name: "Primary", Email: "USER@example.com", UserID: "user-1", ClientID: "client-1",
		AccessToken: "access-one", RefreshToken: "refresh-one", ExpiresAt: expires,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !created || account.ID == 0 || account.Provider != ProviderBuild {
		t.Fatalf("unexpected account: created=%v account=%+v", created, account)
	}
	var rawAccess, rawRefresh string
	if err := store.db.QueryRowContext(ctx, `SELECT access_token, refresh_token FROM gateway_accounts WHERE id = ?`, account.ID).Scan(&rawAccess, &rawRefresh); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rawAccess, "access-one") || strings.Contains(rawRefresh, "refresh-one") {
		t.Fatal("database contains plaintext credentials")
	}
	updated, created, err := store.UpsertAccount(ctx, AccountSeed{
		Name: "Updated", Email: "user@example.com", UserID: "user-1", ClientID: "client-1",
		AccessToken: "access-two", RefreshToken: "refresh-two", ExpiresAt: expires.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if created || updated.ID != account.ID || updated.Name != "Updated" {
		t.Fatalf("upsert did not update existing account: created=%v account=%+v", created, updated)
	}
	credential, err := store.GetCredential(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if credential.AccessToken != "access-two" || credential.RefreshToken != "refresh-two" {
		t.Fatalf("decrypted credential mismatch: %+v", credential)
	}
	accounts, err := store.ListAccounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 1 {
		t.Fatalf("account count = %d", len(accounts))
	}
}

func TestAPIKeyLifecycle(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	key, secret, err := store.CreateAPIKey(ctx, "Local client")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(secret, "grf_") || key.Prefix == secret || !strings.HasPrefix(secret, key.Prefix) {
		t.Fatalf("unexpected key metadata: key=%+v secret=%q", key, secret)
	}
	var stored string
	if err := store.db.QueryRowContext(ctx, `SELECT CAST(secret_hash AS TEXT) FROM gateway_api_keys WHERE id = ?`, key.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored, secret) {
		t.Fatal("database contains plaintext API key")
	}
	verified, err := store.VerifyAPIKey(ctx, secret)
	if err != nil || verified.ID != key.ID || verified.LastUsedAt == nil {
		t.Fatalf("verify key: value=%+v err=%v", verified, err)
	}
	if err := store.SetAPIKeyEnabled(ctx, key.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := store.VerifyAPIKey(ctx, secret); !isNotFound(err) {
		t.Fatalf("disabled key verification error = %v", err)
	}
	if err := store.DeleteAPIKey(ctx, key.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteAPIKey(ctx, key.ID); !isNotFound(err) {
		t.Fatalf("second delete error = %v", err)
	}
}
