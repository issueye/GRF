package gateway

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

var ErrNoAvailableAccount = errors.New("no available Build account")

type Selector struct {
	store  *Store
	mu     sync.Mutex
	leases map[int64]int
}

func NewSelector(store *Store) *Selector {
	return &Selector{store: store, leases: make(map[int64]int)}
}

func (s *Selector) Acquire(ctx context.Context) (Credential, func(), error) {
	accounts, err := s.store.ListAccounts(ctx)
	if err != nil {
		return Credential{}, nil, err
	}
	now := time.Now().UTC()
	sort.SliceStable(accounts, func(i, j int) bool {
		if accounts[i].LastUsedAt == nil {
			return accounts[j].LastUsedAt != nil
		}
		if accounts[j].LastUsedAt == nil {
			return false
		}
		return accounts[i].LastUsedAt.Before(*accounts[j].LastUsedAt)
	})
	s.mu.Lock()
	var selected Account
	for _, account := range accounts {
		if !account.Enabled || account.AuthStatus != AuthActive {
			continue
		}
		if account.CooldownUntil != nil && account.CooldownUntil.After(now) {
			continue
		}
		limit := account.MaxConcurrent
		if limit <= 0 {
			limit = 1
		}
		if s.leases[account.ID] >= limit {
			continue
		}
		selected = account
		s.leases[account.ID]++
		break
	}
	s.mu.Unlock()
	if selected.ID == 0 {
		return Credential{}, nil, ErrNoAvailableAccount
	}
	released := false
	release := func() {
		s.mu.Lock()
		if !released {
			released = true
			s.leases[selected.ID]--
			if s.leases[selected.ID] <= 0 {
				delete(s.leases, selected.ID)
			}
		}
		s.mu.Unlock()
	}
	credential, err := s.store.GetCredential(ctx, selected.ID)
	if err != nil {
		release()
		return Credential{}, nil, err
	}
	return credential, release, nil
}
