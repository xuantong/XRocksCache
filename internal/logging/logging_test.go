package logging

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewFileLogger(t *testing.T) {
	dir := t.TempDir()
	logger, closer, err := New(Config{Dir: dir, Level: "debug", Format: "json"})
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

func TestDailyRotateWriterRotatesAndCleansOldFiles(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 7, 10, 0, 0, 0, time.Local)
	writer, err := newDailyRotateWriter(dir, 1, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("day1\n")); err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(dir, "xrockscache-2026-09-06.log")
	if err := os.WriteFile(oldPath, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	now = now.AddDate(0, 0, 2)
	if _, err := writer.Write([]byte("day3\n")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(dir, "xrockscache-2026-09-07.log")); !os.IsNotExist(err) {
		t.Fatalf("expected first day log to be cleaned, err=%v", err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("expected old log to be cleaned, err=%v", err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "xrockscache-2026-09-09.log"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "day3\n" {
		t.Fatalf("rotated log content = %q", content)
	}
}
