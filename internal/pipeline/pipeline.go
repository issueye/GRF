package pipeline

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/grok-free-register/grok-reg/internal/clearance"
	"github.com/grok-free-register/grok-reg/internal/config"
	"github.com/grok-free-register/grok-reg/internal/cpa"
	"github.com/grok-free-register/grok-reg/internal/email"
	"github.com/grok-free-register/grok-reg/internal/home"
	"github.com/grok-free-register/grok-reg/internal/inventory"
	"github.com/grok-free-register/grok-reg/internal/logx"
	"github.com/grok-free-register/grok-reg/internal/manualoauth"
	"github.com/grok-free-register/grok-reg/internal/oauth"
	"github.com/grok-free-register/grok-reg/internal/onboarding"
	"github.com/grok-free-register/grok-reg/internal/protocol"
	"github.com/grok-free-register/grok-reg/internal/state"
	"github.com/grok-free-register/grok-reg/internal/turnstile"
)

type QItem struct {
	Email    string
	Password string
	Code     string
	Handle   email.Handle
	Castle   string // Castle request token reused for the signup body (JSON castleRequestToken)
}

type SSOJob struct {
	Email    string
	Password string
	SSO      string
}

type Options struct {
	Cfg         config.Config
	Paths       home.Paths
	Run         home.RunDirs
	Target      int
	Log         *logx.Logger
	Store       *state.Store
	GatewaySink AccountSink
	ManualOAuth bool
}

// AccountSink receives a successfully probed Build credential without coupling
// the registration pipeline to a particular gateway storage implementation.
type AccountSink interface {
	Import(context.Context, cpa.Document) error
}

type Engine struct {
	opt Options

	cm       *clearance.Manager
	xai      *protocol.Client
	mail     *email.Provider
	turn     turnstile.Provider
	oauth    *oauth.Client
	inv      *inventory.Inventory[string, QItem]
	phys     *inventory.Semaphore
	qPending *inventory.Semaphore
	width    int

	castleCh chan string // castle request tokens minted by S workers, consumed by P

	oauthCh  chan SSOJob
	uploader *cpa.Uploader

	done     atomic.Int64 // CPA successes (counts toward -t)
	reserved atomic.Int64 // in-flight accounts (email→register→oauth→probe)
	ssoN     atomic.Int64
	oaN      atomic.Int64
	fail     atomic.Int64

	start   time.Time
	wgReg   sync.WaitGroup // S/P/C
	wgOAuth sync.WaitGroup
	wgAux   sync.WaitGroup // status ticker etc
}

// remainingCapacity = target - done - reserved (how many new accounts may start).
func (e *Engine) remainingCapacity() int {
	n := e.opt.Target - int(e.done.Load()) - int(e.reserved.Load())
	if n < 0 {
		return 0
	}
	return n
}

// tryReserve claims one pipeline seat for a new account attempt.
func (e *Engine) tryReserve() bool {
	for {
		d := e.done.Load()
		r := e.reserved.Load()
		if d+r >= int64(e.opt.Target) {
			return false
		}
		if e.reserved.CompareAndSwap(r, r+1) {
			return true
		}
	}
}

func (e *Engine) releaseReserve() {
	for {
		r := e.reserved.Load()
		if r <= 0 {
			return
		}
		if e.reserved.CompareAndSwap(r, r-1) {
			return
		}
	}
}

// tryComplete moves a reserved seat into done. Returns (newDone, ok).
// ok=false means target already met (caller should discard extra success).
func (e *Engine) tryComplete() (int64, bool) {
	for {
		d := e.done.Load()
		if d >= int64(e.opt.Target) {
			e.releaseReserve()
			return d, false
		}
		if e.done.CompareAndSwap(d, d+1) {
			e.releaseReserve()
			return d + 1, true
		}
	}
}

// cfClearance returns the Cloudflare clearance cookie from the shared clearance
// bundle, if any (empty when clearance is disabled or has no cf_clearance).
func (e *Engine) cfClearance() string {
	if e.cm == nil {
		return ""
	}
	for _, c := range e.cm.Get().Cookies {
		if c.Name == "cf_clearance" {
			return c.Value
		}
	}
	return ""
}

