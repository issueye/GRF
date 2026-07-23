package gateway

import (
	"bufio"
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
		Stream              bool            `json:"stream"`
		Temperature         *float64        `json:"temperature,omitempty"`
		MaxTokens           *int            `json:"max_tokens,omitempty"`
		MaxCompletionTokens *int            `json:"max_completion_tokens,omitempty"`
	}
	if json.Unmarshal(body, &request) != nil || strings.TrimSpace(request.Model) == "" || len(request.Messages) == 0 {
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "A valid model and messages are required")
		return
	}
	input, err := normalizeMessageInput(request.Messages)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "messages format is invalid")
		return
	}
	payload := map[string]any{"model": request.Model, "input": input, "stream": request.Stream}
	if request.Temperature != nil {
		payload["temperature"] = *request.Temperature
	}
	if request.MaxCompletionTokens != nil {
		payload["max_output_tokens"] = *request.MaxCompletionTokens
	} else if request.MaxTokens != nil {
		payload["max_output_tokens"] = *request.MaxTokens
	}
	m.proxyCompatibility(w, r, payload, request.Stream, false)
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
		Stream      bool            `json:"stream"`
		Temperature *float64        `json:"temperature,omitempty"`
	}
	if json.Unmarshal(body, &request) != nil || strings.TrimSpace(request.Model) == "" || len(request.Messages) == 0 || request.MaxTokens < 1 {
		writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", "model, messages, and max_tokens are required")
		return
	}
	input, err := normalizeMessageInput(request.Messages)
	if err != nil {
		writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", "messages format is invalid")
		return
	}
	payload := map[string]any{"model": request.Model, "input": input, "max_output_tokens": request.MaxTokens, "stream": request.Stream}
	if request.System != nil {
		payload["instructions"] = request.System
	}
	if request.Temperature != nil {
		payload["temperature"] = *request.Temperature
	}
	m.proxyCompatibility(w, r, payload, request.Stream, true)
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
	proxyRequest.Body = http.NoBody
	proxyRequest.Body = ioNopCloser{bytes.NewReader(body)}
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
	if stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		if anthropic {
			writeAnthropicStream(w, responseBody, payload["model"].(string))
		} else {
			writeChatStream(w, responseBody, payload["model"].(string))
		}
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

func responseDeltas(body []byte) []string {
	var deltas []string
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 4096), 4<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			continue
		}
		var event struct{ Type, Delta string }
		if json.Unmarshal([]byte(data), &event) == nil && event.Type == "response.output_text.delta" && event.Delta != "" {
			deltas = append(deltas, event.Delta)
		}
	}
	return deltas
}

func writeChatStream(w http.ResponseWriter, body []byte, model string) {
	id := "chatcmpl-" + randomIdentifier()
	created := time.Now().Unix()
	for i, delta := range responseDeltas(body) {
		role := map[string]any{"content": delta}
		if i == 0 {
			role["role"] = "assistant"
		}
		writeSSEData(w, map[string]any{"id": id, "object": "chat.completion.chunk", "created": created, "model": model, "choices": []any{map[string]any{"index": 0, "delta": role, "finish_reason": nil}}})
	}
	writeSSEData(w, map[string]any{"id": id, "object": "chat.completion.chunk", "created": created, "model": model, "choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}}})
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
}

func writeAnthropicStream(w http.ResponseWriter, body []byte, model string) {
	id := "msg_" + randomIdentifier()
	writeSSE(w, "message_start", map[string]any{"type": "message_start", "message": map[string]any{"id": id, "type": "message", "role": "assistant", "model": model, "content": []any{}, "stop_reason": nil, "usage": map[string]any{"input_tokens": 0, "output_tokens": 0}}})
	writeSSE(w, "content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}})
	for _, delta := range responseDeltas(body) {
		writeSSE(w, "content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": delta}})
	}
	writeSSE(w, "content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
	writeSSE(w, "message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil}, "usage": map[string]any{"output_tokens": 0}})
	writeSSE(w, "message_stop", map[string]any{"type": "message_stop"})
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
