package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"xrockscache/internal/config"
)

func TestLoadLoggingOptions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "xrockscache.conf")
	content := []byte("log-dir logs\nlog-level debug\nlog-format json\nlog-retention-days 7\nactive-expire-enabled no\nactive-expire-bucket-seconds 60\nactive-expire-interval-seconds 15\nactive-expire-cycle-budget-ms 20\nactive-expire-max-deletes-per-cycle 2000\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(path)
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

func TestWriteRateConfig(t *testing.T) {
	if config.Default().WriteRateMiB != 35 {
		t.Fatal("unexpected baseline")
	}
	for _, value := range []string{"0", "-1", "1025", "bad"} {
		path := filepath.Join(t.TempDir(), "config")
		if err := os.WriteFile(path, []byte("write-rate-mib "+value+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := config.Load(path); err == nil {
			t.Fatalf("accepted %s", value)
		}
	}
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte("write-rate-mib 20\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil || cfg.WriteRateMiB != 20 {
		t.Fatalf("override: %v %v", cfg.WriteRateMiB, err)
	}
}

func TestESSDPL1Baseline(t *testing.T) {
	cfg := config.Default()
	if cfg.DiskType != "cloud_essd" || cfg.DiskPL != "pl1" || cfg.DiskCapacityGiB != 100 {
		t.Fatalf("unexpected ESSD baseline: %+v", cfg)
	}
	for _, line := range []string{"disk-pl pl0\n", "disk-type cloud_ssd\n", "disk-capacity-gib 19\n"} {
		path := filepath.Join(t.TempDir(), "config")
		if err := os.WriteFile(path, []byte(line), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := config.Load(path); err == nil {
			t.Fatalf("accepted invalid baseline %q", line)
		}
	}
}

func TestRejectInvalidLogRetentionDays(t *testing.T) {
	path := filepath.Join(t.TempDir(), "xrockscache.conf")
	if err := os.WriteFile(path, []byte("log-retention-days -1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(path); err == nil {
		t.Fatal("expected invalid log-retention-days error")
	}
}

func TestMissingAndUnknownConfigFail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.conf")
	if _, err := config.Load(path); err == nil {
		t.Fatal("missing configuration accepted")
	}
	if err := os.WriteFile(path, []byte("requrepass secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(path); err == nil {
		t.Fatal("unknown authentication key accepted")
	}
	if err := os.WriteFile(path, []byte("requirepass secret#suffix\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil || cfg.RequirePass != "secret#suffix" {
		t.Fatalf("password truncated: %v", err)
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
		if _, err := config.Load(path); err == nil {
			t.Fatalf("expected error for %q", content)
		}
	}
}
