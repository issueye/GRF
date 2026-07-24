package main

import (
	"net/url"
	"strings"

	"github.com/grok-free-register/grok-reg/internal/config"
	"github.com/grok-free-register/grok-reg/internal/home"
	"github.com/grok-free-register/grok-reg/internal/oauth"
)

func loadWebviewProxy() string {
	proxy := ""
	if paths, err := home.Resolve(); err == nil {
		if cfg, loadErr := config.Load(paths.Config); loadErr == nil {
			proxy = cfg.RegisterProxy
		}
	}
	return validWebviewProxy(proxy)
}

func webviewBrowserArgs(proxy string) []string {
	return []string{
		"--user-agent=" + oauth.DefaultUA,
		"--lang=zh-CN",
		"--disable-blink-features=AutomationControlled",
		"--proxy-server=" + validWebviewProxy(proxy),
		"--proxy-bypass-list=localhost;127.0.0.1;[::1];<local>",
	}
}

func validWebviewProxy(value string) string {
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
