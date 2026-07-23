package gateway

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestManagerCheckAccountsRecordsHealthyAndUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("X-XAI-Token-Auth") != "xai-grok-cli" {
			t.Fatalf("unexpected health request: %s headers=%v", r.URL.Path, r.Header)
		}
		switch r.Header.Get("Authorization") {
		case "Bearer healthy-token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"data":[{"id":"grok-4.5"}]}`)
		case "Bearer dead-token":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = fmt.Fprint(w, `{"error":"invalid_token"}`)
		default:
			t.Fatalf("unexpected authorization header %q", r.Header.Get("Authorization"))
		}
	}))
	defer server.Close()

	store := openTestStore(t)
	_, _, err := store.UpsertAccount(context.Background(), AccountSeed{UserID: "healthy", AccessToken: "healthy-token"})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = store.UpsertAccount(context.Background(), AccountSeed{UserID: "dead", AccessToken: "dead-token"})
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(store)
	manager.build.BaseURL = server.URL + "/v1"
	manager.build.HTTPClient = server.Client()
	summary, err := manager.CheckAccounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if summary.Checked != 2 || summary.Healthy != 1 || summary.Unhealthy != 1 || summary.CompletedAt.IsZero() {
		t.Fatalf("unexpected health summary: %+v", summary)
	}
	accounts, err := store.ListAccounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	states := make(map[string]Account, len(accounts))
	for _, account := range accounts {
		states[account.UserID] = account
	}
	if states["healthy"].HealthStatus != HealthHealthy || states["healthy"].AuthStatus != AuthActive {
		t.Fatalf("healthy account state: %+v", states["healthy"])
	}
	if states["dead"].HealthStatus != HealthFailed || states["dead"].AuthStatus != AuthRequired || states["dead"].HealthError == "" {
		t.Fatalf("unauthorized account state: %+v", states["dead"])
	}
}

func TestConfigureHealthChecksValidatesInterval(t *testing.T) {
	manager := NewManager(openTestStore(t))
	if err := manager.ConfigureHealthChecks(true, 0); err == nil {
		t.Fatal("expected interval validation error")
	}
	if err := manager.ConfigureHealthChecks(false, 0); err != nil {
		t.Fatalf("disabled health checks should ignore interval: %v", err)
	}
}

func TestManagerCheckAccountPersistsCanceledRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	store := openTestStore(t)
	account, _, err := store.UpsertAccount(context.Background(), AccountSeed{UserID: "timeout", AccessToken: "timeout-token"})
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(store)
	manager.build.BaseURL = server.URL + "/v1"
	manager.build.HTTPClient = server.Client()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if manager.checkAccount(ctx, account.ID) {
		t.Fatal("canceled health check was reported as healthy")
	}

	accounts, err := store.ListAccounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 1 || accounts[0].HealthStatus != HealthFailed || accounts[0].LastCheckedAt == nil {
		t.Fatalf("canceled health state was not persisted: %+v", accounts)
	}
	if !strings.Contains(strings.ToLower(accounts[0].HealthError), "deadline") {
		t.Fatalf("unexpected canceled health error: %q", accounts[0].HealthError)
	}
}
