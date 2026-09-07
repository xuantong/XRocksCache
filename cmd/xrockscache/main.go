package main

import (
	"flag"
	"log/slog"
	"os"

	"github.com/xuantong/XRocksCache/internal/config"
	"github.com/xuantong/XRocksCache/internal/logging"
	"github.com/xuantong/XRocksCache/internal/server"
	"github.com/xuantong/XRocksCache/internal/store"
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
		Dir:    cfg.LogDir,
		Level:  cfg.LogLevel,
		Format: cfg.LogFormat,
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
	)

	kv, err := store.Open(cfg.Dir)
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
