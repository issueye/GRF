package config

import (
	"path/filepath"
	"testing"
)

func TestAccountHealthSettingsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.env")
	cfg := Defaults()
	if cfg.APIAccountHealthEnabled || cfg.APIAccountHealthIntervalMinutes != 30 {
		t.Fatalf("unexpected health defaults: enabled=%v interval=%d", cfg.APIAccountHealthEnabled, cfg.APIAccountHealthIntervalMinutes)
	}
	cfg.APIAccountHealthEnabled = true
	cfg.APIAccountHealthIntervalMinutes = 45
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.APIAccountHealthEnabled || loaded.APIAccountHealthIntervalMinutes != 45 {
		t.Fatalf("unexpected loaded health settings: enabled=%v interval=%d", loaded.APIAccountHealthEnabled, loaded.APIAccountHealthIntervalMinutes)
	}
}
