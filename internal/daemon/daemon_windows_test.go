//go:build windows

package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/grok-free-register/grok-reg/internal/home"
)

func TestWindowsBackgroundHelper(t *testing.T) {
	if os.Getenv("GROK_WINDOWS_HELPER_PROCESS") != "1" {
		return
	}
	if stopPath := os.Getenv("GROK_WINDOWS_HELPER_STOP"); stopPath != "" {
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(stopPath); err == nil {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		return
	}
	time.Sleep(30 * time.Second)
}

func TestWindowsBackgroundLifecycle(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "GROK_WINDOWS_HELPER_PROCESS=1")
	pid, err := platformStartBackground(exe, []string{"-test.run=TestWindowsBackgroundHelper"}, env)
	if err != nil {
		t.Fatalf("start background: %v", err)
	}
	t.Cleanup(func() {
		if platformPIDAlive(pid) {
			_ = platformForceKill(pid)
		}
	})

	deadline := time.Now().Add(3 * time.Second)
	for !platformPIDAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !platformPIDAlive(pid) {
		t.Fatalf("background pid %d did not become alive", pid)
	}
	if err := platformForceKill(pid); err != nil {
		t.Fatalf("kill background: %v", err)
	}
	deadline = time.Now().Add(3 * time.Second)
	for platformPIDAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if platformPIDAlive(pid) {
		t.Fatalf("background pid %d still alive after kill", pid)
	}
}

func TestWindowsGracefulStopRequest(t *testing.T) {
	dir := t.TempDir()
	paths := home.Paths{
		PID:   filepath.Join(dir, "run.pid"),
		Stop:  filepath.Join(dir, "stop.request"),
		State: filepath.Join(dir, "state.json"),
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(),
		"GROK_WINDOWS_HELPER_PROCESS=1",
		"GROK_WINDOWS_HELPER_STOP="+paths.Stop,
	)
	pid, err := platformStartBackground(exe, []string{"-test.run=TestWindowsBackgroundHelper"}, env)
	if err != nil {
		t.Fatalf("start background: %v", err)
	}
	t.Cleanup(func() {
		if platformPIDAlive(pid) {
			_ = platformForceKill(pid)
		}
	})
	if err := WritePID(paths.PID, pid); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for !platformPIDAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if err := Stop(paths); err != nil {
		t.Fatalf("graceful stop: %v", err)
	}
	if platformPIDAlive(pid) {
		t.Fatalf("pid %d still alive", pid)
	}
	if _, err := os.Stat(paths.PID); !os.IsNotExist(err) {
		t.Fatalf("pid file remains: %v", err)
	}
	if _, err := os.Stat(paths.Stop); !os.IsNotExist(err) {
		t.Fatalf("stop request remains: %v", err)
	}
}
