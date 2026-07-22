package runner

import "testing"

func TestParseWorkerArgs(t *testing.T) {
	target, threads, runID := parseWorkerArgs([]string{
		"--worker", "--target", "25", "--threads", "4", "--run-id", "run-1",
	})
	if target != 25 || threads != 4 || runID != "run-1" {
		t.Fatalf("target=%d threads=%d runID=%q", target, threads, runID)
	}
}

func TestParseWorkerArgsDefaults(t *testing.T) {
	target, threads, runID := parseWorkerArgs(nil)
	if target != 10 || threads != 2 || runID != "" {
		t.Fatalf("target=%d threads=%d runID=%q", target, threads, runID)
	}
}
