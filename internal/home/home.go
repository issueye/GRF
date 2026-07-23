package home

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

const (
	EnvHome        = "GROK_HOME"
	EnvHomeWindows = "GRF_HOME"
	DirName        = ".grf"
	DirNameWindows = ".grf"
)

// Paths holds all filesystem locations under the platform data root.
type Paths struct {
	Root       string
	Config     string
	PID        string
	Lock       string
	Stop       string
	State      string
	LogsDir    string
	Outputs    string
	Clearance  string // optional: bundled compose path override
	GatewayDir string
	GatewayDB  string
	GatewayKey string
}

func Resolve() (Paths, error) {
	root := ""
	dirName := DirName
	if runtime.GOOS == "windows" {
		root = os.Getenv(EnvHomeWindows)
		if root == "" {
			// Backward-compatible fallback for installations that already set it.
			root = os.Getenv(EnvHome)
		}
		dirName = DirNameWindows
	} else {
		root = os.Getenv(EnvHome)
	}
	if root == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return Paths{}, err
		}
		root = filepath.Join(h, dirName)
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return Paths{}, err
	}
	p := Paths{
		Root:       root,
		Config:     filepath.Join(root, "config.env"),
		PID:        filepath.Join(root, "run.pid"),
		Lock:       filepath.Join(root, "run.lock"),
		Stop:       filepath.Join(root, "stop.request"),
		State:      filepath.Join(root, "state.json"),
		LogsDir:    filepath.Join(root, "logs"),
		Outputs:    filepath.Join(root, "outputs"),
		GatewayDir: filepath.Join(root, "gateway"),
		GatewayDB:  filepath.Join(root, "gateway", "gateway.db"),
		GatewayKey: filepath.Join(root, "gateway", "credential.key"),
	}
	return p, nil
}

// PreferredEnvHome returns the platform-native data-root environment name.
func PreferredEnvHome() string {
	if runtime.GOOS == "windows" {
		return EnvHomeWindows
	}
	return EnvHome
}

func (p Paths) EnsureBase() error {
	for _, d := range []string{p.Root, p.LogsDir, p.Outputs, p.GatewayDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
	}
	return nil
}

// NewRunID returns yyyymmdd-HHMMSS in local time.
func NewRunID() string {
	return time.Now().Format("20060102-150405")
}

// RunDirs is created before the first credential file is written.
type RunDirs struct {
	RunID     string
	Root      string
	SSO       string
	CPA       string
	Discarded string
	LogPath   string
}

func (p Paths) PrepareRun(runID string) (RunDirs, error) {
	if runID == "" {
		runID = NewRunID()
	}
	root := filepath.Join(p.Outputs, runID)
	rd := RunDirs{
		RunID:     runID,
		Root:      root,
		SSO:       filepath.Join(root, "SSO"),
		CPA:       filepath.Join(root, "CPA"),
		Discarded: filepath.Join(root, "discarded"),
		LogPath:   filepath.Join(p.LogsDir, fmt.Sprintf("run-%s.log", runID)),
	}
	for _, d := range []string{rd.Root, rd.SSO, rd.CPA, rd.Discarded} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return RunDirs{}, err
		}
	}
	return rd, nil
}
