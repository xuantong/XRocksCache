package logging_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xrockscache/internal/logging"
)

func TestFileLoggerSizeRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "xrockscache-"+time.Now().Format(time.DateOnly)+".log")
	// 预置稀疏日志到轮转边界，避免测试重复输出大量文本。
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(16 * 1024 * 1024); err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	logger, closer, err := logging.New(logging.Config{Dir: dir, RetentionDays: 2})
	if err != nil {
		t.Fatal(err)
	}
	logger.Info("rotated")
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
	backup := strings.TrimSuffix(path, ".log") + ".1.log"
	info, err := os.Stat(backup)
	if err != nil || info.Size() != 16*1024*1024 {
		t.Fatalf("backup: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "rotated") {
		t.Fatalf("new log: %v", err)
	}
}

func TestNewFileLogger(t *testing.T) {
	dir := t.TempDir()
	logger, closer, err := logging.New(logging.Config{Dir: dir, Level: "debug", Format: "json"})
	if err != nil {
		t.Fatal(err)
	}
	logger.Info("hello", "k", "v")
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "xrockscache-"+time.Now().Format(time.DateOnly)+".log"))
	if err != nil {
		t.Fatal(err)
	}
	if len(content) == 0 {
		t.Fatal("expected log file content")
	}
}

func TestFileLoggerCleansOldFiles(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "xrockscache-2000-01-01.log")
	if err := os.WriteFile(oldPath, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	logger, closer, err := logging.New(logging.Config{Dir: dir, RetentionDays: 1})
	if err != nil {
		t.Fatal(err)
	}
	logger.Info("new")
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("expected old log to be cleaned, err=%v", err)
	}
}
