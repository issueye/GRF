package gateway

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestMigrateV1APIKeysRemainNonRecoverable(t *testing.T) {
	dir := t.TempDir()
	databasePath := filepath.Join(dir, "gateway.db")
	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range schemaV1 {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO gateway_api_keys (name, prefix, secret_hash, enabled, created_at) VALUES ('legacy', 'grf_legacy12', X'01', 1, '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA user_version = 1`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	store, err := OpenStore(context.Background(), databasePath, filepath.Join(dir, "credential.key"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	keys, err := store.ListAPIKeys(context.Background())
	if err != nil || len(keys) != 1 || keys[0].HasSecret {
		t.Fatalf("legacy keys = %+v, err = %v", keys, err)
	}
	if _, err := store.GetAPIKeySecret(context.Background(), keys[0].ID); !isNotFound(err) {
		t.Fatalf("legacy secret error = %v", err)
	}
}