// userAgent returns the clearance-reported User-Agent, falling back to the
// protocol default when clearance is unavailable.
func (e *Engine) userAgent() string {
	if e.cm != nil {
		if ua := e.cm.UserAgent(); ua != "" {
			return ua
		}
	}
	return protocol.DefaultUserAgent
}

func Run(ctx context.Context, opt Options) error {
	e := &Engine{
		opt:     opt,
		oauthCh: make(chan SSOJob, 64),
		start:   time.Now(),
	}
	return e.run(ctx)
}

func (e *Engine) run(ctx context.Context) error {
	cfg := e.opt.Cfg
	log := e.opt.Log
	st := e.opt.Store

	config.ApplyProxyEnv(cfg)

	sWorkers, pWorkers, cWorkers, oauthWorkers, physCap := deriveWorkers(cfg)
	e.width = sWorkers
	// Castle tokens are minted by S workers (which own a browser) and buffered
	// for P workers to attach to CreateEmailCode. Sized to the pipeline width so
	// S workers never block feeding it.
	e.castleCh = make(chan string, e.width)
	e.phys = inventory.NewSemaphore(physCap)
	// The UI's concurrency value is the end-to-end pipeline width. Keep all
	// queues at that width so no hidden fixed cap overrides the user's choice.
	qPend := e.width
	e.qPending = inventory.NewSemaphore(qPend)
	tSlots, qSlots := e.width, e.width
	e.inv = inventory.New[string, QItem](tSlots, qSlots)
	log.Infof("workers S=%d P=%d C=%d OAuth=%d phys=%d q_pending=%d", sWorkers, pWorkers, cWorkers, oauthWorkers, physCap, qPend)

	_ = st.Set(func(s *state.Snapshot) {
		s.Status = state.StatusRunning
		s.RunID = e.opt.Run.RunID
		s.Target = e.opt.Target
		s.Done = 0
		s.Phase = state.PhaseClearance
		s.PhaseDetail = "清障预热中"
		s.Workers = state.Workers{S: sWorkers, P: pWorkers, C: cWorkers, OAuth: oauthWorkers}
		s.PID = os.Getpid()
		s.StartedAt = e.start.UTC().Format(time.RFC3339)
		s.LogPath = e.opt.Run.LogPath
		s.OutputDir = e.opt.Run.Root
		s.Error = ""
	})

	// Clearance
	if cfg.ClearanceEnabled {
		e.cm = clearance.NewManager(cfg.FlareSolverrURL, cfg.ClearanceProxy, cfg.ClearanceURLs)
		msg, err := e.cm.Prewarm()
		if err != nil {
			log.Warnf("clearance: %v (%s)", err, msg)
		} else {
			log.Infof("[clearance] %s", msg)
		}
	} else {
		log.Info("[clearance] 未启用")
	}

	var err error
	e.xai, err = protocol.NewClient(cfg.RegisterProxy, e.cm)
	if err != nil {
		return err
	}
	e.mail = email.New(email.Config{
		Mode:              cfg.EmailMode,
		Domain:            cfg.EmailDomain,
		API:               cfg.EmailAPI,
		LOLRetries:        cfg.TempmailLOLRetries,
		LOLIntervalMS:     cfg.TempmailLOLIntervalMS,
		TestmailAPIKey:    cfg.TestmailAPIKey,
		TestmailNamespace: cfg.TestmailNamespace,
		TestmailDomain:    cfg.TestmailDomain,
	})
	if cfg.EmailMode == config.EmailTestmail {
		log.Infof("Email mode=testmail namespace=%s domain=%s", cfg.TestmailNamespace, cfg.TestmailDomain)
	} else {
		log.Infof("Email mode=%s", cfg.EmailMode)
	}
	e.turn = turnstile.New(turnstile.Options{
		Provider: cfg.TurnstileProvider,
		LiteURL:  cfg.LiteSolverURL,
		Proxy:    cfg.RegisterProxy,
		Clear:    e.cm,
		Workers:  sWorkers, // parallel S = pool slots
	})
	if c, ok := e.turn.(turnstile.Closer); ok {
		defer c.Close()
	}
	if w, ok := e.turn.(turnstile.Warmer); ok {
		if err := w.Warm(ctx); err != nil {
			return fmt.Errorf("turnstile warm: %w", err)
		}
	}
	log.Infof("Turnstile provider=%s workers=%d", e.turn.Name(), sWorkers)
	providerName := strings.ToLower(strings.TrimSpace(cfg.TurnstileProvider))
	if e.turn.Name() == "browser" && providerName != "chromedp" {
		log.Infof("Turnstile fallback: Python pool → one-shot mint → chromedp")
		log.Infof("Turnstile mint: python=%s pool=%s script=%s", turnstile.DetectedPython(), turnstile.DetectedPoolScript(), turnstile.DetectedScript())
	}
	e.uploader = cpa.NewUploader(cpa.UploadConfig{
		Enabled:      cfg.CPAUploadEnabled,
		BaseURL:      cfg.CPAManagementBase,
		Key:          cfg.CPAManagementKey,
		TimeoutSec:   cfg.CPAUploadTimeoutSec,
		Retries:      cfg.CPAUploadRetries,
		NameTemplate: cfg.CPAUploadNameTemplate,
		Verify:       cfg.CPAUploadVerify,
		Mode:         cfg.CPAUploadMode,
	}, func(f string, a ...any) {
		log.Infof(f, a...)
	})
	if e.uploader.Enabled() {
		log.Infof("CPA upload enabled base=%s", cfg.CPAManagementBase)
	}
	if !e.opt.ManualOAuth {
		e.oauth, err = oauth.NewClient(cfg.RegisterProxy, e.cm, time.Duration(cfg.OAuthRetrySec)*time.Second)
		if err != nil {
			return err
		}
	}
	_ = st.Set(func(s *state.Snapshot) {
		s.Phase = state.PhaseRegister
		s.PhaseDetail = "获取注册配置"
	})
	log.Info("Fetching signup config...")
	scfg, err := e.xai.FetchConfig()
	if err != nil {
		_ = st.Set(func(s *state.Snapshot) {
			s.Status = state.StatusError
			s.Error = err.Error()
			s.PhaseDetail = "配置获取失败"
		})
		return fmt.Errorf("config fetch: %w", err)
	}
	log.Infof("SITE_KEY=%s ACTION_ID=%s...", scfg.SiteKey, trim(scfg.ActionID, 12))
	log.OKf("注册服务已启动 | 目标 %d | run=%s", e.opt.Target, e.opt.Run.RunID)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// signal
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sigCh)
	go func() {
		select {
		case <-sigCh:
			log.Warn("收到停止信号，正在退出...")
			cancel()
		case <-ctx.Done():
		}
	}()

	// Detached Windows workers cannot reliably receive POSIX-style signals.
	// Polling a request file gives grok stop the same graceful cancellation path
	// on every supported platform.
	if stopPath := e.opt.Paths.Stop; stopPath != "" {
		e.wgAux.Add(1)
		go func() {
			defer e.wgAux.Done()
			ticker := time.NewTicker(250 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if _, err := os.Stat(stopPath); err == nil {
						_ = os.Remove(stopPath)
						log.Warn("收到停止请求，正在退出...")
						cancel()
						return
					}
				}
			}
		}()
	}

	// status ticker
	e.wgAux.Add(1)
	go func() {
		defer e.wgAux.Done()
		t := time.NewTicker(3 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				e.refreshState()
			}
		}
	}()

	for i := 0; i < sWorkers; i++ {
		e.wgReg.Add(1)
		go e.sWorker(ctx, i, scfg)
	}
	for i := 0; i < pWorkers; i++ {
		e.wgReg.Add(1)
		go e.pWorker(ctx, i)
	}
	for i := 0; i < cWorkers; i++ {
		e.wgReg.Add(1)
		go e.cWorker(ctx, i, scfg)
	}
	for i := 0; i < oauthWorkers; i++ {
		e.wgOAuth.Add(1)
		go e.oauthWorker(ctx, i)
	}

	// wait until target or cancel
	for {
		if int(e.done.Load()) >= e.opt.Target {
			log.OKf("已达目标 %d，停止", e.opt.Target)
			cancel()
			break
		}
		select {
		case <-ctx.Done():
			goto shutdown
		case <-time.After(500 * time.Millisecond):
		}
	}
