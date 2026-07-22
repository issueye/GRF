package turnstile_test

import (
	"runtime"
	"testing"

	"github.com/grok-free-register/grok-reg/internal/browser"
	"github.com/grok-free-register/grok-reg/internal/turnstile"
)

func TestBrowserProviderDefault(t *testing.T) {
	p := browser.FindChrome()
	t.Logf("chrome path: %s", p)
	if p == "" {
		t.Skip("chrome/chromium not found on this machine")
	}
	pr := turnstile.New(turnstile.Options{Provider: "browser"})
	want := "browser"
	if runtime.GOOS == "windows" {
		want = "go-browser"
	}
	if pr.Name() != want {
		t.Fatalf("provider name=%s want=%s", pr.Name(), want)
	}
}

func TestGoBrowserProvider(t *testing.T) {
	pr := turnstile.New(turnstile.Options{Provider: "go-browser", Workers: 3})
	defer pr.(turnstile.Closer).Close()
	if pr.Name() != "go-browser" {
		t.Fatalf("provider name=%s", pr.Name())
	}
}

func TestNewDefaultsToBrowser(t *testing.T) {
	pr := turnstile.New(turnstile.Options{})
	want := "browser"
	if runtime.GOOS == "windows" {
		want = "go-browser"
	}
	if pr.Name() != want {
		t.Fatalf("default provider=%s want=%s", pr.Name(), want)
	}
}
