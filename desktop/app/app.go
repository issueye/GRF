package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
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
	"github.com/grok-free-register/grok-reg/internal/cpa"
	"github.com/grok-free-register/grok-reg/internal/daemon"
	"github.com/grok-free-register/grok-reg/internal/gateway"
	"github.com/grok-free-register/grok-reg/internal/home"
	"github.com/grok-free-register/grok-reg/internal/oauth"
	"github.com/grok-free-register/grok-reg/internal/state"
	"github.com/wailsapp/wails/v3/pkg/application"
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
	EmailMode                       string `json:"email_mode"`
	EmailDomain                     string `json:"email_domain"`
	EmailAPI                        string `json:"email_api"`
	RegisterProxy                   string `json:"register_proxy"`
	ClearanceEnabled                bool   `json:"clearance_enabled"`
	FlareSolverrURL                 string `json:"flaresolverr_url"`
	TurnstileProvider               string `json:"turnstile_provider"`
	LiteSolverURL                   string `json:"lite_solver_url"`
	CPAUploadEnabled                bool   `json:"cpa_upload_enabled"`
	CPAManagementBase               string `json:"cpa_management_base"`
	APIEnabled                      bool   `json:"api_enabled"`
	APIListenHost                   string `json:"api_listen_host"`
	APIListenPort                   int    `json:"api_listen_port"`
	APIStreamDefault                bool   `json:"api_stream_default"`
	APIAccountHealthEnabled         bool   `json:"api_account_health_enabled"`
	APIAccountHealthIntervalMinutes int    `json:"api_account_health_interval_minutes"`
}

type GatewayAccountUpdate struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	Enabled       bool   `json:"enabled"`
	MaxConcurrent int    `json:"max_concurrent"`
}

type GatewayAccountImportFailure struct {
	File  string `json:"file"`
	Error string `json:"error"`
}

type GatewayAccountImportResult struct {
	SelectedFiles    int                           `json:"selected_files"`
	SuccessfulFiles  int                           `json:"successful_files"`
	FailedFiles      int                           `json:"failed_files"`
	ImportedAccounts int                           `json:"imported_accounts"`
	Failures         []GatewayAccountImportFailure `json:"failures"`
}

type GatewayAccountExportFailure struct {
	Account string `json:"account"`
	Error   string `json:"error"`
}

type GatewayAccountExportResult struct {
	TotalAccounts    int                            `json:"total_accounts"`
	ExportedAccounts int                            `json:"exported_accounts"`
	FailedAccounts   int                            `json:"failed_accounts"`
	Path             string                         `json:"path,omitempty"`
	Failures         []GatewayAccountExportFailure  `json:"failures"`
}

type CreatedAPIKey struct {
	Key    gateway.APIKey `json:"key"`
	Secret string         `json:"secret"`
}

type App struct {
	version        string
	mu             sync.Mutex
	gatewayStore   *gateway.Store
	gatewayManager *gateway.Manager
	gatewayErr     error
}

func New(version string) *App { return &App{version: version} }

func (a *App) ServiceStartup(_ context.Context, _ application.ServiceOptions) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.openGatewayLocked()
	if a.gatewayErr != nil {
		return nil
	}
	p, err := resolvePaths()
	if err != nil {
		a.gatewayErr = err
		return nil
	}
	cfg, err := config.Load(p.Config)
	if err != nil {
		a.gatewayErr = err
		return nil
	}
	if err := a.gatewayManager.SetUpstreamProxy(cfg.RegisterProxy); err != nil {
		a.gatewayErr = err
		return nil
	}
	a.gatewayManager.SetDefaultStream(cfg.APIStreamDefault)
	if err := a.gatewayManager.ConfigureHealthChecks(cfg.APIAccountHealthEnabled, time.Duration(cfg.APIAccountHealthIntervalMinutes)*time.Minute); err != nil {
		a.gatewayErr = err
		return nil
	}
	if cfg.APIEnabled {
		a.gatewayErr = a.gatewayManager.Start(net.JoinHostPort(cfg.APIListenHost, fmt.Sprint(cfg.APIListenPort)))
	}
	return nil
}

