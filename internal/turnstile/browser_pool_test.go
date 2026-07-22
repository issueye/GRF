package turnstile

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakePoolWorker struct {
	started chan struct{}
	release chan struct{}
	err     error
	closed  atomic.Int32
	once    sync.Once
}

func (w *fakePoolWorker) Solve(ctx context.Context, _, _ string) (string, error) {
	if w.started != nil {
		w.once.Do(func() { close(w.started) })
	}
	if w.release != nil {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-w.release:
		}
	}
	if w.err != nil {
		return "", w.err
	}
	return "test-token-long-enough", nil
}

func (w *fakePoolWorker) Close() { w.closed.Add(1) }

func TestNewBrowserPoolClampsWorkers(t *testing.T) {
	pMin := NewBrowserPool("", nil, 0)
	defer pMin.Close()
	if got := pMin.workerCount(); got != 1 {
		t.Fatalf("workers=%d want 1", got)
	}
	p := NewBrowserPool("", nil, 99)
	defer p.Close()
	if got := p.workerCount(); got != 8 {
		t.Fatalf("workers=%d want 8", got)
	}
}

func TestPooledWorkerResetsAfterConsecutiveFailures(t *testing.T) {
	underlying := &fakePoolWorker{err: errors.New("solve failed")}
	w := &pooledBrowserWorker{browser: underlying, resetThreshold: 2}
	for i := 0; i < 2; i++ {
		if _, err := w.Solve(context.Background(), "site-key", ""); err == nil {
			t.Fatalf("solve %d unexpectedly succeeded", i+1)
		}
	}
	if got := underlying.closed.Load(); got != 1 {
		t.Fatalf("browser reset count=%d want 1", got)
	}
	if w.failStreak != 0 {
		t.Fatalf("fail streak=%d want 0 after reset", w.failStreak)
	}
}

func TestBrowserPoolQueueRespectsContext(t *testing.T) {
	w := &fakePoolWorker{started: make(chan struct{}), release: make(chan struct{})}
	p := newTestBrowserPool([]browserPoolWorker{w})
	defer p.Close()

	firstDone := make(chan error, 1)
	go func() {
		_, err := p.Solve(context.Background(), "site-key", "http://example.test")
		firstDone <- err
	}()
	<-w.started

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := p.Solve(ctx, "site-key", "http://example.test")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("queued solve error=%v want deadline exceeded", err)
	}

	close(w.release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first solve: %v", err)
	}
}

func TestBrowserPoolCloseIsIdempotent(t *testing.T) {
	w := &fakePoolWorker{}
	p := newTestBrowserPool([]browserPoolWorker{w})
	p.Close()
	p.Close()
	if got := w.closed.Load(); got != 1 {
		t.Fatalf("worker closed %d times want 1", got)
	}
	if _, err := p.Solve(context.Background(), "site-key", ""); err == nil {
		t.Fatal("solve after close succeeded")
	}
}

func TestBuildInjectJSEscapesSiteKey(t *testing.T) {
	js := buildInjectJS("key'\"\n;window.pwned=true//")
	if js == "" {
		t.Fatal("empty injection script")
	}
	if containsLiteralNewline(js) {
		t.Fatalf("site key introduced literal newline: %q", js)
	}
	if want := `"key'\"\n;window.pwned=true//"`; !contains(js, want) {
		t.Fatalf("site key is not JSON encoded; want fragment %q", want)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func containsLiteralNewline(s string) bool {
	for _, r := range s {
		if r == '\n' || r == '\r' {
			return true
		}
	}
	return false
}
