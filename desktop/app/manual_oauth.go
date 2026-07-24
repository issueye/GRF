package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/grok-free-register/grok-reg/internal/browser"
	"github.com/grok-free-register/grok-reg/internal/config"
	"github.com/grok-free-register/grok-reg/internal/manualoauth"
	"github.com/grok-free-register/grok-reg/internal/oauth"
)

const oauthProxyBypass = "localhost;127.0.0.1;[::1];<local>"

func (a *App) ListManualOAuthTasks() ([]manualoauth.PublicTask, error) {
	paths, err := resolvePaths()
	if err != nil {
		return nil, err
	}
	tasks, err := listManualOAuthTasks(paths.Outputs)
	if err != nil {
		return nil, err
	}
	a.oauthMu.Lock()
	activeID := a.oauthTaskID
	a.oauthMu.Unlock()
	public := make([]manualoauth.PublicTask, 0, len(tasks))
	for _, item := range tasks {
		task := item.task
		if task.Status == manualoauth.StatusAuthorizing && task.ID != activeID {
			task.Status = manualoauth.StatusFailed
			task.Error = "上一次授权窗口已中断，请重试"
			task.UserCode = ""
			task.VerificationURL = ""
			task.ExpiresAt = ""
			_ = item.store.Write(task)
		}
		public = append(public, manualoauth.Public(task))
	}
	return public, nil
}

func (a *App) StartManualOAuth(taskID string) (manualoauth.PublicTask, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return manualoauth.PublicTask{}, fmt.Errorf("请选择待授权账号")
	}

	a.oauthMu.Lock()
	if a.oauthTaskID != "" {
		active := a.oauthTaskID
		a.oauthMu.Unlock()
		if active == taskID {
			return manualoauth.PublicTask{}, fmt.Errorf("该账号正在授权")
		}
		return manualoauth.PublicTask{}, fmt.Errorf("已有账号正在授权，请先完成或关闭授权窗口")
	}
	a.oauthMu.Unlock()

	paths, err := resolvePaths()
	if err != nil {
		return manualoauth.PublicTask{}, err
	}
	store, task, err := findManualOAuthTask(paths.Outputs, taskID)
	if err != nil {
		return manualoauth.PublicTask{}, err
	}
	if task.Status == manualoauth.StatusAuthorized {
		return manualoauth.Public(task), nil
	}
	cfg, err := config.Load(paths.Config)
	if err != nil {
		return manualoauth.PublicTask{}, err
	}
	proxy := validOAuthProxy(cfg.RegisterProxy)
	client, err := oauth.NewClient(proxy, nil, time.Duration(cfg.OAuthRetrySec)*time.Second)
	if err != nil {
		return manualoauth.PublicTask{}, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.oauthMu.Lock()
	if a.oauthTaskID != "" {
		a.oauthMu.Unlock()
		cancel()
		return manualoauth.PublicTask{}, fmt.Errorf("已有账号正在授权，请先完成或关闭授权窗口")
	}
	a.oauthTaskID = task.ID
	a.oauthCancel = cancel
	a.oauthMu.Unlock()
	flow, err := client.StartDeviceFlow(ctx)
	if err != nil {
		a.finishManualOAuth(task.ID, nil, err, false)
		return manualoauth.PublicTask{}, fmt.Errorf("启动 OAuth: %w", err)
	}

	task.Status = manualoauth.StatusAuthorizing
	task.UserCode = flow.UserCode
	task.VerificationURL = flow.VerificationURL
	task.Error = ""
	expires := time.Now().Add(time.Duration(flow.ExpiresIn) * time.Second)
	if flow.ExpiresIn <= 0 {
		expires = time.Now().Add(10 * time.Minute)
	}
	task.ExpiresAt = expires.UTC().Format(time.RFC3339)
	if err := store.Write(task); err != nil {
		a.finishManualOAuth(task.ID, nil, err, false)
		return manualoauth.PublicTask{}, err
	}

	chromePath := browser.FindSystemChrome()
	if chromePath == "" {
		err := fmt.Errorf("未检测到 Google Chrome，请安装 Chrome 或设置 CHROME_PATH")
		a.finishManualOAuth(task.ID, nil, err, false)
		return manualoauth.PublicTask{}, err
	}
	profileDir := filepath.Join(paths.Root, "oauth-browser")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		a.finishManualOAuth(task.ID, nil, err, false)
		return manualoauth.PublicTask{}, fmt.Errorf("创建 OAuth 浏览器配置目录: %w", err)
	}
	cmd := exec.Command(chromePath, oauthChromeArgs(profileDir, proxy, flow.VerificationURL)...)
	if err := cmd.Start(); err != nil {
		a.finishManualOAuth(task.ID, nil, err, false)
		return manualoauth.PublicTask{}, fmt.Errorf("启动 Chrome OAuth 窗口: %w", err)
	}
	a.oauthMu.Lock()
	if a.oauthTaskID != task.ID {
		a.oauthMu.Unlock()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return manualoauth.PublicTask{}, fmt.Errorf("OAuth 授权已取消")
	}
	a.oauthProcess = cmd.Process
	a.oauthMu.Unlock()
	go func() {
		_ = cmd.Wait()
		a.finishManualOAuth(task.ID, nil, errors.New("授权窗口已关闭，可重新发起授权"), false)
	}()

	go func() {
		credential, pollErr := client.PollToken(ctx, flow)
		a.finishManualOAuth(task.ID, &credential, pollErr, true)
	}()
	return manualoauth.Public(task), nil
}