shutdown:
	// 1) stop S/P/C producers (ctx canceled)
	// 2) wait register workers so no more sends to oauthCh
	// 3) close oauthCh so OAuth workers exit range
	waitGroupTimeout(&e.wgReg, 15*time.Second, log, "register workers")
	close(e.oauthCh)
	waitGroupTimeout(&e.wgOAuth, 30*time.Second, log, "oauth workers")
	waitGroupTimeout(&e.wgAux, 3*time.Second, log, "aux")

	_ = st.Set(func(s *state.Snapshot) {
		if s.Status != state.StatusError {
			s.Status = state.StatusStopped
		}
		s.Phase = state.PhaseIdle
		s.PhaseDetail = fmt.Sprintf("完成 %d/%d", e.done.Load(), e.opt.Target)
		s.Done = int(e.done.Load())
		s.SSOCount = int(e.ssoN.Load())
		s.OAuthCount = int(e.oaN.Load())
		s.FailCount = int(e.fail.Load())
		s.PID = 0
	})
	log.Infof("结束 done=%d sso=%d oauth=%d fail=%d", e.done.Load(), e.ssoN.Load(), e.oaN.Load(), e.fail.Load())
	return nil
}

func (e *Engine) refreshState() {
	elapsed := time.Since(e.start).Minutes()
	rate := 0.0
	if elapsed > 0 {
		rate = float64(e.done.Load()) / elapsed
	}
	t, q := e.inv.Depths()
	_ = e.opt.Store.Set(func(s *state.Snapshot) {
		s.Done = int(e.done.Load())
		s.SSOCount = int(e.ssoN.Load())
		s.OAuthCount = int(e.oaN.Load())
		s.FailCount = int(e.fail.Load())
		s.RatePerMin = rate
		if s.Phase == state.PhaseRegister || s.Phase == "" {
			s.PhaseDetail = fmt.Sprintf("注册中 T=%d Q=%d done=%d/%d inflight=%d", t, q, e.done.Load(), e.opt.Target, e.reserved.Load())
		}
	})
}

