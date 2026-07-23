package gateway

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	requestModelHeader   = "X-GRF-Request-Model"
	requestAccountHeader = "X-GRF-Request-Account-ID"
	defaultRequestLogCap = 500
)

type RequestLog struct {
	ID         int64     `json:"id"`
	Timestamp  time.Time `json:"timestamp"`
	Method     string    `json:"method"`
	Path       string    `json:"path"`
	Status     int       `json:"status"`
	DurationMS int64     `json:"duration_ms"`
	Model      string    `json:"model,omitempty"`
	AccountID  int64     `json:"account_id,omitempty"`
	UserAgent  string    `json:"user_agent,omitempty"`
}

type RequestLogBuffer struct {
	mu      sync.RWMutex
	entries []RequestLog
	limit   int
	nextID  int64
}

func NewRequestLogBuffer(limit int) *RequestLogBuffer {
	if limit < 1 {
		limit = defaultRequestLogCap
	}
	return &RequestLogBuffer{limit: limit, entries: make([]RequestLog, 0, limit)}
}

func (b *RequestLogBuffer) Append(entry RequestLog) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if entry.ID == 0 {
		b.nextID++
		entry.ID = b.nextID
	} else if entry.ID > b.nextID {
		b.nextID = entry.ID
	}
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}
	if len(b.entries) == b.limit {
		copy(b.entries, b.entries[1:])
		b.entries[len(b.entries)-1] = entry
		return
	}
	b.entries = append(b.entries, entry)
}

func (b *RequestLogBuffer) Snapshot(limit int) []RequestLog {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if limit <= 0 || limit > len(b.entries) {
		limit = len(b.entries)
	}
	result := make([]RequestLog, limit)
	for i := 0; i < limit; i++ {
		result[i] = b.entries[len(b.entries)-1-i]
	}
	return result
}

func (b *RequestLogBuffer) Clear() {
	b.mu.Lock()
	b.entries = b.entries[:0]
	b.mu.Unlock()
}

func (b *RequestLogBuffer) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		observed := &requestLogResponseWriter{ResponseWriter: w}
		next.ServeHTTP(observed, r)
		status := observed.status
		if status == 0 {
			status = http.StatusOK
		}
		accountID, _ := strconv.ParseInt(r.Header.Get(requestAccountHeader), 10, 64)
		b.Append(RequestLog{
			Timestamp: time.Now().UTC(), Method: r.Method, Path: r.URL.Path,
			Status: status, DurationMS: time.Since(started).Milliseconds(),
			Model: strings.TrimSpace(r.Header.Get(requestModelHeader)), AccountID: accountID,
			UserAgent: sanitizeUserAgent(r.UserAgent()),
		})
	})
}

type requestLogResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *requestLogResponseWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *requestLogResponseWriter) Write(payload []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(payload)
}

func (w *requestLogResponseWriter) Flush() {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func sanitizeUserAgent(value string) string {
	value = strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r == '\t' || r < 0x20 {
			return ' '
		}
		return r
	}, strings.TrimSpace(value))
	runes := []rune(value)
	if len(runes) > 512 {
		value = string(runes[:512])
	}
	return value
}
