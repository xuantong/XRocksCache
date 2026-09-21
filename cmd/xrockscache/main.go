package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"xrockscache/internal/config"
	"xrockscache/internal/logging"
	"xrockscache/internal/server"
	"xrockscache/internal/store"
)

var version = "0.2.1-go"

func main() {
	if err := run(); err != nil {
		slog.Error("xrockscache stopped", "error", err)
		os.Exit(1)
	}
}

// run 将退出码处理放到外层，确保错误返回和信号退出都执行资源清理。
func run() error {
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
		return nil
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		slog.New(slog.NewTextHandler(os.Stderr, nil)).Error("load config failed", "error", err)
		return err
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
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return fmt.Errorf("invalid port")
	}

	logger, logCloser, err := logging.New(logging.Config{
		Dir:           cfg.LogDir,
		Level:         cfg.LogLevel,
		Format:        cfg.LogFormat,
		RetentionDays: cfg.LogRetentionDays,
	})
	if err != nil {
		slog.New(slog.NewTextHandler(os.Stderr, nil)).Error("init logger failed", "error", err)
		return err
	}
	defer logCloser.Close()

	logger.Info("starting xrockscache",
		"disk_type", cfg.DiskType, "disk_pl", cfg.DiskPL, "disk_capacity_gib", cfg.DiskCapacityGiB,
		"version", version,
		"config", configPath,
		"bind", cfg.Bind,
		"port", cfg.Port,
		"dir", cfg.Dir,
		"log_dir", cfg.LogDir,
		"log_level", cfg.LogLevel,
		"log_format", cfg.LogFormat,
		"log_retention_days", cfg.LogRetentionDays,
		"expiration_strategy", "rocksdb_compaction_filter",
	)
	if cfg.ActiveExpireEnabled {
		logger.Warn("legacy active expiration settings are ignored; RocksDB compaction controls physical reclamation")
	}

	profile := store.DetectResourceProfile(cfg.Dir)
	// 此限制只覆盖 Go 堆，RocksDB 的 C/C++ 内存另由引擎预算管理。
	if profile.MemoryBytes > 0 && os.Getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(int64(profile.MemoryBytes / 5))
	}
	rocksTuning := store.BuildRocksDBTuning(profile)
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
		WriteRateMiB: cfg.WriteRateMiB,
		Tuning:       rocksTuning,
		Logger:       logger,
	})
	if err != nil {
		logger.Error("open store failed", "error", err, "dir", cfg.Dir)
		return err
	}
	defer kv.Close()

	srv := server.New(cfg, kv, version, logger)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe() }()
	var serveErr error
	select {
	case <-ctx.Done():
	case serveErr = <-done:
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown timed out", "error", err)
		return err
	}
	logger.Info("server stopped")
	return serveErr
}
