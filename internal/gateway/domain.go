package gateway

import "time"

const (
	ProviderBuild = "grok_build"
	AuthActive    = "active"
	AuthRequired  = "reauth_required"
)

// Account is the non-secret account state exposed to the desktop UI.
type Account struct {
	ID            int64      `json:"id"`
	Provider      string     `json:"provider"`
	Name          string     `json:"name"`
	Email         string     `json:"email"`
	UserID        string     `json:"user_id"`
	Enabled       bool       `json:"enabled"`
	AuthStatus    string     `json:"auth_status"`
	MaxConcurrent int        `json:"max_concurrent"`
	FailureCount  int        `json:"failure_count"`
	CooldownUntil *time.Time `json:"cooldown_until,omitempty"`
	LastUsedAt    *time.Time `json:"last_used_at,omitempty"`
	ObservedModel string     `json:"observed_model"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// Credential is the encrypted-at-rest Build credential used internally.
type Credential struct {
	Account
	SourceKey    string
	ClientID     string
	AccessToken  string
	RefreshToken string
}

// AccountSeed is accepted from GRF registration output or credential imports.
type AccountSeed struct {
	Provider     string
	Name         string
	Email        string
	UserID       string
	ClientID     string
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

// APIKey contains only the public metadata stored for a client key.
type APIKey struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Enabled    bool       `json:"enabled"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	HasSecret  bool       `json:"has_secret"`
}
