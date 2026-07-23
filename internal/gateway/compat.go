package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"
)

type responseDocument struct {
	ID     string `json:"id"`
	Model  string `json:"model"`
	Output []struct {
		Type    string                        `json:"type"`
		Role    string                        `json:"role"`
		Content []struct{ Type, Text string } `json:"content"`
	} `json:"output"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}

func (m *Manager) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	body, ok := readCompatBody(w, r)
	if !ok {
		return
	}
	var request struct {
		Model               string          `json:"model"`
		Messages            json.RawMessage `json:"messages"`
		Stream              *bool           `json:"stream"`
		Temperature         *float64        `json:"temperature,omitempty"`
		MaxTokens           *int            `json:"max_tokens,omitempty"`
		MaxCompletionTokens *int            `json:"max_completion_tokens,omitempty"`
	}
	if json.Unmarshal(body, &request) != nil || strings.TrimSpace(request.Model) == "" || len(request.Messages) == 0 {
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "A valid model and messages are required")
		return
	}
	r.Header.Set(requestModelHeader, strings.TrimSpace(request.Model))
	input, err := normalizeMessageInput(request.Messages)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "messages format is invalid")
		return
	}
	stream := m.DefaultStream()
	if request.Stream != nil {
		stream = *request.Stream
	}
	payload := map[string]any{"model": request.Model, "input": input, "stream": stream}
	if request.Temperature != nil {
		payload["temperature"] = *request.Temperature
	}
	if request.MaxCompletionTokens != nil {
		payload["max_output_tokens"] = *request.MaxCompletionTokens
	} else if request.MaxTokens != nil {
		payload["max_output_tokens"] = *request.MaxTokens
	}
	m.proxyCompatibility(w, r, payload, stream, false)
}

func (m *Manager) handleMessages(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(r.Header.Get("Anthropic-Version")) == "" {
		writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", "anthropic-version header is required")
		return
	}
	body, ok := readCompatBody(w, r)
	if !ok {
		return
	}
	var request struct {
		Model       string          `json:"model"`
		System      any             `json:"system"`
		Messages    json.RawMessage `json:"messages"`
		MaxTokens   int             `json:"max_tokens"`
		Stream      *bool           `json:"stream"`
		Temperature *float64        `json:"temperature,omitempty"`
	}
	if json.Unmarshal(body, &request) != nil || strings.TrimSpace(request.Model) == "" || len(request.Messages) == 0 || request.MaxTokens < 1 {
		writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", "model, messages, and max_tokens are required")
		return
	}
	r.Header.Set(requestModelHeader, strings.TrimSpace(request.Model))
	input, err := normalizeMessageInput(request.Messages)
	if err != nil {
		writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", "messages format is invalid")
		return
	}
	stream := m.DefaultStream()
	if request.Stream != nil {
		stream = *request.Stream
	}
	payload := map[string]any{"model": request.Model, "input": input, "max_output_tokens": request.MaxTokens, "stream": stream}
	if request.System != nil {
		payload["instructions"] = request.System
	}
	if request.Temperature != nil {
		payload["temperature"] = *request.Temperature
	}
	m.proxyCompatibility(w, r, payload, stream, true)
}

func normalizeMessageInput(raw json.RawMessage) ([]map[string]any, error) {
	var messages []map[string]any
	if err := json.Unmarshal(raw, &messages); err != nil {
		return nil, err
	}
	for _, message := range messages {
		parts, ok := message["content"].([]any)
		if !ok {
			continue
		}
		role, _ := message["role"].(string)
		for index, value := range parts {
			part, ok := value.(map[string]any)
			if !ok {
				continue
			}
			typeValue, _ := part["type"].(string)
			switch typeValue {
			case "text":
				if role == "assistant" {
					part["type"] = "output_text"
				} else {
					part["type"] = "input_text"
				}
			case "image_url":
				part["type"] = "input_image"
				if image, ok := part["image_url"].(map[string]any); ok {
					part["image_url"] = image["url"]
				}
			case "image":
				if source, ok := part["source"].(map[string]any); ok && source["type"] == "base64" {
					media, _ := source["media_type"].(string)
					data, _ := source["data"].(string)
					part = map[string]any{"type": "input_image", "image_url": "data:" + media + ";base64," + data}
					parts[index] = part
				}
			}
		}
		message["content"] = parts
	}
	return messages, nil
}

func readCompatBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxInferenceBodyBytes)
	var buffer bytes.Buffer
	if _, err := buffer.ReadFrom(r.Body); err != nil {
		writeAPIError(w, http.StatusRequestEntityTooLarge, "request_too_large", "Request body exceeds 32 MiB")
		return nil, false
	}
	return buffer.Bytes(), true
}

func (m *Manager) proxyCompatibility(w http.ResponseWriter, r *http.Request, payload map[string]any, stream, anthropic bool) {
	body, _ := json.Marshal(payload)
	proxyRequest := r.Clone(r.Context())
	proxyRequest.Header = r.Header
	proxyRequest.Body = http.NoBody
	proxyRequest.Body = ioNopCloser{bytes.NewReader(body)}
	if stream {
		streamWriter := newCompatibilityStreamWriter(w, payload["model"].(string), anthropic)
		m.handleResponses(streamWriter, proxyRequest)
		streamWriter.Finish()
		return
	}
	recorder := httptest.NewRecorder()
	m.handleResponses(recorder, proxyRequest)
	result := recorder.Result()
	defer result.Body.Close()
	responseBody := recorder.Body.Bytes()
	if result.StatusCode < 200 || result.StatusCode >= 300 {
		for key, values := range result.Header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(result.StatusCode)
		_, _ = w.Write(responseBody)
		return
	}
	var document responseDocument
	if err := json.Unmarshal(responseBody, &document); err != nil {
		writeAPIError(w, http.StatusBadGateway, "upstream_error", "Upstream response format is invalid")
		return
	}
	text := responseText(document)
	if anthropic {
		writeJSON(w, http.StatusOK, map[string]any{"id": document.ID, "type": "message", "role": "assistant", "model": document.Model, "content": []any{map[string]any{"type": "text", "text": text}}, "stop_reason": "end_turn", "stop_sequence": nil, "usage": map[string]any{"input_tokens": document.Usage.InputTokens, "output_tokens": document.Usage.OutputTokens}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": document.ID, "object": "chat.completion", "created": time.Now().Unix(), "model": document.Model, "choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": text}, "finish_reason": "stop"}}, "usage": map[string]any{"prompt_tokens": document.Usage.InputTokens, "completion_tokens": document.Usage.OutputTokens, "total_tokens": document.Usage.TotalTokens}})
}

type ioNopCloser struct{ *bytes.Reader }

func (ioNopCloser) Close() error { return nil }

func responseText(document responseDocument) string {
	var text strings.Builder
	for _, output := range document.Output {
		for _, content := range output.Content {
			if content.Type == "output_text" || content.Type == "text" {
				text.WriteString(content.Text)
			}
		}
	}
	return text.String()
}

type compatibilityStreamWriter struct {
	target    http.ResponseWriter
	model     string
	anthropic bool
	status    int
	buffer    string
	started   bool
	finished  bool
	id        string
	created   int64
}

func newCompatibilityStreamWriter(target http.ResponseWriter, model string, anthropic bool) *compatibilityStreamWriter {
	prefix := "chatcmpl-"
	if anthropic {
		prefix = "msg_"
	}
	return &compatibilityStreamWriter{target: target, model: model, anthropic: anthropic, id: prefix + randomIdentifier(), created: time.Now().Unix()}
}

func (w *compatibilityStreamWriter) Header() http.Header { return w.target.Header() }

func (w *compatibilityStreamWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	if status >= 200 && status < 300 {
		w.target.Header().Set("Content-Type", "text/event-stream")
		w.target.Header().Set("Cache-Control", "no-cache")
	}
	w.target.WriteHeader(status)
	if status >= 200 && status < 300 && w.anthropic {
		w.startAnthropic()
	}
}

func (w *compatibilityStreamWriter) Write(payload []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if w.status < 200 || w.status >= 300 {
		return w.target.Write(payload)
	}
	w.buffer += string(payload)
	for {
		index := strings.IndexByte(w.buffer, '\n')
		if index < 0 {
			break
		}
		line := strings.TrimSpace(w.buffer[:index])
		w.buffer = w.buffer[index+1:]
		w.consume(line)
	}
	return len(payload), nil
}

func (w *compatibilityStreamWriter) Flush() {
	if flusher, ok := w.target.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *compatibilityStreamWriter) Finish() {
	if strings.TrimSpace(w.buffer) != "" {
		w.consume(strings.TrimSpace(w.buffer))
		w.buffer = ""
	}
	if w.status >= 200 && w.status < 300 {
		w.finishStream()
	}
	w.Flush()
}

func (w *compatibilityStreamWriter) consume(line string) {
	if !strings.HasPrefix(line, "data:") {
		return
	}
	data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if data == "[DONE]" {
		w.finishStream()
		return
	}
	var event struct {
		Type  string `json:"type"`
		Delta string `json:"delta"`
	}
	if json.Unmarshal([]byte(data), &event) != nil {
		return
	}
	if event.Type == "response.output_text.delta" && event.Delta != "" {
		w.writeDelta(event.Delta)
	}
	if event.Type == "response.completed" || event.Type == "response.failed" {
		w.finishStream()
	}
}

func (w *compatibilityStreamWriter) writeDelta(delta string) {
	if w.anthropic {
		w.startAnthropic()
		writeSSE(w.target, "content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": delta}})
	} else {
		content := map[string]any{"content": delta}
		if !w.started {
			content["role"] = "assistant"
			w.started = true
		}
		writeSSEData(w.target, map[string]any{"id": w.id, "object": "chat.completion.chunk", "created": w.created, "model": w.model, "choices": []any{map[string]any{"index": 0, "delta": content, "finish_reason": nil}}})
	}
	w.Flush()
}

func (w *compatibilityStreamWriter) startAnthropic() {
	if w.started {
		return
	}
	w.started = true
	writeSSE(w.target, "message_start", map[string]any{"type": "message_start", "message": map[string]any{"id": w.id, "type": "message", "role": "assistant", "model": w.model, "content": []any{}, "stop_reason": nil, "usage": map[string]any{"input_tokens": 0, "output_tokens": 0}}})
	writeSSE(w.target, "content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}})
	w.Flush()
}

func (w *compatibilityStreamWriter) finishStream() {
	if w.finished {
		return
	}
	w.finished = true
	if w.anthropic {
		w.startAnthropic()
		writeSSE(w.target, "content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
		writeSSE(w.target, "message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil}, "usage": map[string]any{"output_tokens": 0}})
		writeSSE(w.target, "message_stop", map[string]any{"type": "message_stop"})
	} else {
		writeSSEData(w.target, map[string]any{"id": w.id, "object": "chat.completion.chunk", "created": w.created, "model": w.model, "choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}}})
		_, _ = fmt.Fprint(w.target, "data: [DONE]\n\n")
	}
	w.Flush()
}

func writeSSEData(w http.ResponseWriter, value any) {
	raw, _ := json.Marshal(value)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", raw)
}
func writeSSE(w http.ResponseWriter, event string, value any) {
	raw, _ := json.Marshal(value)
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, raw)
}
func writeAnthropicError(w http.ResponseWriter, status int, kind, message string) {
	writeJSON(w, status, map[string]any{"type": "error", "error": map[string]any{"type": kind, "message": message}})
}
