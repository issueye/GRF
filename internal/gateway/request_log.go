package gateway

import (
	"bytes"
	"encoding/json"
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
	// Cap buffered response body used only for token extraction (not stored).
	maxUsageCaptureBytes = 1 << 20
)

type RequestLog struct {
	ID           int64     `json:"id"`
	Timestamp    time.Time `json:"timestamp"`
	Method       string    `json:"method"`
	Path         string    `json:"path"`
	Status       int       `json:"status"`
	DurationMS   int64     `json:"duration_ms"`
	Model        string    `json:"model,omitempty"`
	AccountID    int64     `json:"account_id,omitempty"`
	UserAgent    string    `json:"user_agent,omitempty"`
	InputTokens  int       `json:"input_tokens,omitempty"`
	OutputTokens int       `json:"output_tokens,omitempty"`
	TotalTokens  int       `json:"total_tokens,omitempty"`
}

// TokenUsage is a cumulative token summary since process start (or last log clear).
type TokenUsage struct {
	RequestCount int64 `json:"request_count"`
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	TotalTokens  int64 `json:"total_tokens"`
}

type RequestLogBuffer struct {
	mu      sync.RWMutex
	entries []RequestLog
	limit   int
	nextID  int64
	usage   TokenUsage
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
	if entry.TotalTokens == 0 && (entry.InputTokens > 0 || entry.OutputTokens > 0) {
		entry.TotalTokens = entry.InputTokens + entry.OutputTokens
	}
	b.usage.RequestCount++
	b.usage.InputTokens += int64(entry.InputTokens)
	b.usage.OutputTokens += int64(entry.OutputTokens)
	b.usage.TotalTokens += int64(entry.TotalTokens)
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
	b.usage = TokenUsage{}
	b.mu.Unlock()
}

func (b *RequestLogBuffer) TokenUsage() TokenUsage {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.usage
}

func (b *RequestLogBuffer) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		observed := &requestLogResponseWriter{ResponseWriter: w, capture: shouldCaptureUsage(r.URL.Path)}
		next.ServeHTTP(observed, r)
		status := observed.status
		if status == 0 {
			status = http.StatusOK
		}
		accountID, _ := strconv.ParseInt(r.Header.Get(requestAccountHeader), 10, 64)
		inputTokens, outputTokens, totalTokens := observed.usageTokens()
		b.Append(RequestLog{
			Timestamp: time.Now().UTC(), Method: r.Method, Path: r.URL.Path,
			Status: status, DurationMS: time.Since(started).Milliseconds(),
			Model: strings.TrimSpace(r.Header.Get(requestModelHeader)), AccountID: accountID,
			UserAgent: sanitizeUserAgent(r.UserAgent()),
			InputTokens: inputTokens, OutputTokens: outputTokens, TotalTokens: totalTokens,
		})
	})
}

func shouldCaptureUsage(path string) bool {
	switch path {
	case "/v1/responses", "/v1/responses/compact", "/v1/chat/completions", "/v1/messages":
		return true
	default:
		return strings.HasPrefix(path, "/v1/responses/")
	}
}

