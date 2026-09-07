package logging

import (
	"os"
	"path/filepath"
	"testing"
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
	content, err := os.ReadFile(filepath.Join(dir, "xrockscache.log"))
	if err != nil {
		t.Fatal(err)
	}
	if len(content) == 0 {
		t.Fatal("expected log file content")
	}
}
