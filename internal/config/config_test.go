package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadLoggingOptions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "xrockscache.conf")
	content := []byte("log-dir logs\nlog-level debug\nlog-format json\nlog-retention-days 7\nactive-expire-enabled no\nactive-expire-bucket-seconds 60\nactive-expire-interval-seconds 15\nactive-expire-cycle-budget-ms 20\nactive-expire-max-deletes-per-cycle 2000\n")
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
	if cfg.ActiveExpireEnabled {
		t.Fatal("ActiveExpireEnabled = true")
	}
	if cfg.ActiveExpireBucketSeconds != 60 {
		t.Fatalf("ActiveExpireBucketSeconds = %d", cfg.ActiveExpireBucketSeconds)
	}
	if cfg.ActiveExpireIntervalSeconds != 15 {
		t.Fatalf("ActiveExpireIntervalSeconds = %d", cfg.ActiveExpireIntervalSeconds)
	}
	if cfg.ActiveExpireCycleBudgetMilliseconds != 20 {
		t.Fatalf("ActiveExpireCycleBudgetMilliseconds = %d", cfg.ActiveExpireCycleBudgetMilliseconds)
	}
	if cfg.ActiveExpireMaxDeletesPerCycle != 2000 {
		t.Fatalf("ActiveExpireMaxDeletesPerCycle = %d", cfg.ActiveExpireMaxDeletesPerCycle)
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

func TestRejectInvalidActiveExpireOptions(t *testing.T) {
	tests := []string{
		"active-expire-enabled maybe\n",
		"active-expire-bucket-seconds 0\n",
		"active-expire-interval-seconds 0\n",
		"active-expire-cycle-budget-ms 0\n",
		"active-expire-max-deletes-per-cycle 0\n",
	}
	for _, content := range tests {
		path := filepath.Join(t.TempDir(), "xrockscache.conf")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil {
			t.Fatalf("expected error for %q", content)
		}
	}
}
