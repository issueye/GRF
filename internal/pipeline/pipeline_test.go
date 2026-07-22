package pipeline

import (
	"testing"

	"github.com/grok-free-register/grok-reg/internal/config"
)

func TestDeriveWorkersUsesRequestedPipelineWidth(t *testing.T) {
	for _, threads := range []int{1, 4, 8} {
		cfg := config.Defaults()
		cfg.Target = 10
		cfg.TurnstileWorkers = threads

		s, p, c, oa, phys := deriveWorkers(cfg)
		if s != threads || p != threads || c != threads || oa != threads || phys != threads {
			t.Fatalf("threads=%d got S=%d P=%d C=%d OAuth=%d phys=%d", threads, s, p, c, oa, phys)
		}
	}
}

func TestDeriveWorkersRespectsTargetAndPhysicalCap(t *testing.T) {
	cfg := config.Defaults()
	cfg.Target = 3
	cfg.TurnstileWorkers = 8

	s, p, c, oa, phys := deriveWorkers(cfg)
	if s != 3 || p != 3 || c != 3 || oa != 3 || phys != 3 {
		t.Fatalf("target cap got S=%d P=%d C=%d OAuth=%d phys=%d", s, p, c, oa, phys)
	}

	cfg.Target = 10
	cfg.PhysicalCap = 2
	s, p, c, oa, phys = deriveWorkers(cfg)
	if s != 2 || p != 2 || c != 2 || oa != 2 || phys != 2 {
		t.Fatalf("physical cap got S=%d P=%d C=%d OAuth=%d phys=%d", s, p, c, oa, phys)
	}
}
