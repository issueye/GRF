package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRequestLogBufferBoundsAndClears(t *testing.T) {
	logs := NewRequestLogBuffer(3)
	for i := 1; i <= 5; i++ {
		logs.Append(RequestLog{ID: int64(i), Timestamp: time.Now().UTC()})
	}
	snapshot := logs.Snapshot(10)
	if len(snapshot) != 3 || snapshot[0].ID != 5 || snapshot[2].ID != 3 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	logs.Clear()
	if snapshot := logs.Snapshot(10); len(snapshot) != 0 {
		t.Fatalf("snapshot after clear = %+v", snapshot)
	}
}

func TestRequestLogMiddlewareCapturesStatusAndSupportsFlush(t *testing.T) {
	logs := NewRequestLogBuffer(10)
	handler := logs.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := w.(http.Flusher); !ok {
			t.Fatal("wrapped response writer does not implement http.Flusher")
		}
		r.Header.Set(requestModelHeader, "grok-4.5")
		r.Header.Set(requestAccountHeader, "42")
		w.WriteHeader(http.StatusBadGateway)
	}))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/responses?debug=1", nil)
	request.Header.Set("User-Agent", "GRF-Test/1.0")
	handler.ServeHTTP(recorder, request)
	snapshot := logs.Snapshot(1)
	if len(snapshot) != 1 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	entry := snapshot[0]
	if entry.Method != http.MethodPost || entry.Path != "/v1/responses" || entry.Status != http.StatusBadGateway || entry.Model != "grok-4.5" || entry.AccountID != 42 || entry.UserAgent != "GRF-Test/1.0" {
		t.Fatalf("entry = %+v", entry)
	}
	if entry.DurationMS < 0 {
		t.Fatalf("duration = %d", entry.DurationMS)
	}
}
