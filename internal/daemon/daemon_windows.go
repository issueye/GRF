//go:build windows

package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

const stillActive = 259

func platformPIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActive
}

func platformTryLock(lockPath string) (func(), error) {
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	var overlapped windows.Overlapped
	flags := uint32(windows.LOCKFILE_EXCLUSIVE_LOCK | windows.LOCKFILE_FAIL_IMMEDIATELY)
	if err := windows.LockFileEx(windows.Handle(f.Fd()), flags, 0, 1, 0, &overlapped); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("注册机已经在运行（无法获取锁）")
	}
	return func() {
		_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &overlapped)
		_ = f.Close()
	}, nil
}

func platformStartBackground(exe string, args, env []string) (int, error) {
	cmd := exec.Command(exe, args...)
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_NO_WINDOW,
		HideWindow:    true,
	}
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release()
	return pid, nil
}

// Detached Windows processes cannot reliably receive console control events.
// The common stop-request file is the graceful mechanism on Windows.
func platformRequestStop(int) error { return nil }

func platformForceKill(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Kill()
}
