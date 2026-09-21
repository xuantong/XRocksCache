package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
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
	size          int64
}

// 单日日志同样有界，避免异常请求或底层故障在轮转日期之前打满磁盘。
const maxLogFileBytes int64 = 16 * 1024 * 1024
const logBackups = 3

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
	if int64(len(p)) > maxLogFileBytes {
		return 0, fmt.Errorf("log record exceeds size limit")
	}
	if w.size+int64(len(p)) > maxLogFileBytes {
		if err := w.rotateSizeLocked(); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

// rotateSizeLocked 仅操作当前日期下的固定备份文件，不删除其他用户文件。
func (w *dailyRotateWriter) rotateSizeLocked() error {
	if err := w.file.Close(); err != nil {
		return err
	}
	w.file = nil
	base := filepath.Join(w.dir, "xrockscache-"+w.currentDate)
	oldest := fmt.Sprintf("%s.%d.log", base, logBackups)
	if err := os.Remove(oldest); err != nil && !os.IsNotExist(err) {
		return err
	}
	for i := logBackups - 1; i >= 1; i-- {
		if err := os.Rename(fmt.Sprintf("%s.%d.log", base, i), fmt.Sprintf("%s.%d.log", base, i+1)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := os.Rename(base+".log", base+".1.log"); err != nil {
		return err
	}
	file, err := os.OpenFile(base+".log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	w.file, w.size = file, 0
	return nil
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
	if info, err := file.Stat(); err == nil {
		w.size = info.Size()
	} else {
		_ = file.Close()
		w.file = nil
		return err
	}
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
	if len(dateText) > 10 {
		if dateText[10] != '.' {
			return time.Time{}, false
		}
		backup, err := strconv.Atoi(dateText[11:])
		if err != nil || backup < 1 || backup > logBackups {
			return time.Time{}, false
		}
		dateText = dateText[:10]
	}
	date, err := time.ParseInLocation(time.DateOnly, dateText, time.Local)
	return date, err == nil
}
