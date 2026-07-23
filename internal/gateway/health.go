package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

const (
	healthCheckWorkers = 4
	healthCheckTimeout = 15 * time.Second
)

type HealthCheckError struct {
	StatusCode int
}

func (e *HealthCheckError) Error() string {
	return fmt.Sprintf("Build model catalog returned HTTP %d", e.StatusCode)
}

func (e *HealthCheckError) AuthFailed() bool {
	return e.StatusCode == http.StatusUnauthorized || e.StatusCode == http.StatusForbidden
}

func (m *Manager) CheckAccounts(ctx context.Context) (AccountHealthSummary, error) {
	if !m.healthRunning.CompareAndSwap(false, true) {
		return AccountHealthSummary{}, fmt.Errorf("account health check is already running")
	}
	defer m.healthRunning.Store(false)
	if ctx == nil {
		ctx = context.Background()
	}
	summary := AccountHealthSummary{StartedAt: time.Now().UTC()}
	accounts, err := m.store.ListAccounts(ctx)
	if err != nil {
		return summary, err
	}
	jobs := make(chan int64)
	results := make(chan bool)
	var workers sync.WaitGroup
	for range healthCheckWorkers {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for id := range jobs {
				results <- m.checkAccount(ctx, id)
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, account := range accounts {
			if !account.Enabled {
				continue
			}
			select {
			case jobs <- account.ID:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		workers.Wait()
		close(results)
	}()
	for healthy := range results {
		summary.Checked++
		if healthy {
			summary.Healthy++
		} else {
			summary.Unhealthy++
		}
	}
	summary.CompletedAt = time.Now().UTC()
	if ctx.Err() != nil {
		return summary, ctx.Err()
	}
	return summary, nil
}

func (m *Manager) checkAccount(parent context.Context, id int64) bool {
	ctx, cancel := context.WithTimeout(parent, healthCheckTimeout)
	defer cancel()
	credential, err := m.store.GetCredential(ctx, id)
	if err != nil {
		return false
	}
	originalAccessToken := credential.AccessToken
	started := time.Now()
	credential, err = m.build.CheckHealth(ctx, credential)
	latency := time.Since(started)
	if ctx.Err() != nil {
		return false
	}
	if credential.AccessToken != originalAccessToken && credential.ExpiresAt != nil {
		if saveErr := m.store.UpdateCredentialTokens(ctx, credential.ID, credential.AccessToken, credential.RefreshToken, *credential.ExpiresAt); saveErr != nil && err == nil {
			err = saveErr
		}
	}
	var checkErr *HealthCheckError
	authFailed := errors.As(err, &checkErr) && checkErr.AuthFailed()
	message := ""
	if err != nil {
		message = err.Error()
	}
	if updateErr := m.store.UpdateAccountHealth(context.Background(), id, err == nil, latency, message, authFailed); updateErr != nil {
		return false
	}
	return err == nil
}

func (m *Manager) ConfigureHealthChecks(enabled bool, interval time.Duration) error {
	m.healthMu.Lock()
	if m.healthCancel != nil {
		m.healthCancel()
		m.healthCancel = nil
	}
	if !enabled {
		m.healthMu.Unlock()
		return nil
	}
	if interval < 5*time.Minute || interval > 24*time.Hour {
		m.healthMu.Unlock()
		return fmt.Errorf("account health check interval must be between 5 minutes and 24 hours")
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.healthCancel = cancel
	m.healthMu.Unlock()
	go func() {
		_, _ = m.CheckAccounts(ctx)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_, _ = m.CheckAccounts(ctx)
			case <-ctx.Done():
				return
			}
		}
	}()
	return nil
}

func (m *Manager) StopHealthChecks() {
	m.healthMu.Lock()
	if m.healthCancel != nil {
		m.healthCancel()
		m.healthCancel = nil
	}
	m.healthMu.Unlock()
}
