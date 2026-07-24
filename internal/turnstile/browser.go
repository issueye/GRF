package turnstile

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/storage"
	"github.com/chromedp/chromedp"

	"github.com/grok-free-register/grok-reg/internal/browser"
	"github.com/grok-free-register/grok-reg/internal/clearance"
)

// Browser mints Turnstile via headless Chromium.
// Aligns with grok_register/register.py inject + click + poll (same api.js, same widget JS).
type Browser struct {
	ExecPath string
	Proxy    string
	Clear    *clearance.Manager

	HardTimeout   time.Duration
	InitialWait   time.Duration
	PollInterval  time.Duration
	PollAttempts  int
	ClickRetries  int
	ClickInterval time.Duration

	solveMu       sync.Mutex
	mu            sync.Mutex
	allocCtx      context.Context
	allocCancel   context.CancelFunc
	browserCtx    context.Context
	browserCancel context.CancelFunc
	// Some Chrome builds reject Target.createBrowserContext in headless mode.
	// In that case the worker falls back to a fresh tab plus explicit storage
	// cleanup while still reusing the Chromium process.
	isolationUnsupported bool
}

func NewBrowser(proxy string, cm *clearance.Manager) *Browser {
	return &Browser{
		ExecPath:      browser.FindChrome(),
		Proxy:         proxy,
		Clear:         cm,
		HardTimeout:   90 * time.Second,
		InitialWait:   500 * time.Millisecond, // match Python SOLVER_INITIAL_WAIT_MS default
		PollInterval:  500 * time.Millisecond,
		PollAttempts:  100,
		ClickRetries:  3,
		ClickInterval: 600 * time.Millisecond,
	}
}

func (b *Browser) Name() string { return "browser" }

func (b *Browser) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.browserCancel != nil {
		b.browserCancel()
		b.browserCancel = nil
		b.browserCtx = nil
	}
	if b.allocCancel != nil {
		b.allocCancel()
		b.allocCancel = nil
		b.allocCtx = nil
	}
}

// ensureBrowser starts one persistent Chromium process for this Browser.
// Solve creates a fresh incognito BrowserContext inside this process.
func (b *Browser) ensureBrowser() (context.Context, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.browserCtx != nil {
		return b.browserCtx, nil
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		// Match CloakBrowser/Playwright-ish flags used by original project.
		// Windows system Chrome is blocked by Turnstile in headless mode even
		// when the same proxy works in a normal browser. Run a minimized headed
		// window there; other platforms keep the existing headless behaviour.
		chromedp.Flag("headless", runtime.GOOS != "windows"),
		chromedp.Flag("enable-automation", false),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("no-default-browser-check", true),
		chromedp.Flag("disable-infobars", true),
		chromedp.WindowSize(800, 600),
	)
	if runtime.GOOS == "windows" {
		opts = append(opts,
			chromedp.Flag("start-minimized", true),
			chromedp.Flag("window-position", "-32000,-32000"),
		)
	}
	if b.ExecPath != "" {
		opts = append(opts, chromedp.ExecPath(b.ExecPath))
	}
	if b.Proxy != "" {
		opts = append(opts, chromedp.ProxyServer(b.Proxy))
	}
	// Use Chromium's actual UA by default. Injecting a UA from another browser
	// session can create a fingerprint mismatch, so keep it behind the same
	// explicit opt-in as clearance cookies.
	if injectClearance() && b.Clear != nil {
		if u := b.Clear.UserAgent(); u != "" {
			opts = append(opts, chromedp.UserAgent(u))
		}
	}
	allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	if err := chromedp.Run(browserCtx); err != nil {
		browserCancel()
		cancel()
		return nil, fmt.Errorf("start chromium: %w", err)
	}
	b.allocCtx, b.allocCancel = allocCtx, cancel
	b.browserCtx, b.browserCancel = browserCtx, browserCancel
	return browserCtx, nil
}

// Solve solves only the Turnstile token. SolveFull additionally mints a Castle
// request token in the same browser session.
func (b *Browser) Solve(ctx context.Context, siteKey, pageURL string) (string, error) {
	res, err := b.solve(ctx, siteKey, pageURL)
	return res.Token, err
}

// SolveFull implements CastleMinter.
func (b *Browser) SolveFull(ctx context.Context, siteKey, pageURL string) (SolveResult, error) {
	return b.solve(ctx, siteKey, pageURL)
}

