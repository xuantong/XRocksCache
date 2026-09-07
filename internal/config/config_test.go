package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadLoggingOptions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "xrockscache.conf")
	content := []byte("log-dir logs\nlog-level debug\nlog-format json\nlog-retention-days 7\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LogDir != "logs" {
		t.Fatalf("LogDir = %q", cfg.LogDir)
	}
	if cfg.LogLevel != "debug" {
		t.Fatalf("LogLevel = %q", cfg.LogLevel)
	}
	if cfg.LogFormat != "json" {
		t.Fatalf("LogFormat = %q", cfg.LogFormat)
	}
	if cfg.LogRetentionDays != 7 {
		t.Fatalf("LogRetentionDays = %d", cfg.LogRetentionDays)
	}
}

func TestRejectInvalidLogRetentionDays(t *testing.T) {
	path := filepath.Join(t.TempDir(), "xrockscache.conf")
	if err := os.WriteFile(path, []byte("log-retention-days -1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected invalid log-retention-days error")
	}
}