func (a *App) ServiceShutdown() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	var stopErr error
	if a.gatewayManager != nil {
		a.gatewayManager.StopHealthChecks()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		stopErr = a.gatewayManager.Stop(ctx)
		cancel()
	}
	if a.gatewayStore != nil {
		if err := a.gatewayStore.Close(); stopErr == nil {
			stopErr = err
		}
		a.gatewayStore = nil
		a.gatewayManager = nil
	}
	return stopErr
}

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
		CPAManagementBase: cfg.CPAManagementBase, APIEnabled: cfg.APIEnabled,
		APIListenHost: cfg.APIListenHost, APIListenPort: cfg.APIListenPort,
		APIStreamDefault:                cfg.APIStreamDefault,
		APIAccountHealthEnabled:         cfg.APIAccountHealthEnabled,
		APIAccountHealthIntervalMinutes: cfg.APIAccountHealthIntervalMinutes,
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
	host := strings.TrimSpace(settings.APIListenHost)
	if net.ParseIP(host) == nil && !strings.EqualFold(host, "localhost") {
		return fmt.Errorf("API 监听主机必须是 IP 地址或 localhost")
	}
	if settings.APIListenPort < 1 || settings.APIListenPort > 65535 {
		return fmt.Errorf("API 监听端口必须在 1 到 65535 之间")
	}
	if settings.APIAccountHealthIntervalMinutes < 5 || settings.APIAccountHealthIntervalMinutes > 1440 {
		return fmt.Errorf("账号测活间隔必须在 5 到 1440 分钟之间")
	}
	updates := map[string]string{
		"EMAIL_MODE":                          stringOr(settings.EmailMode, "tempmail"),
		"EMAIL_DOMAIN":                        strings.TrimSpace(settings.EmailDomain),
		"EMAIL_API":                           strings.TrimSpace(settings.EmailAPI),
		"REGISTER_PROXY":                      proxy,
		"HTTP_PROXY":                          proxy,
		"HTTPS_PROXY":                         proxy,
		"CLEARANCE_ENABLED":                   bool01(settings.ClearanceEnabled),
		"FLARESOLVERR_URL":                    strings.TrimSpace(settings.FlareSolverrURL),
		"TURNSTILE_PROVIDER":                  provider,
		"LITE_SOLVER_URL":                     strings.TrimSpace(settings.LiteSolverURL),
		"CPA_UPLOAD_ENABLED":                  bool01(settings.CPAUploadEnabled),
		"CPA_MANAGEMENT_BASE":                 strings.TrimSpace(settings.CPAManagementBase),
		"API_ENABLED":                         bool01(settings.APIEnabled),
		"API_LISTEN_HOST":                     host,
		"API_LISTEN_PORT":                     fmt.Sprint(settings.APIListenPort),
		"API_STREAM_DEFAULT":                  bool01(settings.APIStreamDefault),
		"API_ACCOUNT_HEALTH_ENABLED":          bool01(settings.APIAccountHealthEnabled),
		"API_ACCOUNT_HEALTH_INTERVAL_MINUTES": fmt.Sprint(settings.APIAccountHealthIntervalMinutes),
	}
	if err := patchEnvFile(p.Config, updates); err != nil {
		return err
	}
	return a.applyGatewaySettings(settings.APIEnabled, net.JoinHostPort(host, fmt.Sprint(settings.APIListenPort)), proxy, settings.APIStreamDefault, settings.APIAccountHealthEnabled, settings.APIAccountHealthIntervalMinutes)
}

func (a *App) GatewayStatus() gateway.Status {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.openGatewayLocked()
	if a.gatewayErr != nil {
		return gateway.Status{Error: a.gatewayErr.Error()}
	}
	return a.gatewayManager.Status()
}

func (a *App) ListGatewayRequestLogs(limit int) ([]gateway.RequestLog, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.openGatewayLocked()
	if a.gatewayErr != nil {
		return nil, a.gatewayErr
	}
	return a.gatewayManager.RequestLogs(limit), nil
}

func (a *App) ClearGatewayRequestLogs() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.openGatewayLocked()
	if a.gatewayErr != nil {
		return a.gatewayErr
	}
	a.gatewayManager.ClearRequestLogs()
	return nil
}

func (a *App) StartGateway() error {
	settings, err := a.GetSettings()
	if err != nil {
		return err
	}
	settings.APIEnabled = true
	return a.SaveSettings(settings)
}

func (a *App) StopGateway() error {
	settings, err := a.GetSettings()
	if err != nil {
		return err
	}
	settings.APIEnabled = false
	return a.SaveSettings(settings)
}

func (a *App) ListGatewayAccounts() ([]gateway.Account, error) {
	store, err := a.gatewayStoreReady()
	if err != nil {
		return nil, err
	}
	return store.ListAccounts(context.Background())
}

func (a *App) ImportGatewayAccounts(paths []string) (GatewayAccountImportResult, error) {
	store, err := a.gatewayStoreReady()
	if err != nil {
		return GatewayAccountImportResult{}, err
	}
	return importGatewayAccountFiles(context.Background(), store, paths), nil
}