func waitGroupTimeout(wg *sync.WaitGroup, d time.Duration, log *logx.Logger, name string) {
	ch := make(chan struct{})
	go func() {
		wg.Wait()
		close(ch)
	}()
	select {
	case <-ch:
	case <-time.After(d):
		log.Warnf("%s 退出超时", name)
	}
}

func (e *Engine) sWorker(ctx context.Context, id int, scfg protocol.SignupConfig) {
	defer e.wgReg.Done()
	log := e.opt.Log
	pageURL := protocol.SiteURL + "/sign-up"
	for {
		if e.remainingCapacity() <= 0 && int(e.done.Load()) >= e.opt.Target {
			return
		}
		// Don't mint far ahead of what we still need.
		if e.remainingCapacity() <= 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(500 * time.Millisecond):
			}
			if int(e.done.Load()) >= e.opt.Target {
				return
			}
			continue
		}
		tDepth, _ := e.inv.Depths()
		need := e.remainingCapacity()
		if need < 1 {
			need = 1
		}
		if tDepth >= need {
			select {
			case <-ctx.Done():
				return
			case <-time.After(400 * time.Millisecond):
			}
			continue
		}
		select {
		case <-ctx.Done():
			return
		default:
		}
		if err := e.phys.Acquire(ctx); err != nil {
			return
		}
		// Prefer SolveFull so a Castle request token is minted in the same browser
		// session. Providers without a browser (lite/chromedp) fall back to Solve.
		var tok, castle string
		if m, ok := e.turn.(turnstile.CastleMinter); ok {
			res, err := m.SolveFull(ctx, scfg.SiteKey, pageURL)
			e.phys.Release()
			if err != nil {
				log.Warnf("[S%d] turnstile: %v", id, err)
				select {
				case <-ctx.Done():
					return
				case <-time.After(2 * time.Second):
				}
				continue
			}
			tok, castle = res.Token, res.Castle
		} else {
			var err error
			tok, err = e.turn.Solve(ctx, scfg.SiteKey, pageURL)
			e.phys.Release()
			if err != nil {
				log.Warnf("[S%d] turnstile: %v", id, err)
				select {
				case <-ctx.Done():
					return
				case <-time.After(2 * time.Second):
				}
				continue
			}
		}
		if err := e.inv.PutT(ctx, tok, 5*time.Minute); err != nil {
			return
		}
		// Offer the castle token to the P-stage buffer (non-blocking: if no P
		// worker needs it yet and the buffer is full, drop it — Castle tokens are
		// cheap to regenerate and a missing one just degrades trust, not safety).
		if castle != "" {
			select {
			case e.castleCh <- castle:
			default:
			}
		}
		log.Infof("[S%d] token ok (len=%d castle=%v)", id, len(tok), castle != "")
	}
}

