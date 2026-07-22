package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/grok-free-register/grok-reg/internal/browser"
	"github.com/grok-free-register/grok-reg/internal/config"
	"github.com/grok-free-register/grok-reg/internal/daemon"
	"github.com/grok-free-register/grok-reg/internal/home"
	"github.com/grok-free-register/grok-reg/internal/state"
)

type BootstrapInfo struct {
	Name       string `json:"name"`
	Version    string `json:"version"`
	Platform   string `json:"platform"`
	DataRoot   string `json:"data_root"`
	ConfigPath string `json:"config_path"`
	OutputRoot string `json:"output_root"`
	ChromePath string `json:"chrome_path"`
}

type Dashboard struct {
	Status      string        `json:"status"`
	Running     bool          `json:"running"`
	RunID       string        `json:"run_id"`
	Target      int           `json:"target"`
	Done        int           `json:"done"`
	SSOCount    int           `json:"sso_count"`
	OAuthCount  int           `json:"oauth_count"`
	FailCount   int           `json:"fail_count"`
	Phase       string        `json:"phase"`
	PhaseDetail string        `json:"phase_detail"`
	Workers     state.Workers `json:"workers"`
	PID         int           `json:"pid"`
	StartedAt   string        `json:"started_at"`
	UpdatedAt   string        `json:"updated_at"`
	Error       string        `json:"error"`
	LogPath     string        `json:"log_path"`
	OutputDir   string        `json:"output_dir"`
	RatePerMin  float64       `json:"rate_per_min"`
}

type StartRequest struct {
	Target  int `json:"target"`
	Threads int `json:"threads"`
}

type StartResult struct {
	PID       int    `json:"pid"`
	RunID     string `json:"run_id"`
	LogPath   string `json:"log_path"`
	OutputDir string `json:"output_dir"`
}

type RunEntry struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	UpdatedAt string `json:"updated_at"`
	CPACount  int    `json:"cpa_count"`
	SSOCount  int    `json:"sso_count"`
}

type Settings struct {
	EmailMode         string `json:"email_mode"`
	EmailDomain       string `json:"email_domain"`
	EmailAPI          string `json:"email_api"`
	RegisterProxy     string `json:"register_proxy"`
	ClearanceEnabled  bool   `json:"clearance_enabled"`
	FlareSolverrURL   string `json:"flaresolverr_url"`
	TurnstileProvider string `json:"turnstile_provider"`
	LiteSolverURL     string `json:"lite_solver_url"`
	CPAUploadEnabled  bool   `json:"cpa_upload_enabled"`
	CPAManagementBase string `json:"cpa_management_base"`
}

type App struct {
	version string
	mu      sync.Mutex
}

func New(version string) *App { return &App{version: version} }

func (a *App) Bootstrap() (BootstrapInfo, error) {
	p, err := resolvePaths()
	if err != nil {
		return BootstrapInfo{}, err
	}
	return BootstrapInfo{
		Name: "GRF", Version: a.version, Platform: runtime.GOOS,
		DataRoot: p.Root, ConfigPath: p.Config, OutputRoot: p.Outputs,
		ChromePath: browser.FindChrome(),
	}, nil
}

func (a *App) GetDashboard() (Dashboard, error) {
	p, err := resolvePaths()
	if err != nil {
		return Dashboard{}, err
	}
	snap, err := state.NewStore(p.State).Load()
	if err != nil {
		if os.IsNotExist(err) {
			return Dashboard{Status: string(state.StatusStopped)}, nil
		}
		return Dashboard{}, err
	}
	running := snap.PID > 0 && daemon.PIDAlive(snap.PID)
	statusValue := string(snap.Status)
	if snap.Status == state.StatusRunning && !running {
		statusValue = string(state.StatusStopped)
	}
	return Dashboard{
		Status: statusValue, Running: running, RunID: snap.RunID,
		Target: snap.Target, Done: snap.Done, SSOCount: snap.SSOCount,
		OAuthCount: snap.OAuthCount, FailCount: snap.FailCount,
		Phase: string(snap.Phase), PhaseDetail: snap.PhaseDetail,
		Workers: snap.Workers, PID: snap.PID, StartedAt: snap.StartedAt,
		UpdatedAt: snap.UpdatedAt, Error: snap.Error, LogPath: snap.LogPath,
		OutputDir: snap.OutputDir, RatePerMin: snap.RatePerMin,
	}, nil
}

