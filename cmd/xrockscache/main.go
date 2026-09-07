package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/xuantong/XRocksCache/internal/config"
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
		fmt.Printf("xrockscache %s\n", version)
		return
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
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

	kv, err := store.Open(cfg.Dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open store: %v\n", err)
		os.Exit(1)
	}
	defer kv.Close()

	srv := server.New(cfg, kv, version)
	if err := srv.ListenAndServe(); err != nil {
		fmt.Fprintf(os.Stderr, "server stopped: %v\n", err)
		os.Exit(1)
	}
}