func (e *Engine) pWorker(ctx context.Context, id int) {
	defer e.wgReg.Done()
	log := e.opt.Log
	for {
		if int(e.done.Load()) >= e.opt.Target {
			return
		}
		select {
		case <-ctx.Done():
			return
		default:
		}
		// Global seat: done + reserved <= target (not per-worker).
		if e.remainingCapacity() <= 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(500 * time.Millisecond):
			}
			if int(e.done.Load()) >= e.opt.Target {
				return
			}
			continue
		}
		_, qDepth := e.inv.Depths()
		qCap := e.remainingCapacity()
		if qCap > e.width {
			qCap = e.width
		}
		if qCap < 1 {
			qCap = 1
		}
		if qDepth >= qCap {
			select {
			case <-ctx.Done():
				return
			case <-time.After(800 * time.Millisecond):
			}
			continue
		}

		// Reserve seat BEFORE creating email so multi-P cannot overshoot -t.
		if !e.tryReserve() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(300 * time.Millisecond):
			}
			continue
		}

		if err := e.qPending.Acquire(ctx); err != nil {
			e.releaseReserve()
			return
		}
		h, err := e.mail.Create()
		if err != nil {
			e.qPending.Release()
			e.releaseReserve()
			log.Debugf("[P%d] create email: %v", id, err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
			continue
		}
		// Attach a Castle request token (protobuf field 3) when one is buffered;
		// its absence lowers trust and tends to trigger OAuth invalid_grant later.
		// Non-blocking fetch: drop to "" if no S worker has produced one yet.
		var castle string
		select {
		case castle = <-e.castleCh:
		default:
		}
		if err := e.xai.CreateEmailCode(h.Email, castle); err != nil {
			e.qPending.Release()
			e.releaseReserve()
			log.Debugf("[P%d] create code %s: %v", id, h.Email, err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
			continue
		}
		code, err := e.mail.PollCode(h, 90*time.Second)
		if err != nil {
			e.qPending.Release()
			e.releaseReserve()
			log.Debugf("[P%d] poll code: %v", id, err)
			continue
		}
		item := QItem{Email: h.Email, Password: h.Password, Code: code, Handle: h, Castle: castle}
		if err := e.inv.PutQ(ctx, item, 2*time.Minute); err != nil {
			e.qPending.Release()
			e.releaseReserve()
			return
		}
		e.qPending.Release()
		// seat stays reserved until signup fail / oauth fail / CPA success
		log.Debugf("[P%d] Q ready %s (reserved=%d done=%d/%d)", id, h.Email, e.reserved.Load(), e.done.Load(), e.opt.Target)
	}
}

