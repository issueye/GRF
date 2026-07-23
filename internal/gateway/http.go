package gateway

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (m *Manager) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.Handle("GET /v1/models", m.authenticate(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": buildModels()})
	})))
	mux.Handle("POST /v1/responses", m.authenticate(http.HandlerFunc(m.handleResponses)))
	mux.Handle("POST /v1/responses/compact", m.authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { m.handleResponsesPath(w, r, "/responses/compact") })))
	mux.Handle("GET /v1/responses/{response_id}", m.authenticate(http.HandlerFunc(m.handleGetResponse)))
	mux.Handle("DELETE /v1/responses/{response_id}", m.authenticate(http.HandlerFunc(m.handleDeleteResponse)))
	mux.Handle("POST /v1/chat/completions", m.authenticate(http.HandlerFunc(m.handleChatCompletions)))
	mux.Handle("POST /v1/messages", m.authenticate(http.HandlerFunc(m.handleMessages)))
	return m.logs.Middleware(mux)
}

const maxInferenceBodyBytes = 32 << 20

func (m *Manager) handleResponses(w http.ResponseWriter, r *http.Request) {
	m.handleResponsesPath(w, r, "/responses")
}

func (m *Manager) handleResponsesPath(w http.ResponseWriter, r *http.Request, path string) {
	r.Body = http.MaxBytesReader(w, r.Body, maxInferenceBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeAPIError(w, http.StatusRequestEntityTooLarge, "request_too_large", "Request body exceeds 32 MiB")
		return
	}
	if path == "/responses" {
		body, err = applyDefaultStream(body, m.DefaultStream())
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "Request body must be a JSON object")
			return
		}
	}
	var metadata struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
		Store  bool   `json:"store"`
	}
	if err := json.Unmarshal(body, &metadata); err != nil || strings.TrimSpace(metadata.Model) == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "A valid model is required")
		return
	}
	r.Header.Set(requestModelHeader, strings.TrimSpace(metadata.Model))
	credential, release, err := m.selector.Acquire(r.Context())
	if err != nil {
		if errors.Is(err, ErrNoAvailableAccount) {
			writeAPIError(w, http.StatusServiceUnavailable, "no_available_account", err.Error())
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "Account selection failed")
		return
	}
	defer release()
	r.Header.Set(requestAccountHeader, strconv.FormatInt(credential.ID, 10))
	originalAccessToken := credential.AccessToken
	response, credential, err := m.build.Forward(r.Context(), credential, path, body, metadata.Model)
	if err != nil {
		_ = m.store.MarkAccountFailure(r.Context(), credential.ID, false, 15*time.Second)
		writeAPIError(w, http.StatusBadGateway, "upstream_error", err.Error())
		return
	}
	if credential.AccessToken != originalAccessToken && credential.ExpiresAt != nil {
		if err := m.store.UpdateCredentialTokens(r.Context(), credential.ID, credential.AccessToken, credential.RefreshToken, *credential.ExpiresAt); err != nil {
			_ = response.Body.Close()
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "Refreshed credential could not be saved")
			return
		}
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		_ = m.store.MarkAccountFailure(r.Context(), credential.ID, response.StatusCode == http.StatusUnauthorized, time.Minute)
	} else if response.StatusCode == http.StatusTooManyRequests {
		_ = m.store.MarkAccountFailure(r.Context(), credential.ID, false, time.Minute)
	} else if response.StatusCode >= 200 && response.StatusCode < 300 {
		_ = m.store.MarkAccountUsed(r.Context(), credential.ID, metadata.Model)
	}
	copyResponseHeaders(w.Header(), response.Header)
	w.WriteHeader(response.StatusCode)
	if metadata.Stream {
		if flusher, ok := w.(http.Flusher); ok {
			buffer := make([]byte, 32*1024)
			for {
				n, readErr := response.Body.Read(buffer)
				if n > 0 {
					_, _ = w.Write(buffer[:n])
					flusher.Flush()
				}
				if readErr != nil {
					break
				}
			}
			return
		}
	}
	if metadata.Store && response.StatusCode >= 200 && response.StatusCode < 300 {
		payload, readErr := io.ReadAll(response.Body)
		if readErr != nil {
			writeAPIError(w, http.StatusBadGateway, "upstream_error", "Upstream response could not be read")
			return
		}
		var stored struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(payload, &stored) == nil && stored.ID != "" {
			keyID, _ := strconv.ParseInt(r.Header.Get("X-GRF-API-Key-ID"), 10, 64)
			_ = m.store.SaveResponse(r.Context(), stored.ID, keyID, payload)
		}
		_, _ = w.Write(payload)
		return
	}
	_, _ = io.Copy(w, response.Body)
}

func applyDefaultStream(body []byte, enabled bool) ([]byte, error) {
	if !enabled {
		return body, nil
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil || payload == nil {
		return nil, fmt.Errorf("request body must be a JSON object")
	}
	if _, exists := payload["stream"]; exists {
		return body, nil
	}
	payload["stream"] = json.RawMessage("true")
	return json.Marshal(payload)
}

func (m *Manager) handleGetResponse(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("response_id"))
	keyID, _ := strconv.ParseInt(r.Header.Get("X-GRF-API-Key-ID"), 10, 64)
	payload, err := m.store.GetResponse(r.Context(), id, keyID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeAPIError(w, http.StatusNotFound, "not_found", "Response not found")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "Stored response could not be read")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}

func (m *Manager) handleDeleteResponse(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("response_id"))
	keyID, _ := strconv.ParseInt(r.Header.Get("X-GRF-API-Key-ID"), 10, 64)
	if err := m.store.DeleteResponse(r.Context(), id, keyID); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeAPIError(w, http.StatusNotFound, "not_found", "Response not found")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "Stored response could not be deleted")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "object": "response.deleted", "deleted": true})
}

func copyResponseHeaders(target, source http.Header) {
	for _, name := range []string{"Content-Type", "Cache-Control", "X-Request-Id", "OpenAI-Processing-Ms"} {
		if value := source.Get(name); value != "" {
			target.Set(name, value)
		}
	}
	target.Del("Content-Length")
}

func (m *Manager) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := strings.TrimSpace(r.Header.Get("Authorization"))
		if header == "" && r.URL.Path == "/v1/messages" {
			header = "Bearer " + strings.TrimSpace(r.Header.Get("X-API-Key"))
		}
		scheme, token, ok := strings.Cut(header, " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
			writeAPIError(w, http.StatusUnauthorized, "invalid_api_key", "Missing or invalid API key")
			return
		}
		key, err := m.store.VerifyAPIKey(r.Context(), strings.TrimSpace(token))
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				writeAPIError(w, http.StatusUnauthorized, "invalid_api_key", "Missing or invalid API key")
				return
			}
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "API key verification failed")
			return
		}
		r.Header.Set("X-GRF-API-Key-ID", strconv.FormatInt(key.ID, 10))
		next.ServeHTTP(w, r)
	})
}

func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{
		"message": message,
		"type":    code,
		"code":    code,
	}})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
