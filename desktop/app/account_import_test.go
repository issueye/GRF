package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grok-free-register/grok-reg/internal/gateway"
)

func TestImportGatewayAccountFilesContinuesAfterInvalidFiles(t *testing.T) {
	dir := t.TempDir()
	store, err := gateway.OpenStore(context.Background(), filepath.Join(dir, "gateway.db"), filepath.Join(dir, "gateway.key"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	validPath := filepath.Join(dir, "valid.json")
	invalidPath := filepath.Join(dir, "invalid.json")
	textPath := filepath.Join(dir, "account.txt")
	if err := os.WriteFile(validPath, []byte(`{"type":"xai","access_token":"access","sub":"user-1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(invalidPath, []byte(`{"type":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(textPath, []byte(`{"type":"xai","access_token":"other"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	result := importGatewayAccountFiles(context.Background(), store, []string{invalidPath, validPath, textPath})
	if result.SelectedFiles != 3 || result.SuccessfulFiles != 1 || result.FailedFiles != 2 || result.ImportedAccounts != 1 {
		t.Fatalf("unexpected import result: %+v", result)
	}
	accounts, err := store.ListAccounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 1 || accounts[0].UserID != "user-1" {
		t.Fatalf("unexpected imported accounts: %+v", accounts)
	}
}

func TestImportGatewayAccountFilesReportsSelectionLimit(t *testing.T) {
	dir := t.TempDir()
	store, err := gateway.OpenStore(context.Background(), filepath.Join(dir, "gateway.db"), filepath.Join(dir, "gateway.key"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	paths := make([]string, 501)
	for index := range paths {
		paths[index] = filepath.Join(dir, "unsupported.txt")
	}
	result := importGatewayAccountFiles(context.Background(), store, paths)
	if result.SelectedFiles != 501 || result.FailedFiles != 501 {
		t.Fatalf("unexpected limited import result: %+v", result)
	}
	if len(result.Failures) != 501 || !strings.Contains(result.Failures[0].Error, "1 个文件未处理") {
		t.Fatalf("expected selection limit failure first: %+v", result.Failures)
	}
}