func (e *Engine) cWorker(ctx context.Context, id int, scfg protocol.SignupConfig) {
	defer e.wgReg.Done()
	log := e.opt.Log
	for {
		if int(e.done.Load()) >= e.opt.Target {
			return
		}
		pair, err := e.inv.ClaimPair(ctx)
		if err != nil {
			return
		}
		token := pair.T.Value
		q := pair.Q.Value
		_ = e.opt.Store.Set(func(s *state.Snapshot) {
			s.Phase = state.PhaseRegister
			s.PhaseDetail = fmt.Sprintf("正在注册 %s", q.Email)
		})
		log.Startf("开始注册 %s", q.Email)

		e.xai.ClearAuthCookies()
		if err := e.xai.VerifyEmailCode(q.Email, q.Code); err != nil {
			log.Warnf("verify fail %s: %v", q.Email, err)
			pair.Release()
			e.fail.Add(1)
			e.releaseReserve()
			continue
		}
		body := protocol.BuildSignupBody(q.Email, q.Password, q.Code, token, q.Castle)
		text, sso, err := e.xai.SignupServerAction(body, scfg.ActionID, scfg.StateTree)
		if sso == "" {
			sso = protocol.ExtractSSOFromText(text)
		}
		pair.Release()
		if err != nil || sso == "" {
			preview := text
			if len(preview) > 180 {
				preview = preview[:180]
			}
			log.Warnf("signup fail %s: err=%v sso=%v body=%q", q.Email, err, sso != "", preview)
			e.fail.Add(1)
			e.releaseReserve() // free seat for another attempt
			continue
		}

		// ensure run dirs exist (first credential)
		accPath := filepath.Join(e.opt.Run.SSO, "accounts.txt")
		if err := cpa.AppendSSO(accPath, q.Email, q.Password, sso); err != nil {
			log.Warnf("write sso: %v", err)
		}
		_ = cpa.AppendAuthSession(filepath.Join(e.opt.Run.SSO, "auth-sessions.jsonl"), q.Email, sso)
		n := e.ssoN.Add(1)
		log.OKf("注册成功 #%d %s", n, q.Email)

		job := SSOJob{Email: q.Email, Password: q.Password, SSO: sso}
		select {
		case <-ctx.Done():
			e.releaseReserve()
			return
		case e.oauthCh <- job:
		default:
			select {
			case <-ctx.Done():
				e.releaseReserve()
				return
			case e.oauthCh <- job:
			}
		}
	}
}

