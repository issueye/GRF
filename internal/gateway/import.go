package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

const defaultBuildOAuthClientID = "b1a00492-073a-47ea-816f-4c329264a828"

type credentialDocument struct {
	Accounts []credentialEntry `json:"accounts"`
}

type credentialEntry struct {
	Provider     string `json:"provider"`
	Name         string `json:"name"`
	ClientID     string `json:"client_id"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	TokenType    string `json:"token_type"`
	ExpiresAt    string `json:"expires_at"`
	Expired      string `json:"expired"`
	ExpiresIn    int64  `json:"expires_in"`
	Email        string `json:"email"`
	Subject      string `json:"sub"`
	UserID       string `json:"user_id"`
}

// ImportAccountDocument accepts GRF CPA JSON, grok2api Build export JSON, and
// JSON sequences containing either representation.
func (s *Store) ImportAccountDocument(ctx context.Context, data []byte) ([]Account, error) {
	entries, err := parseCredentialEntries(data)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("credential document contains no accounts")
	}
	accounts := make([]Account, 0, len(entries))
	for index, entry := range entries {
		seed, err := normalizeCredentialEntry(entry)
		if err != nil {
			return nil, fmt.Errorf("credential %d: %w", index+1, err)
		}
		account, _, err := s.UpsertAccount(ctx, seed)
		if err != nil {
			return nil, fmt.Errorf("import credential %d: %w", index+1, err)
		}
		accounts = append(accounts, account)
	}
	return accounts, nil
}

func parseCredentialEntries(data []byte) ([]credentialEntry, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	entries := make([]credentialEntry, 0)
	for documentIndex := 1; ; documentIndex++ {
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err == io.EOF {
			return entries, nil
		} else if err != nil {
			return nil, fmt.Errorf("parse credential JSON document %d: %w", documentIndex, err)
		}
		var shape map[string]json.RawMessage
		if err := json.Unmarshal(raw, &shape); err != nil {
			return nil, fmt.Errorf("parse credential JSON document %d: %w", documentIndex, err)
		}
		if accounts, ok := shape["accounts"]; ok {
			var batch []credentialEntry
			if err := json.Unmarshal(accounts, &batch); err != nil {
				return nil, fmt.Errorf("parse credential batch %d: %w", documentIndex, err)
			}
			entries = append(entries, batch...)
			continue
		}
		var entry credentialEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			return nil, fmt.Errorf("parse credential %d: %w", documentIndex, err)
		}
		entries = append(entries, entry)
	}
}

func normalizeCredentialEntry(entry credentialEntry) (AccountSeed, error) {
	provider := strings.ToLower(strings.TrimSpace(entry.Provider))
	if provider == "" || provider == "xai" {
		provider = ProviderBuild
	}
	if provider != ProviderBuild {
		return AccountSeed{}, fmt.Errorf("unsupported provider %q", entry.Provider)
	}
	if entry.TokenType != "" && !strings.EqualFold(strings.TrimSpace(entry.TokenType), "Bearer") {
		return AccountSeed{}, fmt.Errorf("unsupported token type %q", entry.TokenType)
	}
	accessToken := strings.TrimSpace(entry.AccessToken)
	refreshToken := strings.TrimSpace(entry.RefreshToken)
	if accessToken == "" && refreshToken == "" {
		return AccountSeed{}, fmt.Errorf("access token or refresh token is required")
	}
	expiresAt, err := credentialExpiry(entry)
	if err != nil {
		return AccountSeed{}, err
	}
	clientID := strings.TrimSpace(entry.ClientID)
	if clientID == "" {
		clientID = defaultBuildOAuthClientID
	}
	userID := strings.TrimSpace(entry.UserID)
	if userID == "" {
		userID = strings.TrimSpace(entry.Subject)
	}
	name := strings.TrimSpace(entry.Name)
	if name == "" {
		name = strings.TrimSpace(entry.Email)
	}
	if name == "" {
		name = userID
	}
	if name == "" {
		name = "Grok Build account"
	}
	return AccountSeed{
		Provider: provider, Name: name, Email: strings.TrimSpace(entry.Email), UserID: userID,
		ClientID: clientID, AccessToken: accessToken, RefreshToken: refreshToken, ExpiresAt: expiresAt,
	}, nil
}

func credentialExpiry(entry credentialEntry) (time.Time, error) {
	raw := strings.TrimSpace(entry.ExpiresAt)
	if raw == "" {
		raw = strings.TrimSpace(entry.Expired)
	}
	if raw != "" {
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return time.Time{}, fmt.Errorf("credential expiry must be RFC3339: %w", err)
		}
		return parsed.UTC(), nil
	}
	if entry.ExpiresIn < 0 || entry.ExpiresIn > int64((365*24*time.Hour)/time.Second) {
		return time.Time{}, fmt.Errorf("expires_in is outside the supported range")
	}
	if entry.ExpiresIn > 0 {
		return time.Now().UTC().Add(time.Duration(entry.ExpiresIn) * time.Second), nil
	}
	return time.Time{}, nil
}