func (a *App) finishManualOAuth(taskID string, credential *oauth.Credential, resultErr error, closeWindow bool) {
	a.oauthMu.Lock()
	if a.oauthTaskID != taskID {
		a.oauthMu.Unlock()
		return
	}
	cancel := a.oauthCancel
	process := a.oauthProcess
	a.oauthTaskID = ""
	a.oauthCancel = nil
	a.oauthProcess = nil
	a.oauthMu.Unlock()
	if cancel != nil {
		cancel()
	}

	paths, err := resolvePaths()
	if err == nil {
		store, task, findErr := findManualOAuthTask(paths.Outputs, taskID)
		if findErr == nil {
			if resultErr == nil && credential != nil && credential.AccessToken != "" {
				task.Status = manualoauth.StatusAuthorized
				task.Credential = *credential
				task.Error = ""
			} else {
				task.Status = manualoauth.StatusFailed
				if resultErr != nil {
					task.Error = friendlyOAuthError(resultErr)
				} else {
					task.Error = "授权未完成，请重试"
				}
				task.UserCode = ""
				task.VerificationURL = ""
				task.ExpiresAt = ""
			}
			_ = store.Write(task)
		}
	}
	if closeWindow && process != nil {
		_ = process.Kill()
	}
}

func (a *App) cancelManualOAuth(message string, closeWindow bool) {
	a.oauthMu.Lock()
	taskID := a.oauthTaskID
	a.oauthMu.Unlock()
	if taskID == "" {
		return
	}
	a.finishManualOAuth(taskID, nil, errors.New(message), closeWindow)
}

type storedManualOAuthTask struct {
	store *manualoauth.Store
	task  manualoauth.Task
}

func listManualOAuthTasks(outputs string) ([]storedManualOAuthTask, error) {
	entries, err := os.ReadDir(outputs)
	if errors.Is(err, os.ErrNotExist) {
		return []storedManualOAuthTask{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := make([]storedManualOAuthTask, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		store := manualoauth.NewStore(filepath.Join(outputs, entry.Name()))
		tasks, listErr := store.List()
		if listErr != nil {
			continue
		}
		for _, task := range tasks {
			result = append(result, storedManualOAuthTask{store: store, task: task})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].task.CreatedAt < result[j].task.CreatedAt })
	return result, nil
}

func findManualOAuthTask(outputs, taskID string) (*manualoauth.Store, manualoauth.Task, error) {
	items, err := listManualOAuthTasks(outputs)
	if err != nil {
		return nil, manualoauth.Task{}, err
	}
	for _, item := range items {
		if item.task.ID == taskID {
			return item.store, item.task, nil
		}
	}
	return nil, manualoauth.Task{}, fmt.Errorf("待授权任务不存在或已完成")
}

func friendlyOAuthError(err error) string {
	value := err.Error()
	switch {
	case strings.Contains(value, "oauth_denied"):
		return "授权已拒绝，可重新发起"
	case strings.Contains(value, "oauth_expired"):
		return "授权码已过期，请重试"
	case strings.Contains(value, "context canceled"):
		return "授权已取消，可重新发起"
	default:
		return value
	}
}

func validOAuthProxy(value string) string {
	fallback := config.Defaults().RegisterProxy
	value = strings.TrimSpace(value)
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return fallback
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https", "socks5":
		return parsed.String()
	default:
		return fallback
	}
}

func oauthChromeArgs(profileDir, proxy, verificationURL string) []string {
	return []string{
		"--app=" + verificationURL,
		"--user-data-dir=" + profileDir,
		"--proxy-server=" + validOAuthProxy(proxy),
		"--proxy-bypass-list=" + oauthProxyBypass,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-background-mode",
	}
}