func (e *Engine) oauthWorker(ctx context.Context, id int) {
	defer e.wgOAuth.Done()
	log := e.opt.Log
	tasks := manualoauth.NewStore(e.opt.Run.Root)
	minInterval := time.Duration(e.opt.Cfg.OAuthMinIntervalSec * float64(time.Second))
	if minInterval <= 0 {
		minInterval = 10 * time.Second
	}
	var last time.Time
	for job := range e.oauthCh {
		// Soft-stop: still drain with seat accounting, but skip work past target.
		if int(e.done.Load()) >= e.opt.Target {
			e.releaseReserve()
			continue
		}
		var cred oauth.Credential
		var err error
		if e.opt.ManualOAuth {
			_ = e.opt.Store.Set(func(s *state.Snapshot) {
				s.Phase = state.PhaseOAuth
				s.PhaseDetail = fmt.Sprintf("等待人工 OAuth (%s)", job.Email)
			})
			task, enqueueErr := tasks.Enqueue(e.opt.Run.RunID, job.Email, job.Password, job.SSO)
			if enqueueErr != nil {
				log.Warnf("创建人工 OAuth 任务失败 %s: %v", job.Email, enqueueErr)
				e.fail.Add(1)
				e.releaseReserve()
				continue
			}
			log.Startf("等待人工 OAuth %s", job.Email)
			cred, err = tasks.WaitAuthorized(ctx, task.ID, 500*time.Millisecond)
			_ = tasks.Remove(task.ID)
			if err != nil {
				if ctx.Err() == nil {
					log.Warnf("人工 OAuth 失败 %s: %v", job.Email, err)
					e.fail.Add(1)
				}
				e.releaseReserve()
				continue
			}
			log.OKf("人工 OAuth 完成 %s", job.Email)
		} else {
			if !last.IsZero() {
				if delay := time.Until(last.Add(minInterval)); delay > 0 {
					select {
					case <-ctx.Done():
						return
					case <-time.After(delay):
					}
				}
			}
			last = time.Now()
			_ = e.opt.Store.Set(func(s *state.Snapshot) {
				s.Phase = state.PhaseOAuth
				s.PhaseDetail = fmt.Sprintf("正在 OAuth (%s)", job.Email)
			})
			log.Startf("OAuth %s", job.Email)
			cred, err = e.oauth.Exchange(ctx, job.SSO)
			if err != nil {
				log.Warnf("OAuth fail %s: %v", job.Email, err)
				e.fail.Add(1)
				e.releaseReserve()
				continue
			}
		}
		e.oaN.Add(1)
		// Onboarding (TOS / birth date / NSFW) is best-effort: a failure does not
		// demote a successfully registered account, matching probe semantics.
		if e.opt.Cfg.OnboardingEnabled {
			onbRes, onbErr := onboarding.Run(ctx, job.SSO, e.cfClearance(), onboarding.Options{
				Proxy:     e.opt.Cfg.RegisterProxy,
				UserAgent: e.userAgent(),
			}, log)
			if onbErr != nil {
				log.Warnf("onboarding 部分失败 %s: %v (TOS:%v 生日:%v NSFW:%v)", job.Email, onbErr, onbRes.TOS, onbRes.BirthDate, onbRes.NSFW)
			} else {
				log.OKf("onboarding 完成 %s", job.Email)
			}
		}
		doc := cpa.FromCredential(cred, job.Email)
		_ = e.opt.Store.Set(func(s *state.Snapshot) {
			s.Phase = state.PhaseProbe
			s.PhaseDetail = fmt.Sprintf("探活 %s", job.Email)
		})
		if e.opt.Cfg.ProbeEnabled {
			if err := cpa.Probe(doc, e.opt.Cfg.RegisterProxy); err != nil {
				log.Warnf("探活失败 %s: %v", job.Email, err)
				path, _ := cpa.WriteAtomic(e.opt.Run.Discarded, doc, cpa.DefaultSecret())
				_ = path
				e.fail.Add(1)
				e.releaseReserve()
				continue
			}
		}
		// Atomic complete: prevents multi-OAuth overshoot of -t.
		d, ok := e.tryComplete()
		if !ok {
			// Target already filled by another worker — keep file in discarded.
			path, _ := cpa.WriteAtomic(e.opt.Run.Discarded, doc, cpa.DefaultSecret())
			log.Warnf("已达目标，额外号移入 discarded: %s (%s)", job.Email, filepath.Base(path))
			continue
		}
		path, err := cpa.WriteAtomic(e.opt.Run.CPA, doc, cpa.DefaultSecret())
		if err != nil {
			log.Warnf("写 CPA 失败: %v", err)
			// seat already converted to done; count as fail but don't re-open flood
			e.fail.Add(1)
			continue
		}
		if e.uploader != nil && e.uploader.Enabled() {
			up := e.uploader
			docCopy := doc
			go func() {
				defer func() { _ = recover() }()
				_ = up.UploadDocument(docCopy)
			}()
		}
		if e.opt.GatewaySink != nil {
			if err := e.opt.GatewaySink.Import(ctx, doc); err != nil {
				log.Warnf("网关账号导入失败 %s: %v", job.Email, err)
			}
		}
		log.OKf("CPA 就绪 #%d/%d %s -> %s", d, e.opt.Target, job.Email, filepath.Base(path))
		e.refreshState()
	}
}

func deriveWorkers(cfg config.Config) (s, p, c, oa, phys int) {
	width := cfg.TurnstileWorkers
	if width <= 0 {
		cpus := runtime.NumCPU()
		width = cpus
		if width > 4 {
			width = 4
		}
		if width < 2 {
			width = 2
		}
	}
	if width > 8 {
		width = 8
	}
	if width < 1 {
		width = 1
	}
	if cfg.PhysicalCap > 0 && cfg.PhysicalCap < width {
		width = cfg.PhysicalCap
	}
	if cfg.Target > 0 && cfg.Target < width {
		width = cfg.Target
	}
	return width, width, width, width, width
}

func trim(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
