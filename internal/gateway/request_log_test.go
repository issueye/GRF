package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
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
	if usage := logs.TokenUsage(); usage.RequestCount != 5 {
		t.Fatalf("request count = %+v", usage)
	}
	logs.Clear()
	if snapshot := logs.Snapshot(10); len(snapshot) != 0 {
		t.Fatalf("snapshot after clear = %+v", snapshot)
	}
	if usage := logs.TokenUsage(); usage != (TokenUsage{}) {
		t.Fatalf("usage after clear = %+v", usage)
	}
}

func TestRequestLogBufferAccumulatesTokenUsage(t *testing.T) {
	logs := NewRequestLogBuffer(2)
	logs.Append(RequestLog{InputTokens: 10, OutputTokens: 4, TotalTokens: 14})
	logs.Append(RequestLog{InputTokens: 3, OutputTokens: 7, TotalTokens: 10})
	// Ring buffer drops first entry but cumulative totals remain.
	logs.Append(RequestLog{InputTokens: 1, OutputTokens: 1, TotalTokens: 2})
	usage := logs.TokenUsage()
	if usage.RequestCount != 3 || usage.InputTokens != 14 || usage.OutputTokens != 12 || usage.TotalTokens != 26 {
		t.Fatalf("usage = %+v", usage)
	}
	if len(logs.Snapshot(10)) != 2 {
		t.Fatalf("buffer size = %d", len(logs.Snapshot(10)))
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

func TestRequestLogMiddlewareCapturesJSONUsage(t *testing.T) {
	logs := NewRequestLogBuffer(10)
	handler := logs.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set(requestModelHeader, "grok-4.5")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"resp_1","model":"grok-4.5","usage":{"input_tokens":12,"output_tokens":8,"total_tokens":20}}`))
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/responses", nil))
	entry := logs.Snapshot(1)[0]
	if entry.InputTokens != 12 || entry.OutputTokens != 8 || entry.TotalTokens != 20 {
		t.Fatalf("tokens = in=%d out=%d total=%d", entry.InputTokens, entry.OutputTokens, entry.TotalTokens)
	}
}

func TestRequestLogMiddlewareCapturesChatCompletionsUsage(t *testing.T) {
	logs := NewRequestLogBuffer(10)
	handler := logs.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"chat","usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`))
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
	entry := logs.Snapshot(1)[0]
	if entry.InputTokens != 3 || entry.OutputTokens != 2 || entry.TotalTokens != 5 {
		t.Fatalf("tokens = %+v", entry)
	}
}

func TestRequestLogMiddlewareCapturesSSEUsage(t *testing.T) {
	logs := NewRequestLogBuffer(10)
	handler := logs.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if flusher, ok := w.(http.Flusher); ok {
			_, _ = w.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\n"))
			flusher.Flush()
			_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":9,\"output_tokens\":4,\"total_tokens\":13}}}\n\n"))
			flusher.Flush()
		}
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/responses", nil))
	entry := logs.Snapshot(1)[0]
	if entry.InputTokens != 9 || entry.OutputTokens != 4 || entry.TotalTokens != 13 {
		t.Fatalf("sse tokens = %+v", entry)
	}
}

func TestExtractUsageTokensVariants(t *testing.T) {
	cases := []struct {
		name                 string
		body                 string
		input, output, total int
	}{
		{"empty", "", 0, 0, 0},
		{"anthropic", `{"usage":{"input_tokens":1,"output_tokens":2}}`, 1, 2, 3},
		{"openai chat", `{"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`, 10, 5, 15},
		{"sse done only", "data: [DONE]\n\n", 0, 0, 0},
		{"sse usage line", "event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"input_tokens\":7,\"output_tokens\":1}}\n\n", 7, 1, 8},
	}
	for _, tc := range cases {
		in, out, tot := extractUsageTokens([]byte(tc.body))
		if in != tc.input || out != tc.output || tot != tc.total {
			t.Fatalf("%s: got %d/%d/%d want %d/%d/%d", tc.name, in, out, tot, tc.input, tc.output, tc.total)
		}
	}
}

func TestRequestLogDoesNotCaptureModelsListBody(t *testing.T) {
	logs := NewRequestLogBuffer(10)
	handler := logs.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Large-ish body that must not be buffered for non-inference routes.
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"grok"}],"usage":{"input_tokens":99,"output_tokens":1}}`))
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	entry := logs.Snapshot(1)[0]
	if entry.InputTokens != 0 || entry.OutputTokens != 0 {
		t.Fatalf("models list should not capture usage: %+v", entry)
	}
}

func TestExtractUsageFromTruncatedJSONFragment(t *testing.T) {
	body := []byte(strings.Repeat("x", 100) + `,"usage":{"input_tokens":21,"output_tokens":3},"trailing":`)
	in, out, tot := extractUsageTokens(body)
	if in != 21 || out != 3 || tot != 24 {
		t.Fatalf("fragment = %d/%d/%d", in, out, tot)
	}
}
