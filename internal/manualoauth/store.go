package manualoauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/grok-free-register/grok-reg/internal/oauth"
)

type Status string

const (
	StatusQueued      Status = "queued"
	StatusAuthorizing Status = "authorizing"
	StatusAuthorized  Status = "authorized"
	StatusFailed      Status = "failed"
)

type Task struct {
	ID              string           `json:"id"`
	RunID           string           `json:"run_id"`
	Email           string           `json:"email"`
	Password        string           `json:"password"`
	SSO             string           `json:"sso"`
	Status          Status           `json:"status"`
	UserCode        string           `json:"user_code,omitempty"`
	VerificationURL string           `json:"verification_url,omitempty"`
	ExpiresAt       string           `json:"expires_at,omitempty"`
	Error           string           `json:"error,omitempty"`
	Credential      oauth.Credential `json:"credential,omitempty"`
	CreatedAt       string           `json:"created_at"`
	UpdatedAt       string           `json:"updated_at"`
}

type PublicTask struct {
	ID              string `json:"id"`
	RunID           string `json:"run_id"`
	Email           string `json:"email"`
	Password        string `json:"password"`
	Status          Status `json:"status"`
	UserCode        string `json:"user_code,omitempty"`
	VerificationURL string `json:"verification_url,omitempty"`
	ExpiresAt       string `json:"expires_at,omitempty"`
	Error           string `json:"error,omitempty"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

type Store struct {
	dir string
}

func NewStore(runRoot string) *Store {
	return &Store{dir: filepath.Join(runRoot, "oauth")}
}

func (s *Store) Enqueue(runID, email, password, sso string) (Task, error) {
	now := time.Now().UTC()
	id := fmt.Sprintf("%d-%s", now.UnixNano(), sanitizeID(email))
	task := Task{
		ID: id, RunID: runID, Email: email, Password: password, SSO: sso,
		Status: StatusQueued, CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano),
	}
	return task, s.Write(task)
}

func (s *Store) Read(id string) (Task, error) {
	data, err := os.ReadFile(s.path(id))
	if err != nil {
		return Task{}, err
	}
	var task Task
	if err := json.Unmarshal(data, &task); err != nil {
		return Task{}, fmt.Errorf("decode manual OAuth task: %w", err)
	}
	return task, nil
}

func (s *Store) Write(task Task) error {
	if task.ID == "" || filepath.Base(task.ID) != task.ID {
		return fmt.Errorf("invalid manual OAuth task id")
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	task.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	data, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path(task.ID) + fmt.Sprintf(".%d.tmp", os.Getpid())
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path(task.ID)); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func (s *Store) List() ([]Task, error) {
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return []Task{}, nil
	}
	if err != nil {
		return nil, err
	}
	tasks := make([]Task, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		task, readErr := s.Read(strings.TrimSuffix(entry.Name(), ".json"))
		if readErr == nil {
			tasks = append(tasks, task)
		}
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].CreatedAt < tasks[j].CreatedAt })
	return tasks, nil
}

func (s *Store) Remove(id string) error {
	err := os.Remove(s.path(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *Store) WaitAuthorized(ctx context.Context, id string, interval time.Duration) (oauth.Credential, error) {
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		task, err := s.Read(id)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return oauth.Credential{}, err
		}
		if err == nil && task.Status == StatusAuthorized {
			if task.Credential.AccessToken == "" || task.Credential.RefreshToken == "" {
				return oauth.Credential{}, fmt.Errorf("manual OAuth task has no credential")
			}
			return task.Credential, nil
		}
		select {
		case <-ctx.Done():
			return oauth.Credential{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func Public(task Task) PublicTask {
	return PublicTask{
		ID: task.ID, RunID: task.RunID, Email: task.Email, Password: task.Password, Status: task.Status,
		UserCode: task.UserCode, VerificationURL: task.VerificationURL,
		ExpiresAt: task.ExpiresAt, Error: task.Error, CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt,
	}
}

func (s *Store) path(id string) string {
	return filepath.Join(s.dir, id+".json")
}

func sanitizeID(value string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(value) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "account"
	}
	return b.String()
}