func (a *App) Start(request StartRequest) (StartResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	target, err := config.ClampTarget(request.Target)
	if err != nil {
		return StartResult{}, err
	}
	threads, err := config.ClampThreads(request.Threads)
	if err != nil {
		return StartResult{}, err
	}
	p, err := resolvePaths()
	if err != nil {
		return StartResult{}, err
	}
	if pid, readErr := daemon.ReadPID(p.PID); readErr == nil && daemon.PIDAlive(pid) {
		return StartResult{}, fmt.Errorf("已有任务正在运行（PID %d）", pid)
	}
	if err := ensureConfig(p); err != nil {
		return StartResult{}, err
	}
	runID := home.NewRunID()
	run, err := p.PrepareRun(runID)
	if err != nil {
		return StartResult{}, err
	}
	store := state.NewStore(p.State)
	_ = store.Set(func(s *state.Snapshot) {
		s.Status, s.RunID, s.Target = state.StatusRunning, runID, target
		s.Done, s.SSOCount, s.OAuthCount, s.FailCount = 0, 0, 0, 0
		s.Phase, s.PhaseDetail, s.Error = state.PhaseIdle, "正在启动", ""
		s.Workers, s.PID = state.Workers{S: threads}, 0
		s.LogPath, s.OutputDir = run.LogPath, run.Root
	})
	daemon.ClearStop(p.Stop)
	pid, err := daemon.StartBackground(target, threads, runID)
	if err != nil {
		return StartResult{}, err
	}
	if err := daemon.WritePID(p.PID, pid); err != nil {
		return StartResult{}, err
	}
	_ = store.Set(func(s *state.Snapshot) { s.PID = pid })
	return StartResult{PID: pid, RunID: runID, LogPath: run.LogPath, OutputDir: run.Root}, nil
}

func (a *App) Stop() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	p, err := resolvePaths()
	if err != nil {
		return err
	}
	if err := daemon.Stop(p); err != nil {
		return err
	}
	store := state.NewStore(p.State)
	return store.Set(func(s *state.Snapshot) {
		s.Status, s.Phase, s.PhaseDetail, s.PID = state.StatusStopped, state.PhaseIdle, "已手动停止", 0
	})
}

func (a *App) TailLog(maxBytes int) (string, error) {
	if maxBytes <= 0 || maxBytes > 1<<20 {
		maxBytes = 64 << 10
	}
	p, err := resolvePaths()
	if err != nil {
		return "", err
	}
	path := ""
	if snap, loadErr := state.NewStore(p.State).Load(); loadErr == nil {
		path = snap.LogPath
	}
	if path == "" {
		path = latestLog(p.LogsDir)
	}
	if path == "" {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if int64(len(data)) > int64(maxBytes) {
		data = data[len(data)-maxBytes:]
	}
	return string(data), nil
}

func (a *App) ListRuns(limit int) ([]RunEntry, error) {
	if limit <= 0 || limit > 50 {
		limit = 12
	}
	p, err := resolvePaths()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(p.Outputs)
	if err != nil {
		if os.IsNotExist(err) {
			return []RunEntry{}, nil
		}
		return nil, err
	}
	runs := make([]RunEntry, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, _ := entry.Info()
		root := filepath.Join(p.Outputs, entry.Name())
		runs = append(runs, RunEntry{
			ID: entry.Name(), Path: root, UpdatedAt: info.ModTime().Format(time.RFC3339),
			CPACount: countFiles(filepath.Join(root, "CPA")),
			SSOCount: countFiles(filepath.Join(root, "SSO")),
		})
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].ID > runs[j].ID })
	if len(runs) > limit {
		runs = runs[:limit]
	}
	return runs, nil
}

func (a *App) GetSettings() (Settings, error) {
	p, err := resolvePaths()
	if err != nil {
		return Settings{}, err
	}
	if err := ensureConfig(p); err != nil {
		return Settings{}, err
	}
	cfg, err := config.Load(p.Config)
	if err != nil {
		return Settings{}, err
	}
	return Settings{
		EmailMode: string(cfg.EmailMode), EmailDomain: cfg.EmailDomain, EmailAPI: cfg.EmailAPI,
		RegisterProxy: cfg.RegisterProxy, ClearanceEnabled: cfg.ClearanceEnabled,
		FlareSolverrURL: cfg.FlareSolverrURL, TurnstileProvider: cfg.TurnstileProvider,
		LiteSolverURL: cfg.LiteSolverURL, CPAUploadEnabled: cfg.CPAUploadEnabled,
		CPAManagementBase: cfg.CPAManagementBase,
	}, nil
}

