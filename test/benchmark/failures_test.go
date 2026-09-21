package benchmark_test

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadDoesNotCountReconnectAsSuccess(t *testing.T) {
	server := newFakeRESPServer(t)
	defer server.close()
	server.mu.Lock()
	server.rejectWrites = true
	server.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "run", "./xrcbench", "load", "--addr", server.addr(), "--keys", "1", "--clients", "1", "--pipeline", "1", "--progress-interval", "0s")
	cmd.Dir = filepath.Join("..", "..", "benchmark")
	output, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "rejected") {
		t.Fatalf("failed batch reported success: %v %s", err, output)
	}
	if strings.Contains(string(output), "panic") {
		t.Fatalf("retry panic: %s", output)
	}
}

func TestOverloadedRunReportsActualThroughput(t *testing.T) {
	server := newFakeRESPServer(t)
	defer server.close()
	server.mu.Lock()
	server.delay = 50 * time.Millisecond
	server.mu.Unlock()
	path := filepath.Join(t.TempDir(), "run.json")
	runXRCBench(t, "run", "--addr", server.addr(), "--keys", "1", "--clients", "1", "--warmup", "0s", "--duration", "100ms", "--report-interval", "50ms", "--target-qps", "1000", "--read-ratio", "100", "--output", path)
	var result struct {
		QPS      float64 `json:"qps"`
		Elapsed  float64 `json:"elapsed_seconds"`
		Attempts uint64  `json:"attempts"`
	}
	readJSONFile(t, path, &result)
	if result.QPS > 30 || result.Attempts > 3 || result.Elapsed < 0.1 {
		t.Fatalf("overload inflated throughput: %+v", result)
	}
}
