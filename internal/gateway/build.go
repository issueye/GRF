package gateway

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

const (
	defaultBuildBaseURL  = "https://cli-chat-proxy.grok.com/v1"
	defaultBuildTokenURL = "https://auth.x.ai/oauth2/token"
	buildClientVersion   = "0.2.106"
)

type BuildClient struct {
	HTTPClient *http.Client
	BaseURL    string
	TokenURL   string
	agentID    string
	proxyURL   atomic.Value
}

func NewBuildClient() *BuildClient {
	client := &BuildClient{BaseURL: defaultBuildBaseURL, TokenURL: defaultBuildTokenURL, agentID: randomIdentifier()}
	client.proxyURL.Store("")
	transport := &http.Transport{
		Proxy: client.proxy, ForceAttemptHTTP2: true,
		MaxIdleConns: 64, MaxIdleConnsPerHost: 32, IdleConnTimeout: 90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 5 * time.Minute,
	}
	client.HTTPClient = &http.Client{Transport: transport}
	return client
}

func (c *BuildClient) SetProxy(rawURL string) error {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL != "" {
		parsed, err := url.Parse(rawURL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return fmt.Errorf("invalid gateway upstream proxy %q", rawURL)
		}
		switch strings.ToLower(parsed.Scheme) {
		case "http", "https", "socks5", "socks5h":
		default:
			return fmt.Errorf("unsupported gateway upstream proxy scheme %q", parsed.Scheme)
		}
	}
	c.proxyURL.Store(rawURL)
	if transport, ok := c.HTTPClient.Transport.(*http.Transport); ok {
		transport.CloseIdleConnections()
	}
	return nil
}

func (c *BuildClient) proxy(_ *http.Request) (*url.URL, error) {
	rawURL, _ := c.proxyURL.Load().(string)
	if rawURL == "" {
		return nil, nil
	}
	return url.Parse(rawURL)
}

func (c *BuildClient) ForwardResponses(ctx context.Context, credential Credential, body []byte, model string) (*http.Response, Credential, error) {
	return c.Forward(ctx, credential, "/responses", body, model)
}

func (c *BuildClient) CheckHealth(ctx context.Context, credential Credential) (Credential, error) {
	var err error
	if credential.AccessToken == "" || (credential.ExpiresAt != nil && credential.ExpiresAt.Before(time.Now().UTC().Add(time.Minute))) {
		credential, err = c.refresh(ctx, credential)
		if err != nil {
			return credential, err
		}
	}
	response, err := c.doHealth(ctx, credential)
	if err != nil {
		return credential, err
	}
	if response.StatusCode == http.StatusUnauthorized && credential.RefreshToken != "" {
		_ = response.Body.Close()
		credential, err = c.refresh(ctx, credential)
		if err != nil {
			return credential, err
		}
		response, err = c.doHealth(ctx, credential)
		if err != nil {
			return credential, err
		}
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return credential, fmt.Errorf("read Build model catalog: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return credential, &HealthCheckError{StatusCode: response.StatusCode}
	}
	var catalog struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		return credential, fmt.Errorf("parse Build model catalog: %w", err)
	}
	if catalog.Data == nil {
		return credential, fmt.Errorf("Build model catalog does not contain data")
	}
	return credential, nil
}

func (c *BuildClient) doHealth(ctx context.Context, credential Credential) (*http.Response, error) {
	endpoint := strings.TrimRight(c.BaseURL, "/") + "/models"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+credential.AccessToken)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-XAI-Token-Auth", "xai-grok-cli")
	request.Header.Set("x-grok-client-version", buildClientVersion)
	request.Header.Set("x-grok-client-identifier", "grok-shell")
	request.Header.Set("x-grok-client-mode", "headless")
	request.Header.Set("x-authenticateresponse", "authenticate-response")
	request.Header.Set("x-grok-agent-id", c.agentID)
	request.Header.Set("x-grok-req-id", randomIdentifier())
	request.Header.Set("User-Agent", "grok-shell/"+buildClientVersion+" (linux; x86_64)")
	if credential.UserID != "" {
		request.Header.Set("x-grok-user-id", credential.UserID)
	}
	return c.HTTPClient.Do(request)
}

func (c *BuildClient) Forward(ctx context.Context, credential Credential, path string, body []byte, model string) (*http.Response, Credential, error) {
	var err error
	if credential.AccessToken == "" || (credential.ExpiresAt != nil && credential.ExpiresAt.Before(time.Now().UTC().Add(time.Minute))) {
		credential, err = c.refresh(ctx, credential)
		if err != nil {
			return nil, credential, err
		}
	}
	response, err := c.do(ctx, credential, path, body, model)
	if err != nil {
		return nil, credential, err
	}
	if response.StatusCode != http.StatusUnauthorized || credential.RefreshToken == "" {
		return response, credential, nil
	}
	_ = response.Body.Close()
	credential, err = c.refresh(ctx, credential)
	if err != nil {
		return nil, credential, err
	}
	response, err = c.do(ctx, credential, path, body, model)
	return response, credential, err
}

func (c *BuildClient) doResponses(ctx context.Context, credential Credential, body []byte, model string) (*http.Response, error) {
	return c.do(ctx, credential, "/responses", body, model)
}

func (c *BuildClient) do(ctx context.Context, credential Credential, path string, body []byte, model string) (*http.Response, error) {
	endpoint := strings.TrimRight(c.BaseURL, "/") + "/" + strings.TrimLeft(path, "/")
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+credential.AccessToken)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-XAI-Token-Auth", "xai-grok-cli")
	request.Header.Set("x-grok-client-version", buildClientVersion)
	request.Header.Set("x-grok-client-identifier", "grok-shell")
	request.Header.Set("x-grok-client-mode", "headless")
	request.Header.Set("x-authenticateresponse", "authenticate-response")
	request.Header.Set("x-grok-agent-id", c.agentID)
	request.Header.Set("x-grok-req-id", randomIdentifier())
	request.Header.Set("User-Agent", "grok-shell/"+buildClientVersion+" (linux; x86_64)")
	if credential.UserID != "" {
		request.Header.Set("x-grok-user-id", credential.UserID)
	}
	if model != "" {
		request.Header.Set("x-grok-model-override", model)
	}
	return c.HTTPClient.Do(request)
}

func (c *BuildClient) refresh(ctx context.Context, credential Credential) (Credential, error) {
	if credential.RefreshToken == "" {
		return credential, fmt.Errorf("Build credential requires reauthentication")
	}
	clientID := credential.ClientID
	if clientID == "" {
		clientID = defaultBuildOAuthClientID
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {clientID},
		"refresh_token": {credential.RefreshToken},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return credential, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := c.HTTPClient.Do(request)
	if err != nil {
		return credential, fmt.Errorf("refresh Build credential: %w", err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return credential, err
	}
	var document struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Error        string `json:"error"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		return credential, fmt.Errorf("parse Build OAuth refresh: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || document.AccessToken == "" {
		return credential, fmt.Errorf("Build OAuth refresh failed: status=%d code=%s", response.StatusCode, document.Error)
	}
	if document.RefreshToken == "" {
		document.RefreshToken = credential.RefreshToken
	}
	if document.ExpiresIn <= 0 {
		document.ExpiresIn = 3600
	}
	credential.AccessToken = document.AccessToken
	credential.RefreshToken = document.RefreshToken
	expiresAt := time.Now().UTC().Add(time.Duration(document.ExpiresIn) * time.Second)
	credential.ExpiresAt = &expiresAt
	return credential, nil
}

func randomIdentifier() string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return fmt.Sprintf("grf-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(value)
}