func (b *Browser) solve(ctx context.Context, siteKey, pageURL string) (SolveResult, error) {
	// One Browser represents one pool worker. Serializing direct callers also
	// keeps the storage-cleanup fallback safe on Chromium builds that do not
	// support isolated browser contexts.
	b.solveMu.Lock()
	defer b.solveMu.Unlock()

	if siteKey == "" {
		return SolveResult{}, fmt.Errorf("empty site key")
	}
	if pageURL == "" {
		pageURL = "https://accounts.x.ai/sign-up"
	}
	if b.ExecPath == "" {
		b.ExecPath = browser.FindChrome()
	}
	if b.ExecPath == "" {
		return SolveResult{}, fmt.Errorf("chrome/chromium not found; set CHROME_PATH or install cloakbrowser")
	}

	hard := b.HardTimeout
	if hard <= 0 {
		hard = 90 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, hard)
	defer cancel()

	browserCtx, err := b.ensureBrowser()
	if err != nil {
		return SolveResult{}, err
	}
	// Prefer an incognito browser context. A few Chromium builds reject CDP's
	// Target.createBrowserContext; for those, use a fresh tab and clear state.
	tabCtx, tabCancel, isolated, err := b.newSolveContext(browserCtx)
	if err != nil {
		return SolveResult{}, err
	}
	defer tabCancel()

	tabCtx, cancel2 := context.WithCancel(tabCtx)
	defer cancel2()
	go func() {
		select {
		case <-ctx.Done():
			cancel2()
		case <-tabCtx.Done():
		}
	}()

	// --- navigate (Python: page.goto sign-up, wait 1s) ---
	actions := []chromedp.Action{
		network.Enable(),
	}
	if !isolated {
		actions = append(actions, clearBrowserState(pageURL))
	}
	actions = append(actions,
		chromedp.ActionFunc(func(ctx context.Context) error {
			return b.injectClearanceCookies(ctx)
		}),
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(
				`Object.defineProperty(navigator,"webdriver",{get:()=>undefined})`,
			).Do(ctx)
			return err
		}),
		// chromedp.Navigate waits for the full load event. accounts.x.ai keeps
		// background resources alive, which can consume the entire solve timeout
		// before widget injection starts. Match Playwright's domcontentloaded
		// behaviour: issue Page.navigate directly and continue once body exists.
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, _, errorText, _, err := page.Navigate(pageURL).Do(ctx)
			if err != nil {
				return err
			}
			if errorText != "" {
				return fmt.Errorf("navigation failed: %s", errorText)
			}
			return nil
		}),
		chromedp.WaitReady("body", chromedp.ByQuery),
	)
	if err := chromedp.Run(tabCtx, actions...); err != nil {
		return SolveResult{}, fmt.Errorf("navigate: %w", err)
	}
	var pageTitle, bodyText string
	if err := chromedp.Run(tabCtx,
		chromedp.Title(&pageTitle),
		chromedp.Text("body", &bodyText, chromedp.ByQuery),
	); err != nil {
		return SolveResult{}, fmt.Errorf("inspect page: %w", err)
	}
	if strings.Contains(pageTitle, "Attention Required") ||
		strings.Contains(bodyText, "You are unable to access") {
		return SolveResult{}, fmt.Errorf("accounts.x.ai blocked automated browser (title=%q); switch proxy or use a headed browser", pageTitle)
	}
	select {
	case <-ctx.Done():
		return SolveResult{}, ctx.Err()
	case <-time.After(1000 * time.Millisecond):
	}

	// --- inject EXACT Python widget (api.js without ?render=explicit) ---
	if err := chromedp.Run(tabCtx, chromedp.Evaluate(buildInjectJS(siteKey), nil)); err != nil {
		return SolveResult{}, fmt.Errorf("inject: %w", err)
	}

	// Python: SOLVER_INITIAL_WAIT_MS then early poll
	iw := b.InitialWait
	if iw <= 0 {
		iw = 500 * time.Millisecond
	}
	select {
	case <-ctx.Done():
		return SolveResult{}, ctx.Err()
	case <-time.After(iw):
	}

	// early poll (Python SOLVER_EARLY_POLL: 2 x 800ms)
	for i := 0; i < 2; i++ {
		if tok, _ := readToken(tabCtx); len(tok) > 10 {
			return SolveResult{Token: tok, Castle: b.mintCastleToken(tabCtx)}, nil
		}
		select {
		case <-ctx.Done():
			return SolveResult{}, ctx.Err()
		case <-time.After(800 * time.Millisecond):
		}
	}

	// click stage (Python SOLVER_MOUSE_CLICK_RETRIES=3, interval 600ms)
	retries := b.ClickRetries
	if retries < 0 {
		retries = 3
	}
	interval := b.ClickInterval
	if interval <= 0 {
		interval = 600 * time.Millisecond
	}
	for i := 0; i < retries; i++ {
		if tok, _ := readToken(tabCtx); len(tok) > 10 {
			return SolveResult{Token: tok, Castle: b.mintCastleToken(tabCtx)}, nil
		}
		_ = mouseClickTurnstileCenter(tabCtx)
		if i+1 < retries {
			select {
			case <-ctx.Done():
				return SolveResult{}, ctx.Err()
			case <-time.After(interval):
			}
		}
	}

	// poll (Python 100 x 500ms)
	attempts := b.PollAttempts
	if attempts <= 0 {
		attempts = 100
	}
	poll := b.PollInterval
	if poll <= 0 {
		poll = 500 * time.Millisecond
	}
	for i := 0; i < attempts; i++ {
		select {
		case <-ctx.Done():
			return SolveResult{}, fmt.Errorf("turnstile timeout (no token) %s", pageDiag(tabCtx))
		case <-time.After(poll):
		}
		tok, err := readToken(tabCtx)
		if err != nil {
			continue
		}
		if len(tok) > 10 {
			return SolveResult{Token: tok, Castle: b.mintCastleToken(tabCtx)}, nil
		}
		// Python re-clicks every ~10s while polling
		if i > 0 && i%20 == 0 {
			_ = mouseClickTurnstileCenter(tabCtx)
		}
	}
	return SolveResult{}, fmt.Errorf("turnstile timeout (no token) %s", pageDiag(tabCtx))
}

