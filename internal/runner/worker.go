package runner

import (
	"context"
	"os"
	"strconv"

	"github.com/grok-free-register/grok-reg/internal/config"
	"github.com/grok-free-register/grok-reg/internal/daemon"
	"github.com/grok-free-register/grok-reg/internal/gateway"
	"github.com/grok-free-register/grok-reg/internal/home"
	"github.com/grok-free-register/grok-reg/internal/logx"
	"github.com/grok-free-register/grok-reg/internal/pipeline"
	"github.com/grok-free-register/grok-reg/internal/state"
)

// RunWorker executes the detached registration worker. It is shared by the
// CLI and the Wails desktop binary so either executable can re-exec itself.
func RunWorker(args []string) error {
	target, threads, runID, manualOAuth := parseWorkerArgs(args)
	var err error
	target, err = config.ClampTarget(target)
	if err != nil {
		return err
	}
	threads, err = config.ClampThreads(threads)
	if err != nil {
		if threads < 1 {
			threads = 1
		}
		if threads > 8 {
			threads = 8
		}
	}

	p, err := home.Resolve()
	if err != nil {
		return err
	}
	if err := p.EnsureBase(); err != nil {
		return err
	}
	_ = config.SyncExample(p.Root)

	unlock, err := daemon.TryLock(p.Lock)
	if err != nil {
		return err
	}
	defer unlock()
	daemon.ClearStop(p.Stop)
	defer daemon.ClearStop(p.Stop)

	if err := daemon.WritePID(p.PID, os.Getpid()); err != nil {
		return err
	}
	defer daemon.ClearPID(p.PID)

	cfg, err := config.Load(p.Config)
	if err != nil {
		return err
	}
	cfg.Target = target
	cfg.TurnstileWorkers = threads
	var gatewaySink *gateway.Sink
	if sink, sinkErr := gateway.OpenSink(context.Background(), p.GatewayDB, p.GatewayKey); sinkErr != nil {
		// Registration remains usable if the optional gateway database is unavailable.
		// The CPA file remains the source of truth and can be imported later.
	} else {
		gatewaySink = sink
		defer gatewaySink.Close()
	}

	run, err := p.PrepareRun(runID)
	if err != nil {
		return err
	}
	log, err := logx.New(run.LogPath)
	if err != nil {
		return err
	}
	defer log.Close()

	st := state.NewStore(p.State)
	_ = st.Set(func(s *state.Snapshot) {
		s.Status = state.StatusRunning
		s.RunID = run.RunID
		s.Target = target
		s.PID = os.Getpid()
		s.LogPath = run.LogPath
		s.OutputDir = run.Root
		s.Workers = state.Workers{S: threads}
	})

	err = pipeline.Run(context.Background(), pipeline.Options{
		Cfg: cfg, Paths: p, Run: run, Target: target, Log: log, Store: st, GatewaySink: gatewaySink,
		ManualOAuth: manualOAuth,
	})
	if err != nil {
		_ = st.Set(func(s *state.Snapshot) {
			s.Status = state.StatusError
			s.Error = err.Error()
			s.PhaseDetail = "错误退出"
			s.PID = 0
		})
		log.Errf("%v", err)
		return err
	}
	return nil
}

func parseWorkerArgs(args []string) (target, threads int, runID string, manualOAuth bool) {
	target, threads = 10, 2
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--target":
			if i+1 < len(args) {
				if n, err := strconv.Atoi(args[i+1]); err == nil && n > 0 {
					target = n
				}
				i++
			}
		case "--threads", "--thread":
			if i+1 < len(args) {
				if n, err := strconv.Atoi(args[i+1]); err == nil && n > 0 {
					threads = n
				}
				i++
			}
		case "--run-id":
			if i+1 < len(args) {
				runID = args[i+1]
				i++
			}
		case "--manual-oauth":
			manualOAuth = true
		}
	}
	return target, threads, runID, manualOAuth
}
