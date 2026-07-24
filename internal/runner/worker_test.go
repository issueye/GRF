package runner

import "testing"

func TestParseWorkerArgs(t *testing.T) {
	target, threads, runID, manualOAuth := parseWorkerArgs([]string{
		"--worker", "--target", "25", "--threads", "4", "--run-id", "run-1", "--manual-oauth",
	})
	if target != 25 || threads != 4 || runID != "run-1" || !manualOAuth {
		t.Fatalf("target=%d threads=%d runID=%q manualOAuth=%v", target, threads, runID, manualOAuth)
	}
}

func TestParseWorkerArgsDefaults(t *testing.T) {
	target, threads, runID, manualOAuth := parseWorkerArgs(nil)
	if target != 10 || threads != 2 || runID != "" || manualOAuth {
		t.Fatalf("target=%d threads=%d runID=%q manualOAuth=%v", target, threads, runID, manualOAuth)
	}
}
