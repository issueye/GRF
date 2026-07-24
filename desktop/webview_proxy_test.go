package main

import (
	"slices"
	"testing"
)

func TestValidWebviewProxy(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
		want  string
	}{
		{name: "custom HTTP", value: "http://127.0.0.1:7890", want: "http://127.0.0.1:7890"},
		{name: "SOCKS5", value: "socks5://127.0.0.1:1080", want: "socks5://127.0.0.1:1080"},
		{name: "empty fallback", value: "", want: "http://127.0.0.1:40080"},
		{name: "invalid fallback", value: "http://proxy.test --remote-debugging-port=9222", want: "http://127.0.0.1:40080"},
		{name: "unsupported fallback", value: "file://proxy.test/path", want: "http://127.0.0.1:40080"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := validWebviewProxy(test.value); got != test.want {
				t.Fatalf("validWebviewProxy(%q)=%q want %q", test.value, got, test.want)
			}
		})
	}
}

func TestWebviewBrowserArgsIncludeProxyAndLocalBypass(t *testing.T) {
	args := webviewBrowserArgs("http://127.0.0.1:7890")
	if !slices.Contains(args, "--proxy-server=http://127.0.0.1:7890") {
		t.Fatalf("proxy argument missing: %v", args)
	}
	if !slices.Contains(args, "--proxy-bypass-list=localhost;127.0.0.1;[::1];<local>") {
		t.Fatalf("local bypass argument missing: %v", args)
	}
}
