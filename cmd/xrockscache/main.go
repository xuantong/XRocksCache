package main

import (
	"flag"
	"log/slog"
	"os"
	"time"

	"xrockscache/internal/config"
	"xrockscache/internal/logging"
	"xrockscache/internal/server"
	"xrockscache/internal/store"
)

const version = "0.2.0-go"

func main() {
	var configPath string
	var showVersion bool
	var bind string
	var dir string
	var requirePass string
	var port int

	flag.StringVar(&configPath, "c", "xrockscache.conf", "path to xrockscache config file")
	flag.BoolVar(&showVersion, "version", false, "print version and exit")
	flag.StringVar(&bind, "bind", "", "override bind address")
	flag.IntVar(&port, "port", 0, "override listen port")
	flag.StringVar(&dir, "dir", "", "override data directory")
	flag.StringVar(&requirePass, "requirepass", "", "override password")
	flag.Parse()

	if showVersion {
		_, _ = os.Stdout.WriteString("xrockscache " + version + "\n")
		return
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		slog.New(slog.NewTextHandler(os.Stderr, nil)).Error("load config failed", "error", err)
		os.Exit(1)
	}

	visited := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { visited[f.Name] = true })
	if visited["bind"] {
		cfg.Bind = bind
	}
	if visited["port"] {
		cfg.Port = port
	}
	if visited["dir"] {
		cfg.Dir = dir
	}
	if visited["requirepass"] {
		cfg.RequirePass = requirePass
	}

	logger, logCloser, err := logging.New(logging.Config{
		Dir:           cfg.LogDir,
		Level:         cfg.LogLevel,
		Format:        cfg.LogFormat,
		RetentionDays: cfg.LogRetentionDays,
	})
	if err != nil {
		slog.New(slog.NewTextHandler(os.Stderr, nil)).Error("init logger failed", "error", err)
		os.Exit(1)
	}
	defer logCloser.Close()

	logger.Info("starting xrockscache",
		"version", version,
		"config", configPath,
		"bind", cfg.Bind,
		"port", cfg.Port,
		"dir", cfg.Dir,
		"log_dir", cfg.LogDir,
		"log_level", cfg.LogLevel,
		"log_format", cfg.LogFormat,
		"log_retention_days", cfg.LogRetentionDays,
		"active_expire_enabled", cfg.ActiveExpireEnabled,
		"active_expire_bucket_seconds", cfg.ActiveExpireBucketSeconds,
		"active_expire_interval_seconds", cfg.ActiveExpireIntervalSeconds,
		"active_expire_cycle_budget_ms", cfg.ActiveExpireCycleBudgetMilliseconds,
		"active_expire_max_deletes_per_cycle", cfg.ActiveExpireMaxDeletesPerCycle,
	)

	rocksTuning := store.BuildRocksDBTuning(store.DetectResourceProfile(cfg.Dir))
	logger.Info("rocksdb tuning calculated",
		"auto_tuned", rocksTuning.AutoTuned,
		"compression", rocksTuning.Compression,
		"disk_budget_bytes", rocksTuning.DiskBudgetBytes,
		"memory_budget_bytes", rocksTuning.MemoryBudgetBytes,
		"block_cache_bytes", rocksTuning.BlockCacheSizeBytes,
		"write_buffer_bytes", rocksTuning.WriteBufferSizeBytes,
		"target_file_size_bytes", rocksTuning.TargetFileSizeBaseBytes,
		"blob_files_enabled", rocksTuning.EnableBlobFiles,
		"min_blob_size_bytes", rocksTuning.MinBlobSizeBytes,
		"blob_file_size_bytes", rocksTuning.BlobFileSizeBytes,
		"blob_gc_enabled", rocksTuning.EnableBlobGarbageCollection,
		"max_background_jobs", rocksTuning.MaxBackgroundJobs,
		"max_subcompactions", rocksTuning.MaxSubcompactions,
		"soft_pending_compaction_bytes", rocksTuning.SoftPendingCompactionBytesLimit,
		"hard_pending_compaction_bytes", rocksTuning.HardPendingCompactionBytesLimit,
		"rate_limiter_bytes_per_sec", rocksTuning.RateLimiterBytesPerSec,
		"periodic_compaction_seconds", rocksTuning.PeriodicCompactionSeconds,
		"disk_warn_watermark_bytes", rocksTuning.DiskWarnWatermarkBytes,
		"disk_slowdown_watermark_bytes", rocksTuning.DiskSlowdownWatermarkBytes,
		"disk_reject_watermark_bytes", rocksTuning.DiskRejectWatermarkBytes,
	)

	kv, err := store.OpenWithOptions(cfg.Dir, store.Options{
		Expiration: store.ExpirationConfig{
			Enabled:            cfg.ActiveExpireEnabled,
			BucketSize:         time.Duration(cfg.ActiveExpireBucketSeconds) * time.Second,
			Interval:           time.Duration(cfg.ActiveExpireIntervalSeconds) * time.Second,
			CycleBudget:        time.Duration(cfg.ActiveExpireCycleBudgetMilliseconds) * time.Millisecond,
			MaxDeletesPerCycle: cfg.ActiveExpireMaxDeletesPerCycle,
		},
		Tuning: rocksTuning,
		Logger: logger,
	})
	if err != nil {
		logger.Error("open store failed", "error", err, "dir", cfg.Dir)
		os.Exit(1)
	}
	defer kv.Close()

	srv := server.New(cfg, kv, version, logger)
	if err := srv.ListenAndServe(); err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