func (b *Browser) newSolveContext(browserCtx context.Context) (context.Context, context.CancelFunc, bool, error) {
	b.mu.Lock()
	unsupported := b.isolationUnsupported
	b.mu.Unlock()
	if !unsupported {
		ctx, cancel := chromedp.NewContext(browserCtx, chromedp.WithNewBrowserContext())
		if err := chromedp.Run(ctx); err == nil {
			return ctx, cancel, true, nil
		} else {
			cancel()
			// Fall back only for the known browser-context capability failure;
			// do not hide crashes or transport errors.
			if !strings.Contains(err.Error(), "Failed to open new tab") {
				return nil, func() {}, false, fmt.Errorf("create isolated browser context: %w", err)
			}
			b.mu.Lock()
			b.isolationUnsupported = true
			b.mu.Unlock()
		}
	}
	ctx, cancel := chromedp.NewContext(browserCtx)
	if err := chromedp.Run(ctx); err != nil {
		cancel()
		return nil, func() {}, false, fmt.Errorf("create browser tab: %w", err)
	}
	return ctx, cancel, false, nil
}

func clearBrowserState(pageURL string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		_ = network.ClearBrowserCookies().Do(ctx)
		_ = network.ClearBrowserCache().Do(ctx)
		u, err := url.Parse(pageURL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return nil
		}
		origin := u.Scheme + "://" + u.Host
		return storage.ClearDataForOrigin(origin, "all").Do(ctx)
	})
}

func (b *Browser) injectClearanceCookies(ctx context.Context) error {
	if !injectClearance() || b.Clear == nil {
		return nil
	}
	for _, c := range b.Clear.Get().Cookies {
		if c.Name == "" {
			continue
		}
		domain := c.Domain
		if domain == "" {
			domain = ".x.ai"
		}
		path := c.Path
		if path == "" {
			path = "/"
		}
		_ = network.SetCookie(c.Name, c.Value).
			WithURL("https://accounts.x.ai/").
			WithDomain(domain).
			WithPath(path).
			Do(ctx)
	}
	return nil
}

// buildInjectJS ports register.py _inject_turnstile_widget and JSON-encodes the
// externally supplied site key before embedding it in JavaScript.
func buildInjectJS(siteKey string) string {
	skJSON, err := json.Marshal(siteKey)
	if err != nil {
		return ""
	}
	sk := string(skJSON)
	// Same structure as Python f-string inject (non-timeline path).
	return fmt.Sprintf(
		`var d=document.createElement('div');d.className='cf-turnstile';d.setAttribute('data-sitekey',%s);d.style.cssText='position:fixed;top:10px;left:10px;z-index:99999;background:white;padding:12px;border:2px solid red;border-radius:6px;width:300px;height:70px';document.body.appendChild(d);function __r(){window.turnstile&&window.turnstile.render(d,{sitekey:%s,callback:function(t){var i=document.querySelector('input[name="cf-turnstile-response"]');if(!i){i=document.createElement('input');i.type='hidden';i.name='cf-turnstile-response';document.body.appendChild(i);}i.value=t;}})}if(window.turnstile){__r()}else{var s=document.createElement('script');s.src='https://challenges.cloudflare.com/turnstile/v0/api.js';s.onload=function(){setTimeout(__r,1000)};document.head.appendChild(s);}`,
		sk, sk,
	)
}