// ExportGatewayAccounts exports every gateway account as a re-importable CPA
// JSON document and bundles them into a zip at destPath. Credentials are stored
// encrypted at rest; only the store can decrypt them, so export must run here.
func (a *App) ExportGatewayAccounts(destPath string) (GatewayAccountExportResult, error) {
	store, err := a.gatewayStoreReady()
	if err != nil {
		return GatewayAccountExportResult{}, err
	}
	return exportGatewayAccountFiles(context.Background(), store, destPath)
}

func (a *App) CheckGatewayAccounts() (gateway.AccountHealthSummary, error) {
	a.mu.Lock()
	a.openGatewayLocked()
	manager, err := a.gatewayManager, a.gatewayErr
	a.mu.Unlock()
	if err != nil {
		return gateway.AccountHealthSummary{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	return manager.CheckAccounts(ctx)
}

func importGatewayAccountFiles(ctx context.Context, store *gateway.Store, paths []string) GatewayAccountImportResult {
	const maxFiles = 500
	const maxFileSize = 4 << 20

	result := GatewayAccountImportResult{Failures: make([]GatewayAccountImportFailure, 0)}
	result.SelectedFiles = len(paths)
	if len(paths) > maxFiles {
		skipped := len(paths) - maxFiles
		paths = paths[:maxFiles]
		result.FailedFiles += skipped
		result.Failures = append(result.Failures, GatewayAccountImportFailure{
			File: "批量选择", Error: fmt.Sprintf("一次最多导入 %d 个文件，另有 %d 个文件未处理", maxFiles, skipped),
		})
	}
	for _, path := range paths {
		path = strings.TrimSpace(path)
		name := filepath.Base(strings.TrimSpace(path))
		if name == "." || name == "" {
			name = "未知文件"
		}
		fail := func(err error) {
			result.FailedFiles++
			result.Failures = append(result.Failures, GatewayAccountImportFailure{File: name, Error: err.Error()})
		}
		if !strings.EqualFold(filepath.Ext(path), ".json") {
			fail(fmt.Errorf("仅支持 JSON 格式的 CPA 文件"))
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			fail(fmt.Errorf("读取文件信息: %w", err))
			continue
		}
		if !info.Mode().IsRegular() {
			fail(fmt.Errorf("所选路径不是普通文件"))
			continue
		}
		if info.Size() > maxFileSize {
			fail(fmt.Errorf("文件超过 4 MiB 限制"))
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			fail(fmt.Errorf("读取文件: %w", err))
			continue
		}
		accounts, err := store.ImportAccountDocument(ctx, data)
		if err != nil {
			fail(err)
			continue
		}
		result.SuccessfulFiles++
		result.ImportedAccounts += len(accounts)
	}
	return result
}

func exportGatewayAccountFiles(ctx context.Context, store *gateway.Store, destPath string) (GatewayAccountExportResult, error) {
	result := GatewayAccountExportResult{Failures: make([]GatewayAccountExportFailure, 0)}
	destPath = strings.TrimSpace(destPath)
	if destPath == "" {
		return result, fmt.Errorf("未选择导出路径")
	}
	if !strings.EqualFold(filepath.Ext(destPath), ".zip") {
		destPath += ".zip"
	}
	absPath, err := filepath.Abs(destPath)
	if err != nil {
		return result, fmt.Errorf("解析导出路径: %w", err)
	}

	accounts, err := store.ListAccounts(ctx)
	if err != nil {
		return result, fmt.Errorf("读取账号列表: %w", err)
	}
	result.TotalAccounts = len(accounts)

	secret := cpa.DefaultSecret()
	buf := new(bytes.Buffer)
	zipWriter := zip.NewWriter(buf)
	usedNames := make(map[string]int, len(accounts))

	for _, account := range accounts {
		label := account.Email
		if label == "" {
			label = account.UserID
		}
		if label == "" {
			label = fmt.Sprintf("id-%d", account.ID)
		}

		credential, err := store.GetCredential(ctx, account.ID)
		if err != nil {
			result.FailedAccounts++
			result.Failures = append(result.Failures, GatewayAccountExportFailure{Account: label, Error: err.Error()})
			continue
		}

		var expired string
		if credential.ExpiresAt != nil {
			expired = credential.ExpiresAt.UTC().Format(time.RFC3339Nano)
		}
		doc := cpa.FromCredential(oauth.Credential{
			AccessToken:  credential.AccessToken,
			RefreshToken: credential.RefreshToken,
			ExpiresAt:    expired,
			Subject:      credential.UserID,
			Email:        credential.Email,
		}, credential.Email)

		name := cpa.Filename(doc, secret)
		if existing, ok := usedNames[name]; ok {
			usedNames[name] = existing + 1
			ext := filepath.Ext(name)
			name = fmt.Sprintf("%s-%d%s", strings.TrimSuffix(name, ext), usedNames[name], ext)
		} else {
			usedNames[name] = 1
		}

		entry, err := zipWriter.Create(name)
		if err != nil {
			result.FailedAccounts++
			result.Failures = append(result.Failures, GatewayAccountExportFailure{Account: label, Error: err.Error()})
			continue
		}
		raw, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			result.FailedAccounts++
			result.Failures = append(result.Failures, GatewayAccountExportFailure{Account: label, Error: err.Error()})
			continue
		}
		raw = append(raw, '\n')
		if _, err := entry.Write(raw); err != nil {
			result.FailedAccounts++
			result.Failures = append(result.Failures, GatewayAccountExportFailure{Account: label, Error: err.Error()})
			continue
		}
		result.ExportedAccounts++
	}

	if err := zipWriter.Close(); err != nil {
		return result, fmt.Errorf("生成压缩包失败: %w", err)
	}
	if result.ExportedAccounts == 0 {
		return result, fmt.Errorf("没有可导出的账号")
	}
	if err := os.WriteFile(absPath, buf.Bytes(), 0o600); err != nil {
		return result, fmt.Errorf("写入压缩包: %w", err)
	}
	result.Path = absPath
	return result, nil
}

func (a *App) UpdateGatewayAccount(update GatewayAccountUpdate) error {
	store, err := a.gatewayStoreReady()
	if err != nil {
		return err
	}
	return store.UpdateAccount(context.Background(), update.ID, update.Name, update.Enabled, update.MaxConcurrent)
}

func (a *App) DeleteGatewayAccount(id int64) error {
	store, err := a.gatewayStoreReady()
	if err != nil {
		return err
	}
	return store.DeleteAccount(context.Background(), id)
}

func (a *App) ListGatewayModels() []gateway.Model { return gateway.ListModels() }

func (a *App) ListAPIKeys() ([]gateway.APIKey, error) {
	store, err := a.gatewayStoreReady()
	if err != nil {
		return nil, err
	}
	return store.ListAPIKeys(context.Background())
}

func (a *App) CreateAPIKey(name string) (CreatedAPIKey, error) {
	store, err := a.gatewayStoreReady()
	if err != nil {
		return CreatedAPIKey{}, err
	}
	key, secret, err := store.CreateAPIKey(context.Background(), name)
	return CreatedAPIKey{Key: key, Secret: secret}, err
}

func (a *App) GetAPIKeySecret(id int64) (string, error) {
	store, err := a.gatewayStoreReady()
	if err != nil {
		return "", err
	}
	return store.GetAPIKeySecret(context.Background(), id)
}

func (a *App) SetAPIKeyEnabled(id int64, enabled bool) error {
	store, err := a.gatewayStoreReady()
	if err != nil {
		return err
	}
	return store.SetAPIKeyEnabled(context.Background(), id, enabled)
}

func (a *App) DeleteAPIKey(id int64) error {
	store, err := a.gatewayStoreReady()
	if err != nil {
		return err
	}
	return store.DeleteAPIKey(context.Background(), id)
}

func (a *App) applyGatewaySettings(enabled bool, address, proxyURL string, defaultStream, healthEnabled bool, healthIntervalMinutes int) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.openGatewayLocked()
	if a.gatewayErr != nil {
		return a.gatewayErr
	}
	if err := a.gatewayManager.SetUpstreamProxy(proxyURL); err != nil {
		return err
	}
	a.gatewayManager.SetDefaultStream(defaultStream)
	if err := a.gatewayManager.ConfigureHealthChecks(healthEnabled, time.Duration(healthIntervalMinutes)*time.Minute); err != nil {
		return err
	}
	status := a.gatewayManager.Status()
	if status.Running && (!enabled || status.Address != address) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := a.gatewayManager.Stop(ctx)
		cancel()
		if err != nil {
			return err
		}
	}
	if enabled && !a.gatewayManager.Status().Running {
		return a.gatewayManager.Start(address)
	}
	return nil
}

func (a *App) gatewayStoreReady() (*gateway.Store, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.openGatewayLocked()
	return a.gatewayStore, a.gatewayErr
}

func (a *App) openGatewayLocked() {
	if a.gatewayStore != nil || a.gatewayErr != nil {
		return
	}
	p, err := resolvePaths()
	if err != nil {
		a.gatewayErr = err
		return
	}
	store, err := gateway.OpenStore(context.Background(), p.GatewayDB, p.GatewayKey)
	if err != nil {
		a.gatewayErr = err
		return
	}
	a.gatewayStore = store
	a.gatewayManager = gateway.NewManager(store)
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
