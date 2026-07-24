package app

import (
	"slices"
	"testing"

	"github.com/grok-free-register/grok-reg/internal/config"
)

func TestValidOAuthProxy(t *testing.T) {
	fallback := config.Defaults().RegisterProxy
	for _, test := range []struct {
		name  string
		value string
		want  string
	}{
		{name: "custom HTTP", value: "http://127.0.0.1:7897", want: "http://127.0.0.1:7897"},
		{name: "SOCKS5", value: "socks5://127.0.0.1:1080", want: "socks5://127.0.0.1:1080"},
		{name: "empty fallback", value: "", want: fallback},
		{name: "invalid fallback", value: "http://proxy.test --remote-debugging-port=9222", want: fallback},
		{name: "unsupported fallback", value: "file://proxy.test/path", want: fallback},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := validOAuthProxy(test.value); got != test.want {
				t.Fatalf("validOAuthProxy(%q)=%q want %q", test.value, got, test.want)
			}
		})
	}
}

func TestOAuthChromeArgs(t *testing.T) {
	args := oauthChromeArgs(`C:\data\oauth-browser`, "http://127.0.0.1:7897", "https://accounts.x.ai/oauth2/device/verify")
	want := []string{
		"--app=https://accounts.x.ai/oauth2/device/verify",
		`--user-data-dir=C:\data\oauth-browser`,
		"--proxy-server=http://127.0.0.1:7897",
		"--proxy-bypass-list=" + oauthProxyBypass,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-background-mode",
	}
	for _, arg := range want {
		if !slices.Contains(args, arg) {
			t.Fatalf("missing Chrome argument %q in %v", arg, args)
		}
	}
	for _, arg := range args {
		if len(arg) >= 13 && arg[:13] == "--user-agent=" {
			t.Fatalf("OAuth Chrome must use its native user agent: %v", args)
		}
	}
}
