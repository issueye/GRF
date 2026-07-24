package turnstile

import (
	"context"
	"fmt"
	"sync"

	"github.com/grok-free-register/grok-reg/internal/clearance"
)

const defaultWorkerResetThreshold = 2

// browserPoolWorker is deliberately small so pool scheduling and recovery can
// be tested without starting Chromium.
type browserPoolWorker interface {
	Solve(context.Context, string, string) (string, error)
	SolveFull(context.Context, string, string) (SolveResult, error)
	Close()
}

type pooledBrowserWorker struct {
	browser        browserPoolWorker
	failStreak     int
	resetThreshold int
}

func (w *pooledBrowserWorker) Solve(ctx context.Context, siteKey, pageURL string) (string, error) {
	res, err := w.SolveFull(ctx, siteKey, pageURL)
	return res.Token, err
}

func (w *pooledBrowserWorker) SolveFull(ctx context.Context, siteKey, pageURL string) (SolveResult, error) {
	res, err := w.browser.SolveFull(ctx, siteKey, pageURL)
	if err == nil && len(res.Token) > 10 {
		w.failStreak = 0
		return res, nil
	}
	if err == nil {
		err = fmt.Errorf("empty token")
	}
	w.failStreak++
	if w.failStreak >= w.resetThreshold {
		w.browser.Close()
		w.failStreak = 0
	}
	return SolveResult{}, err
}

func (w *pooledBrowserWorker) Close() { w.browser.Close() }

// BrowserPool owns a bounded set of persistent Chromium workers. Each Solve
// borrows exactly one worker, and Browser creates a fresh isolated browser
// context for that request.
type BrowserPool struct {
	workers chan browserPoolWorker
	all     []browserPoolWorker

	mu       sync.Mutex
	closing  bool
	closed   chan struct{}
	close    sync.Once
	inflight sync.WaitGroup
}

func NewBrowserPool(proxy string, cm *clearance.Manager, workers int) *BrowserPool {
	if workers < 1 {
		workers = 1
	}
	if workers > 8 {
		workers = 8
	}
	p := &BrowserPool{
		workers: make(chan browserPoolWorker, workers),
		all:     make([]browserPoolWorker, 0, workers),
		closed:  make(chan struct{}),
	}
	for i := 0; i < workers; i++ {
		worker := &pooledBrowserWorker{
			browser:        NewBrowser(proxy, cm),
			resetThreshold: defaultWorkerResetThreshold,
		}
		p.all = append(p.all, worker)
		p.workers <- worker
	}
	return p
}

func (p *BrowserPool) Name() string { return "go-browser" }

// Warm starts all Chromium workers in parallel so the first registration does
// not pay browser startup latency.
func (p *BrowserPool) Warm(ctx context.Context) error {
	p.mu.Lock()
	if p.closing {
		p.mu.Unlock()
		return fmt.Errorf("browser pool closed")
	}
	p.mu.Unlock()

	errs := make(chan error, len(p.all))
	var wg sync.WaitGroup
	for _, worker := range p.all {
		pooled, ok := worker.(*pooledBrowserWorker)
		if !ok {
			continue
		}
		browser, ok := pooled.browser.(*Browser)
		if !ok {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := browser.ensureBrowser()
			if err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	for err := range errs {
		return fmt.Errorf("warm browser pool: %w", err)
	}
	return nil
}

func (p *BrowserPool) Solve(ctx context.Context, siteKey, pageURL string) (string, error) {
	if siteKey == "" {
		return "", fmt.Errorf("empty site key")
	}

	p.mu.Lock()
	if p.closing {
		p.mu.Unlock()
		return "", fmt.Errorf("browser pool closed")
	}
	p.inflight.Add(1)
	p.mu.Unlock()
	defer p.inflight.Done()

	var worker browserPoolWorker
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-p.closed:
		return "", fmt.Errorf("browser pool closed")
	case worker = <-p.workers:
	}

	defer func() {
		select {
		case p.workers <- worker:
		case <-p.closed:
		}
	}()
	return worker.Solve(ctx, siteKey, pageURL)
}

// SolveFull borrows a worker and returns both the Turnstile token and the
// Castle request token minted in the same browser session.
func (p *BrowserPool) SolveFull(ctx context.Context, siteKey, pageURL string) (SolveResult, error) {
	if siteKey == "" {
		return SolveResult{}, fmt.Errorf("empty site key")
	}

	p.mu.Lock()
	if p.closing {
		p.mu.Unlock()
		return SolveResult{}, fmt.Errorf("browser pool closed")
	}
	p.inflight.Add(1)
	p.mu.Unlock()
	defer p.inflight.Done()

	var worker browserPoolWorker
	select {
	case <-ctx.Done():
		return SolveResult{}, ctx.Err()
	case <-p.closed:
		return SolveResult{}, fmt.Errorf("browser pool closed")
	case worker = <-p.workers:
	}

	defer func() {
		select {
		case p.workers <- worker:
		case <-p.closed:
		}
	}()
	return worker.SolveFull(ctx, siteKey, pageURL)
}

func (p *BrowserPool) Close() {
	p.close.Do(func() {
		p.mu.Lock()
		p.closing = true
		close(p.closed)
		p.mu.Unlock()

		p.inflight.Wait()
		for _, worker := range p.all {
			worker.Close()
		}
	})
}

// workerCount is kept internal for deterministic unit tests and diagnostics.
func (p *BrowserPool) workerCount() int { return len(p.all) }

// Keep the compiler checking that BrowserPool satisfies the public contracts.
var _ Provider = (*BrowserPool)(nil)
var _ Closer = (*BrowserPool)(nil)
var _ Warmer = (*BrowserPool)(nil)

func newTestBrowserPool(workers []browserPoolWorker) *BrowserPool {
	p := &BrowserPool{
		workers: make(chan browserPoolWorker, len(workers)),
		all:     append([]browserPoolWorker(nil), workers...),
		closed:  make(chan struct{}),
	}
	for _, worker := range workers {
		p.workers <- worker
	}
	return p
}