type requestLogResponseWriter struct {
	http.ResponseWriter
	status       int
	capture      bool
	body         bytes.Buffer // non-SSE JSON bodies
	lineBuf      []byte       // incomplete SSE line
	inputTokens  int
	outputTokens int
	totalTokens  int
	sawSSE       bool
	truncated    bool
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
	if w.capture {
		w.observe(payload)
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

func (w *requestLogResponseWriter) observe(payload []byte) {
	// Detect SSE early so large streams do not fill the JSON buffer.
	if !w.sawSSE && (bytes.Contains(payload, []byte("data:")) || bytes.HasPrefix(bytes.TrimSpace(payload), []byte("data:"))) {
		w.sawSSE = true
		// Re-scan anything already buffered as potential SSE.
		if w.body.Len() > 0 {
			w.consumeSSE(w.body.Bytes())
			w.body.Reset()
		}
	}
	if w.sawSSE {
		w.consumeSSE(payload)
		return
	}
	if w.truncated {
		return
	}
	remaining := maxUsageCaptureBytes - w.body.Len()
	if remaining <= 0 {
		w.truncated = true
		return
	}
	if len(payload) <= remaining {
		_, _ = w.body.Write(payload)
		return
	}
	_, _ = w.body.Write(payload[:remaining])
	w.truncated = true
}

func (w *requestLogResponseWriter) consumeSSE(payload []byte) {
	w.lineBuf = append(w.lineBuf, payload...)
	// Bound incomplete-line buffer growth.
	if len(w.lineBuf) > maxUsageCaptureBytes {
		w.lineBuf = w.lineBuf[len(w.lineBuf)-maxUsageCaptureBytes:]
	}
	for {
		index := bytes.IndexByte(w.lineBuf, '\n')
		if index < 0 {
			break
		}
		line := bytes.TrimSpace(w.lineBuf[:index])
		w.lineBuf = w.lineBuf[index+1:]
		w.observeSSELine(line)
	}
}

func (w *requestLogResponseWriter) observeSSELine(line []byte) {
	if !bytes.HasPrefix(line, []byte("data:")) {
		return
	}
	data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
	if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
		return
	}
	if in, out, tot, ok := extractUsageFromJSON(data); ok {
		w.inputTokens, w.outputTokens, w.totalTokens = in, out, tot
		return
	}
	var envelope struct {
		Response json.RawMessage `json:"response"`
		Message  json.RawMessage `json:"message"`
		Usage    json.RawMessage `json:"usage"`
	}
	if json.Unmarshal(data, &envelope) != nil {
		return
	}
	for _, raw := range []json.RawMessage{envelope.Usage, envelope.Response, envelope.Message} {
		if len(raw) == 0 {
			continue
		}
		if in, out, tot, ok := extractUsageFromJSON(raw); ok {
			w.inputTokens, w.outputTokens, w.totalTokens = in, out, tot
		} else if in, out, tot, ok := extractUsageFromJSONFragment(raw); ok {
			w.inputTokens, w.outputTokens, w.totalTokens = in, out, tot
		}
	}
}

func (w *requestLogResponseWriter) usageTokens() (input, output, total int) {
	if w.inputTokens > 0 || w.outputTokens > 0 || w.totalTokens > 0 {
		return w.inputTokens, w.outputTokens, w.totalTokens
	}
	if w.sawSSE {
		// Flush incomplete final line if present.
		if len(w.lineBuf) > 0 {
			w.observeSSELine(bytes.TrimSpace(w.lineBuf))
			w.lineBuf = nil
		}
		return w.inputTokens, w.outputTokens, w.totalTokens
	}
	return extractUsageTokens(w.body.Bytes())
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

// extractUsageTokens parses usage from JSON or SSE response bodies.
// Supports OpenAI Responses/Chat Completions and Anthropic Messages field names.
func extractUsageTokens(body []byte) (input, output, total int) {
	if len(body) == 0 {
		return 0, 0, 0
	}
	if bytes.Contains(body, []byte("data:")) {
		if in, out, tot, ok := extractUsageFromSSE(body); ok {
			return in, out, tot
		}
	}
	if in, out, tot, ok := extractUsageFromJSON(body); ok {
		return in, out, tot
	}
	if in, out, tot, ok := extractUsageFromJSONFragment(body); ok {
		return in, out, tot
	}
	return 0, 0, 0
}

func extractUsageFromSSE(body []byte) (input, output, total int, ok bool) {
	found := false
	for _, line := range bytes.Split(body, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
			continue
		}
		if in, out, tot, parsed := extractUsageFromJSON(data); parsed {
			input, output, total = in, out, tot
			found = true
			continue
		}
		var envelope struct {
			Response json.RawMessage `json:"response"`
			Message  json.RawMessage `json:"message"`
			Usage    json.RawMessage `json:"usage"`
		}
		if json.Unmarshal(data, &envelope) != nil {
			continue
		}
		for _, raw := range []json.RawMessage{envelope.Response, envelope.Message, envelope.Usage} {
			if len(raw) == 0 {
				continue
			}
			if in, out, tot, parsed := extractUsageFromJSON(raw); parsed {
				input, output, total = in, out, tot
				found = true
			}
		}
	}
	return input, output, total, found
}

func extractUsageFromJSON(body []byte) (input, output, total int, ok bool) {
	var document struct {
		Usage *usagePayload `json:"usage"`
	}
	if json.Unmarshal(body, &document) != nil || document.Usage == nil {
		// Raw usage object without wrapper.
		var usage usagePayload
		if json.Unmarshal(body, &usage) == nil {
			return normalizeUsage(usage)
		}
		return 0, 0, 0, false
	}
	return normalizeUsage(*document.Usage)
}

func extractUsageFromJSONFragment(body []byte) (input, output, total int, ok bool) {
	key := []byte(`"usage"`)
	idx := bytes.LastIndex(body, key)
	if idx < 0 {
		return 0, 0, 0, false
	}
	rest := body[idx+len(key):]
	brace := bytes.IndexByte(rest, '{')
	if brace < 0 {
		return 0, 0, 0, false
	}
	obj, okSlice := sliceJSONObject(rest[brace:])
	if !okSlice {
		return 0, 0, 0, false
	}
	var usage usagePayload
	if json.Unmarshal(obj, &usage) != nil {
		return 0, 0, 0, false
	}
	return normalizeUsage(usage)
}

type usagePayload struct {
	InputTokens      int `json:"input_tokens"`
	OutputTokens     int `json:"output_tokens"`
	TotalTokens      int `json:"total_tokens"`
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

func normalizeUsage(u usagePayload) (input, output, total int, ok bool) {
	input = u.InputTokens
	if input == 0 {
		input = u.PromptTokens
	}
	output = u.OutputTokens
	if output == 0 {
		output = u.CompletionTokens
	}
	total = u.TotalTokens
	if total == 0 && (input > 0 || output > 0) {
		total = input + output
	}
	ok = input > 0 || output > 0 || total > 0
	return input, output, total, ok
}

func sliceJSONObject(data []byte) ([]byte, bool) {
	if len(data) == 0 || data[0] != '{' {
		return nil, false
	}
	depth := 0
	inString := false
	escape := false
	for i := 0; i < len(data); i++ {
		c := data[i]
		if inString {
			if escape {
				escape = false
				continue
			}
			if c == '\\' {
				escape = true
				continue
			}
			if c == '"' {
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return data[:i+1], true
			}
		}
	}
	return nil, false
}
