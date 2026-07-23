package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type flushCaptureWriter struct {
	header  http.Header
	body    bytes.Buffer
	status  int
	flushes int
}

func (w *flushCaptureWriter) Header() http.Header             { return w.header }
func (w *flushCaptureWriter) WriteHeader(status int)          { w.status = status }
func (w *flushCaptureWriter) Write(value []byte) (int, error) { return w.body.Write(value) }
func (w *flushCaptureWriter) Flush()                          { w.flushes++ }

func newCompatibilityManager(t *testing.T, upstream http.Handler) (*Manager, string) {
	t.Helper()
	server := httptest.NewServer(upstream)
	t.Cleanup(server.Close)
	store := openTestStore(t)
	_, _, err := store.UpsertAccount(context.Background(), AccountSeed{UserID: "compat", AccessToken: "access", ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	_, secret, err := store.CreateAPIKey(context.Background(), "client")
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(store)
	manager.build.BaseURL = server.URL + "/v1"
	manager.build.HTTPClient = server.Client()
	return manager, secret
}

func TestChatCompletionsConvertsResponses(t *testing.T) {
	manager, secret := newCompatibilityManager(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if r.URL.Path != "/v1/responses" || payload["input"] == nil {
			t.Fatalf("request path=%s payload=%v", r.URL.Path, payload)
		}
		_, _ = io.WriteString(w, `{"id":"resp_chat","model":"grok-4.5","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}}`)
	}))
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"grok-4.5","messages":[{"role":"user","content":"hi"}]}`))
	request.Header.Set("Authorization", "Bearer "+secret)
	recorder := httptest.NewRecorder()
	manager.routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"content":"hello"`) {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestChatCompletionsConvertsFunctionTools(t *testing.T) {
	manager, secret := newCompatibilityManager(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		tools, _ := payload["tools"].([]any)
		tool, _ := tools[0].(map[string]any)
		choice, _ := payload["tool_choice"].(map[string]any)
		if len(tools) != 1 || tool["type"] != "function" || tool["name"] != "get_weather" || choice["name"] != "get_weather" {
			t.Fatalf("upstream tool payload = %#v", payload)
		}
		_, _ = io.WriteString(w, `{"id":"resp_tool","model":"grok-4.5","output":[{"type":"function_call","call_id":"call_1","name":"get_weather","arguments":"{\"city\":\"Shanghai\"}"}]}`)
	}))
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{
		"model":"grok-4.5","messages":[{"role":"user","content":"weather"}],
		"tools":[{"type":"function","function":{"name":"get_weather","description":"Weather","parameters":{"type":"object"}}}],
		"tool_choice":{"type":"function","function":{"name":"get_weather"}}
	}`))
	request.Header.Set("Authorization", "Bearer "+secret)
	recorder := httptest.NewRecorder()
	manager.routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"finish_reason":"tool_calls"`) || !strings.Contains(recorder.Body.String(), `"name":"get_weather"`) || !strings.Contains(recorder.Body.String(), `"id":"call_1"`) {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestCompatibilityNormalizesToolResultHistory(t *testing.T) {
	input, err := normalizeMessageInput(json.RawMessage(`[
		{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},
		{"role":"tool","tool_call_id":"call_1","content":"result"}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(input) != 2 || input[0]["type"] != "function_call" || input[0]["name"] != "lookup" || input[1]["type"] != "function_call_output" || input[1]["output"] != "result" {
		t.Fatalf("normalized tool history = %#v", input)
	}
}

func TestAnthropicMessagesSupportsXAPIKey(t *testing.T) {
	manager, secret := newCompatibilityManager(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"id":"resp_msg","model":"grok-4.5","output":[{"type":"message","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":3,"output_tokens":2}}`)
	}))
	request := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"grok-4.5","max_tokens":32,"messages":[{"role":"user","content":"hi"}]}`))
	request.Header.Set("X-API-Key", secret)
	request.Header.Set("Anthropic-Version", "2023-06-01")
	recorder := httptest.NewRecorder()
	manager.routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"type":"message"`) {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestAnthropicMessagesConvertsTools(t *testing.T) {
	manager, secret := newCompatibilityManager(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		tools, _ := payload["tools"].([]any)
		tool, _ := tools[0].(map[string]any)
		if len(tools) != 1 || tool["name"] != "lookup" || tool["parameters"] == nil || payload["tool_choice"] != "required" {
			t.Fatalf("upstream Anthropic tool payload = %#v", payload)
		}
		_, _ = io.WriteString(w, `{"id":"resp_msg_tool","model":"grok-4.5","output":[{"type":"function_call","call_id":"tool_1","name":"lookup","arguments":"{\"id\":7}"}]}`)
	}))
	request := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{
		"model":"grok-4.5","max_tokens":64,"messages":[{"role":"user","content":"lookup"}],
		"tools":[{"name":"lookup","description":"Lookup","input_schema":{"type":"object"}}],
		"tool_choice":{"type":"any"}
	}`))
	request.Header.Set("X-API-Key", secret)
	request.Header.Set("Anthropic-Version", "2023-06-01")
	recorder := httptest.NewRecorder()
	manager.routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"stop_reason":"tool_use"`) || !strings.Contains(recorder.Body.String(), `"type":"tool_use"`) || !strings.Contains(recorder.Body.String(), `"name":"lookup"`) {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestStoredResponseIsScopedToAPIKey(t *testing.T) {
	manager, secret := newCompatibilityManager(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"id":"resp_saved","status":"completed"}`)
	}))
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"grok-4.5","input":"hi","store":true}`))
	request.Header.Set("Authorization", "Bearer "+secret)
	recorder := httptest.NewRecorder()
	manager.routes().ServeHTTP(recorder, request)
	get := httptest.NewRequest(http.MethodGet, "/v1/responses/resp_saved", nil)
	get.Header.Set("Authorization", "Bearer "+secret)
	getRecorder := httptest.NewRecorder()
	manager.routes().ServeHTTP(getRecorder, get)
	if getRecorder.Code != http.StatusOK || !strings.Contains(getRecorder.Body.String(), "resp_saved") {
		t.Fatalf("status=%d body=%s", getRecorder.Code, getRecorder.Body.String())
	}
}

