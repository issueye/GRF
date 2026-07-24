package manualoauth

import (
	"context"
	"testing"
	"time"

	"github.com/grok-free-register/grok-reg/internal/oauth"
)

func TestStoreRoundTripAndPublicView(t *testing.T) {
	store := NewStore(t.TempDir())
	task, err := store.Enqueue("run-1", "person@example.com", "secret-password", "secret-sso")
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Read(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Password != "secret-password" || loaded.SSO != "secret-sso" || loaded.Status != StatusQueued {
		t.Fatalf("unexpected task: %+v", loaded)
	}
	public := Public(loaded)
	if public.Email != loaded.Email || public.Password != "secret-password" || public.Status != StatusQueued {
		t.Fatalf("unexpected public task: %+v", public)
	}
}

func TestWaitAuthorized(t *testing.T) {
	store := NewStore(t.TempDir())
	task, err := store.Enqueue("run-1", "person@example.com", "password", "sso")
	if err != nil {
		t.Fatal(err)
	}
	want := oauth.Credential{AccessToken: "access", RefreshToken: "refresh"}
	go func() {
		time.Sleep(20 * time.Millisecond)
		current, _ := store.Read(task.ID)
		current.Status = StatusAuthorized
		current.Credential = want
		_ = store.Write(current)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, err := store.WaitAuthorized(ctx, task.ID, 5*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != want.AccessToken || got.RefreshToken != want.RefreshToken {
		t.Fatalf("got %+v want %+v", got, want)
	}
}
