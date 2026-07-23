package gateway

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestResponsesForwardsBuildRequest(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("upstream path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer access-token" || r.Header.Get("X-XAI-Token-Auth") != "xai-grok-cli" {
			t.Fatalf("upstream headers = %+v", r.Header)
		}
		if r.Header.Get("x-grok-client-version") != buildClientVersion || r.Header.Get("x-grok-model-override") != "grok-4.5" {
			t.Fatalf("Build identity headers = %+v", r.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"resp_1","object":"response","status":"completed"}`)
	}))
	defer upstream.Close()

	store := openTestStore(t)
	_, _, err := store.UpsertAccount(context.Background(), AccountSeed{
		UserID: "user-1", AccessToken: "access-token", RefreshToken: "refresh-token",
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, secret, err := store.CreateAPIKey(context.Background(), "client")
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(store)
	manager.build.BaseURL = upstream.URL + "/v1"
	manager.build.HTTPClient = upstream.Client()
	if err := manager.Start("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Stop(context.Background()) })

	request, _ := http.NewRequest(http.MethodPost, "http://"+manager.Status().Address+"/v1/responses",
		strings.NewReader(`{"model":"grok-4.5","input":"hello"}`))
	request.Header.Set("Authorization", "Bearer "+secret)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(`"resp_1"`)) {
		t.Fatalf("response status=%d body=%s", response.StatusCode, body)
	}
}

func TestResponsesRefreshesExpiredCredential(t *testing.T) {
	var refreshCalls int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			refreshCalls++
			_ = r.ParseForm()
			if r.Form.Get("refresh_token") != "old-refresh" {
				t.Fatalf("refresh token = %q", r.Form.Get("refresh_token"))
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"access_token":"new-access","refresh_token":"new-refresh","expires_in":3600}`)
		case "/v1/responses":
			if r.Header.Get("Authorization") != "Bearer new-access" {
				t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"resp_refresh","status":"completed"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	store := openTestStore(t)
	account, _, err := store.UpsertAccount(context.Background(), AccountSeed{
		UserID: "refresh-user", AccessToken: "old-access", RefreshToken: "old-refresh",
		ExpiresAt: time.Now().UTC().Add(-time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, secret, _ := store.CreateAPIKey(context.Background(), "client")
	manager := NewManager(store)
	manager.build.BaseURL = upstream.URL + "/v1"
	manager.build.TokenURL = upstream.URL + "/token"
	manager.build.HTTPClient = upstream.Client()
	if err := manager.Start("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Stop(context.Background()) })
	request, _ := http.NewRequest(http.MethodPost, "http://"+manager.Status().Address+"/v1/responses",
		strings.NewReader(`{"model":"grok-4.5","input":"hello"}`))
	request.Header.Set("Authorization", "Bearer "+secret)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || refreshCalls != 1 {
		t.Fatalf("status=%d refreshCalls=%d", response.StatusCode, refreshCalls)
	}
	credential, err := store.GetCredential(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if credential.AccessToken != "new-access" || credential.RefreshToken != "new-refresh" {
		t.Fatalf("saved credential = %+v", credential)
	}
}

func TestResponsesStreamsSSE(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = io.WriteString(w, "data: first\n\n")
		flusher.Flush()
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()
	store := openTestStore(t)
	_, _, _ = store.UpsertAccount(context.Background(), AccountSeed{UserID: "stream", AccessToken: "access", ExpiresAt: time.Now().Add(time.Hour)})
	_, secret, _ := store.CreateAPIKey(context.Background(), "client")
	manager := NewManager(store)
	manager.build.BaseURL = upstream.URL + "/v1"
	manager.build.HTTPClient = upstream.Client()
	if err := manager.Start("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Stop(context.Background()) })
	request, _ := http.NewRequest(http.MethodPost, "http://"+manager.Status().Address+"/v1/responses",
		strings.NewReader(`{"model":"grok-4.5","input":"hello","stream":true}`))
	request.Header.Set("Authorization", "Bearer "+secret)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	scanner := bufio.NewScanner(response.Body)
	var lines []string
	for scanner.Scan() {
		if scanner.Text() != "" {
			lines = append(lines, scanner.Text())
		}
	}
	if strings.Join(lines, "|") != "data: first|data: [DONE]" {
		t.Fatalf("SSE lines = %v", lines)
	}
}
