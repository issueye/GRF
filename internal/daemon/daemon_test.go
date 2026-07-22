package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPIDAliveCurrentProcess(t *testing.T) {
	if !PIDAlive(os.Getpid()) {
		t.Fatalf("current pid %d reported dead", os.Getpid())
	}
	if PIDAlive(-1) {
		t.Fatal("negative pid reported alive")
	}
}

func TestTryLockExclusiveAndReusable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.lock")
	unlock, err := TryLock(path)
	if err != nil {
		t.Fatalf("first lock: %v", err)
	}
	if _, err := TryLock(path); err == nil {
		unlock()
		t.Fatal("second overlapping lock succeeded")
	}
	unlock()

	unlockAgain, err := TryLock(path)
	if err != nil {
		t.Fatalf("lock after unlock: %v", err)
	}
	unlockAgain()
}

func TestStopRequestLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "stop.request")
	if err := RequestStop(path); err != nil {
		t.Fatalf("request stop: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stop request not created: %v", err)
	}
	ClearStop(path)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("stop request still exists: %v", err)
	}
}
