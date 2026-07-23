package gateway

import (
	"context"
	"errors"
	"testing"
)

func TestSelectorRespectsConcurrencyAndRelease(t *testing.T) {
	store := openTestStore(t)
	account, _, err := store.UpsertAccount(context.Background(), AccountSeed{UserID: "one", AccessToken: "access"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE gateway_accounts SET max_concurrent = 1 WHERE id = ?`, account.ID); err != nil {
		t.Fatal(err)
	}
	selector := NewSelector(store)
	_, release, err := selector.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := selector.Acquire(context.Background()); !errors.Is(err, ErrNoAvailableAccount) {
		t.Fatalf("second acquire error = %v", err)
	}
	release()
	_, release, err = selector.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	release()
	release()
}
