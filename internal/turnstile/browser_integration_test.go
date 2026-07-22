package turnstile

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	projectbrowser "github.com/grok-free-register/grok-reg/internal/browser"
)

// This test uses a local fake widget and never contacts Cloudflare. It is
// opt-in because it launches a real Chromium process.
func TestBrowserReusesProcessWithIsolatedContexts(t *testing.T) {
	if os.Getenv("GROK_BROWSER_INTEGRATION") != "1" {
		t.Skip("set GROK_BROWSER_INTEGRATION=1 to launch local Chromium integration test")
	}
	if projectbrowser.FindChrome() == "" {
		t.Skip("chrome/chromium not installed")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><head><script>
window.turnstile={render:function(_,opts){
  var leaked=localStorage.getItem('seen');
  localStorage.setItem('seen','1');
  setTimeout(function(){opts.callback(leaked?'context-leaked-token':'isolated-context-token')},10);
}};
</script></head><body>local fixture</body></html>`))
	}))
	defer server.Close()

	b := NewBrowser("", nil)
	b.HardTimeout = 10 * time.Second
	b.InitialWait = 20 * time.Millisecond
	b.PollInterval = 20 * time.Millisecond
	b.PollAttempts = 20
	b.ClickRetries = 0
	defer b.Close()

	for i := 0; i < 2; i++ {
		token, err := b.Solve(context.Background(), "local-site-key", server.URL)
		if err != nil {
			t.Fatalf("solve %d: %v", i+1, err)
		}
		if token != "isolated-context-token" {
			t.Fatalf("solve %d token=%q, browser context leaked", i+1, token)
		}
	}
}

// This opt-in test contacts the live x.ai and Cloudflare endpoints.
func TestBrowserLiveTurnstile(t *testing.T) {
	if os.Getenv("GROK_BROWSER_LIVE") != "1" {
		t.Skip("set GROK_BROWSER_LIVE=1 to run the live Turnstile test")
	}
	if projectbrowser.FindChrome() == "" {
		t.Skip("chrome/chromium not installed")
	}

	proxy := os.Getenv("GROK_BROWSER_PROXY")
	b := NewBrowser(proxy, nil)
	defer b.Close()
	token, err := b.Solve(context.Background(), "0x4AAAAAAAhr9JGVDZbrZOo0", "https://accounts.x.ai/sign-up")
	if err != nil {
		t.Fatal(err)
	}
	if len(token) <= 10 {
		t.Fatalf("short token: %q", token)
	}
	t.Logf("minted live token (%d bytes)", len(token))
}
