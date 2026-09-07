package logging

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Dir           string
	Level         string
	Format        string
	RetentionDays int
}

type nopCloser struct{}

func (nopCloser) Close() error { return nil }

func New(cfg Config) (*slog.Logger, io.Closer, error) {
	level := parseLevel(cfg.Level)
	writer, closer, err := openWriter(cfg)
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

func openWriter(cfg Config) (io.Writer, io.Closer, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Dir)) {
	case "", "stdout":
		return os.Stdout, nopCloser{}, nil
	case "stderr":
		return os.Stderr, nopCloser{}, nil
	default:
		writer, err := newDailyRotateWriter(cfg.Dir, cfg.RetentionDays, time.Now)
		if err != nil {
			return nil, nil, err
		}
		return writer, writer, nil
	}
}

type dailyRotateWriter struct {
	mu            sync.Mutex
	dir           string
	retentionDays int
	now           func() time.Time
	currentDate   string
	file          *os.File
}

func newDailyRotateWriter(dir string, retentionDays int, now func() time.Time) (*dailyRotateWriter, error) {
	if now == nil {
		now = time.Now
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	writer := &dailyRotateWriter{
		dir:           dir,
		retentionDays: retentionDays,
		now:           now,
	}
	if err := writer.rotateLocked(now()); err != nil {
		return nil, err
	}
	return writer, nil
}

func (w *dailyRotateWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.rotateLocked(w.now()); err != nil {
		return 0, err
	}
	return w.file.Write(p)
}

func (w *dailyRotateWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

func (w *dailyRotateWriter) rotateLocked(now time.Time) error {
	date := now.Format(time.DateOnly)
	if w.file != nil && w.currentDate == date {
		return nil
	}
	if w.file != nil {
		if err := w.file.Close(); err != nil {
			return err
		}
		w.file = nil
	}
	file, err := os.OpenFile(filepath.Join(w.dir, "xrockscache-"+date+".log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	w.file = file
	w.currentDate = date
	return w.cleanupLocked(now)
}

func (w *dailyRotateWriter) cleanupLocked(now time.Time) error {
	if w.retentionDays <= 0 {
		return nil
	}
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return err
	}
	cutoff := now.AddDate(0, 0, -w.retentionDays)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		date, ok := parseLogDate(entry.Name())
		if !ok || !date.Before(cutoff) {
			continue
		}
		if err := os.Remove(filepath.Join(w.dir, entry.Name())); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func parseLogDate(name string) (time.Time, bool) {
	if !strings.HasPrefix(name, "xrockscache-") || !strings.HasSuffix(name, ".log") {
		return time.Time{}, false
	}
	dateText := strings.TrimSuffix(strings.TrimPrefix(name, "xrockscache-"), ".log")
	date, err := time.ParseInLocation(time.DateOnly, dateText, time.Local)
	return date, err == nil
}