func TestCompatibilityStreamWriterFlushesBeforeFinish(t *testing.T) {
	target := &flushCaptureWriter{header: make(http.Header)}
	writer := newCompatibilityStreamWriter(target, "grok-4.5", false)
	writer.WriteHeader(http.StatusOK)
	chunk := []byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n")
	if _, err := writer.Write(chunk); err != nil {
		t.Fatal(err)
	}
	writer.Flush()
	if target.flushes == 0 || !strings.Contains(target.body.String(), `"content":"hello"`) || strings.Contains(target.body.String(), "[DONE]") {
		t.Fatalf("before finish: flushes=%d body=%s", target.flushes, target.body.String())
	}
	writer.Finish()
	if !strings.Contains(target.body.String(), "[DONE]") {
		t.Fatalf("after finish body=%s", target.body.String())
	}
}

func TestCompatibilityStreamWriterConvertsFunctionCall(t *testing.T) {
	target := &flushCaptureWriter{header: make(http.Header)}
	writer := newCompatibilityStreamWriter(target, "grok-4.5", false)
	writer.WriteHeader(http.StatusOK)
	stream := strings.Join([]string{
		`data: {"type":"response.output_item.added","item":{"id":"item_1","type":"function_call","call_id":"call_1","name":"lookup","arguments":""}}`,
		`data: {"type":"response.function_call_arguments.delta","item_id":"item_1","delta":"{\"id\":"}`,
		`data: {"type":"response.function_call_arguments.delta","item_id":"item_1","delta":"7}"}`,
		`data: {"type":"response.output_item.done","item":{"id":"item_1","type":"function_call","call_id":"call_1","name":"lookup","arguments":"{\"id\":7}"}}`,
		`data: {"type":"response.completed"}`,
		`data: [DONE]`,
		``,
	}, "\n\n")
	if _, err := writer.Write([]byte(stream)); err != nil {
		t.Fatal(err)
	}
	writer.Finish()
	body := target.body.String()
	for _, expected := range []string{`"tool_calls"`, `"name":"lookup"`, `"arguments":"{\"id\":"`, `"finish_reason":"tool_calls"`, `data: [DONE]`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("stream missing %s: %s", expected, body)
		}
	}
}

func TestApplyDefaultStreamRespectsExplicitValue(t *testing.T) {
	defaulted, err := applyDefaultStream([]byte(`{"model":"grok-4.5"}`), true)
	if err != nil || !bytes.Contains(defaulted, []byte(`"stream":true`)) {
		t.Fatalf("defaulted=%s err=%v", defaulted, err)
	}
	explicit, err := applyDefaultStream([]byte(`{"model":"grok-4.5","stream":false}`), true)
	if err != nil || !bytes.Contains(explicit, []byte(`"stream":false`)) {
		t.Fatalf("explicit=%s err=%v", explicit, err)
	}
}
