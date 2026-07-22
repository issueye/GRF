package home

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolvePlatformDefault(t *testing.T) {
	t.Setenv(EnvHome, "")
	t.Setenv(EnvHomeWindows, "")
	p, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}
	want := DirName
	if runtime.GOOS == "windows" {
		want = DirNameWindows
	}
	if filepath.Base(p.Root) != want {
		t.Fatalf("root=%q want base %q", p.Root, want)
	}
	if p.Config != filepath.Join(p.Root, "config.env") {
		t.Fatalf("config=%q", p.Config)
	}
}

func TestResolveExplicitHome(t *testing.T) {
	want := filepath.Join(t.TempDir(), "custom-home")
	if runtime.GOOS == "windows" {
		t.Setenv(EnvHomeWindows, want)
		t.Setenv(EnvHome, filepath.Join(t.TempDir(), "legacy-home"))
	} else {
		t.Setenv(EnvHome, want)
	}
	p, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}
	want, _ = filepath.Abs(want)
	if p.Root != want {
		t.Fatalf("root=%q want %q", p.Root, want)
	}
}

func TestPreferredEnvHome(t *testing.T) {
	want := EnvHome
	if runtime.GOOS == "windows" {
		want = EnvHomeWindows
	}
	if got := PreferredEnvHome(); got != want {
		t.Fatalf("env=%q want %q", got, want)
	}
}