func (a *App) SaveSettings(settings Settings) error {
	p, err := resolvePaths()
	if err != nil {
		return err
	}
	if err := ensureConfig(p); err != nil {
		return err
	}
	provider := strings.ToLower(strings.TrimSpace(settings.TurnstileProvider))
	if provider == "" {
		provider = "browser"
	}
	proxy := strings.TrimSpace(settings.RegisterProxy)
	updates := map[string]string{
		"EMAIL_MODE":          stringOr(settings.EmailMode, "tempmail"),
		"EMAIL_DOMAIN":        strings.TrimSpace(settings.EmailDomain),
		"EMAIL_API":           strings.TrimSpace(settings.EmailAPI),
		"REGISTER_PROXY":      proxy,
		"HTTP_PROXY":          proxy,
		"HTTPS_PROXY":         proxy,
		"CLEARANCE_ENABLED":   bool01(settings.ClearanceEnabled),
		"FLARESOLVERR_URL":    strings.TrimSpace(settings.FlareSolverrURL),
		"TURNSTILE_PROVIDER":  provider,
		"LITE_SOLVER_URL":     strings.TrimSpace(settings.LiteSolverURL),
		"CPA_UPLOAD_ENABLED":  bool01(settings.CPAUploadEnabled),
		"CPA_MANAGEMENT_BASE": strings.TrimSpace(settings.CPAManagementBase),
	}
	return patchEnvFile(p.Config, updates)
}

func (a *App) OpenConfig() error {
	p, err := resolvePaths()
	if err != nil {
		return err
	}
	if err := ensureConfig(p); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		return exec.Command("notepad.exe", p.Config).Start()
	}
	return openPath(p.Config)
}

func (a *App) OpenPath(path string) error { return openPath(path) }

func resolvePaths() (home.Paths, error) {
	p, err := home.Resolve()
	if err != nil {
		return p, err
	}
	return p, p.EnsureBase()
}

func ensureConfig(p home.Paths) error {
	if _, err := os.Stat(p.Config); err == nil {
		return config.SyncExample(p.Root)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := config.Save(p.Config, config.Defaults()); err != nil {
		return err
	}
	return config.SyncExample(p.Root)
}

func latestLog(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var path string
	var newest time.Time
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "run-") {
			continue
		}
		info, err := entry.Info()
		if err == nil && info.ModTime().After(newest) {
			newest, path = info.ModTime(), filepath.Join(dir, entry.Name())
		}
	}
	return path
}

func countFiles(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			n++
		}
	}
	return n
}

func openPath(path string) error {
	cleaned := strings.TrimSpace(path)
	if cleaned == "" {
		return fmt.Errorf("path is required")
	}
	abs, err := filepath.Abs(cleaned)
	if err != nil {
		return err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return err
	}
	switch runtime.GOOS {
	case "windows":
		if info.IsDir() {
			return exec.Command("explorer.exe", abs).Start()
		}
		return exec.Command("explorer.exe", "/select,"+abs).Start()
	case "darwin":
		if info.IsDir() {
			return exec.Command("open", abs).Start()
		}
		return exec.Command("open", "-R", abs).Start()
	default:
		if !info.IsDir() {
			abs = filepath.Dir(abs)
		}
		return exec.Command("xdg-open", abs).Start()
	}
}

func patchEnvFile(path string, updates map[string]string) error {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	seen := map[string]bool{}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, _, ok := strings.Cut(trimmed, "=")
		key = strings.TrimSpace(key)
		if !ok {
			continue
		}
		if value, exists := updates[key]; exists {
			lines[i], seen[key] = key+"="+value, true
		}
	}
	keys := make([]string, 0, len(updates))
	for key := range updates {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if !seen[key] {
			lines = append(lines, key+"="+updates[key])
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.TrimRight(strings.Join(lines, "\n"), "\n")+"\n"), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func bool01(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

func stringOr(value, fallback string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return fallback
}
