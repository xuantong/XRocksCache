package logging

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Dir    string
	Level  string
	Format string
}

type nopCloser struct{}

func (nopCloser) Close() error { return nil }

func New(cfg Config) (*slog.Logger, io.Closer, error) {
	level := parseLevel(cfg.Level)
	writer, closer, err := openWriter(cfg.Dir)
	if err != nil {
		return nil, nil, err
	}

	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	switch strings.ToLower(cfg.Format) {
	case "json":
		handler = slog.NewJSONHandler(writer, opts)
	default:
		handler = slog.NewTextHandler(writer, opts)
	}

	logger := slog.New(handler).With("component", "xrockscache")
	return logger, closer, nil
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func openWriter(dir string) (io.Writer, io.Closer, error) {
	switch strings.ToLower(strings.TrimSpace(dir)) {
	case "", "stdout":
		return os.Stdout, nopCloser{}, nil
	case "stderr":
		return os.Stderr, nopCloser{}, nil
	default:
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, nil, err
		}
		file, err := os.OpenFile(filepath.Join(dir, "xrockscache.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, nil, err
		}
		return file, file, nil
	}
}
