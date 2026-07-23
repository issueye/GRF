package gateway

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestImportAccountDocumentAcceptsGRFCPA(t *testing.T) {
	store := openTestStore(t)
	expires := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	document := fmt.Sprintf(`{
		"type":"xai",
		"access_token":"access",
		"refresh_token":"refresh",
		"token_type":"Bearer",
		"expired":%q,
		"sub":"subject-1",
		"email":"user@example.com"
	}`, expires.Format(time.RFC3339))
	accounts, err := store.ImportAccountDocument(context.Background(), []byte(document))
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 1 || accounts[0].UserID != "subject-1" || accounts[0].ExpiresAt == nil || !accounts[0].ExpiresAt.Equal(expires) {
		t.Fatalf("unexpected imported account: %+v", accounts)
	}
}

func TestImportAccountDocumentAcceptsGrok2APIBatch(t *testing.T) {
	store := openTestStore(t)
	document := `{"accounts":[
		{"provider":"grok_build","name":"one","client_id":"client","refresh_token":"refresh-1","user_id":"user-1"},
		{"provider":"grok_build","name":"two","access_token":"access-2","email":"two@example.com","expires_in":3600}
	]}`
	accounts, err := store.ImportAccountDocument(context.Background(), []byte(document))
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 2 || accounts[0].Name != "one" || accounts[1].Name != "two" {
		t.Fatalf("unexpected imported accounts: %+v", accounts)
	}
}

func TestImportAccountDocumentRejectsUnsupportedProvider(t *testing.T) {
	store := openTestStore(t)
	_, err := store.ImportAccountDocument(context.Background(), []byte(`{"provider":"grok_web","access_token":"token"}`))
	if err == nil {
		t.Fatal("expected unsupported provider error")
	}
}