func readToken(ctx context.Context) (string, error) {
	var tok string
	err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.querySelector('input[name="cf-turnstile-response"]')?.value||""`,
		&tok,
	))
	return tok, err
}

// castleMintScript fetches the Castle v2 SDK, evals it (so its IIFE runs),
// configures the publishable app id, and resolves createRequestToken(). The
// promise resolves to the token string or "" on any failure.
const castleMintScript = `(async () => {
  try {
    if (typeof window._castle !== 'function') {
      const candidates = [
        'https://js.castle.io/v2/castle.js',
        'https://d2a8a4nxofan8h.cloudfront.net/v2/castle.js',
      ];
      let src = '';
      for (const u of candidates) {
        try {
          const r = await fetch(u);
          if (r.ok) { src = await r.text(); break; }
        } catch (e) {}
      }
      if (src) { (0, eval)(src); }
    }
    if (typeof window._castle !== 'function') return '';
    window._castle('setAppId', %s);
    const token = await window._castle('createRequestToken');
    return typeof token === 'string' ? token : '';
  } catch (e) {
    return '';
  }
})()`

// mintCastleToken loads the Castle SDK in the current tab and returns a fresh
// request token. It never returns an error: a Castle failure must not demote an
// otherwise-valid Turnstile solve, so callers simply get an empty string.
func (b *Browser) mintCastleToken(ctx context.Context) string {
	pk, _ := json.Marshal(CastlePublishableKey)
	script := fmt.Sprintf(castleMintScript, string(pk))
	mintCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var token string
	if err := chromedp.Run(mintCtx,
		chromedp.Evaluate(script, &token, chromedp.EvalAsValue),
	); err != nil {
		return ""
	}
	return token
}

// mouseClickTurnstileCenter ports Python _mouse_click_turnstile_center_trace.
func mouseClickTurnstileCenter(ctx context.Context) error {
	var raw any
	err := chromedp.Run(ctx, chromedp.Evaluate(`(function(){
  const e = document.querySelector('.cf-turnstile');
  if (!e) return null;
  const r = e.getBoundingClientRect();
  return {x: r.left + r.width / 2, y: r.top + r.height / 2};
})()`, &raw))
	if err != nil || raw == nil {
		return err
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	x, _ := m["x"].(float64)
	y, _ := m["y"].(float64)
	if x <= 0 || y <= 0 {
		return nil
	}
	// Python: move (x-25,y-8) → move (x,y) with 8 intermediate steps.
	return chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		startX, startY := max0(x-25), max0(y-8)
		if err := input.DispatchMouseEvent(input.MouseMoved, startX, startY).Do(ctx); err != nil {
			return err
		}
		for i := 1; i <= 8; i++ {
			px := startX + (x-startX)*float64(i)/8
			py := startY + (y-startY)*float64(i)/8
			if err := input.DispatchMouseEvent(input.MouseMoved, px, py).Do(ctx); err != nil {
				return err
			}
			time.Sleep(5 * time.Millisecond)
		}
		if err := input.DispatchMouseEvent(input.MousePressed, x, y).
			WithButton(input.Left).WithClickCount(1).Do(ctx); err != nil {
			return err
		}
		time.Sleep(50 * time.Millisecond)
		return input.DispatchMouseEvent(input.MouseReleased, x, y).
			WithButton(input.Left).WithClickCount(1).Do(ctx)
	}))
}

func max0(v float64) float64 {
	if v < 0 {
		return 0
	}
	return v
}

func pageDiag(ctx context.Context) string {
	var s string
	_ = chromedp.Run(ctx, chromedp.Evaluate(`(function(){
  var ifr=document.querySelectorAll('iframe[src*="challenges.cloudflare.com"], iframe[src*="turnstile"]').length;
  var allIfr=document.querySelectorAll('iframe').length;
  var w=!!document.querySelector('.cf-turnstile');
  var ts=!!window.turnstile;
  var tok=(document.querySelector('input[name="cf-turnstile-response"]')||{}).value||'';
  return 'iframes='+ifr+' all_ifr='+allIfr+' widget='+w+' turnstile='+ts+' toklen='+tok.length;
})()`, &s))
	if s == "" {
		return "(no diag)"
	}
	return s
}
