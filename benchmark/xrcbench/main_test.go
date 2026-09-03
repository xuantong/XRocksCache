package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseBytes(t *testing.T) {
	tests := map[string]int64{
		"100B":   100,
		"1KB":    1000,
		"1KiB":   1024,
		"1.5MiB": 1572864,
		"2GB":    2000000000,
		"2GiB":   2147483648,
	}
	for input, expected := range tests {
		actual, err := parseBytes(input)
		if err != nil {
			t.Fatalf("parseBytes(%q): %v", input, err)
		}
		if actual != expected {
			t.Fatalf("parseBytes(%q) = %d, want %d", input, actual, expected)
		}
	}
}

func TestLatencyHistogram(t *testing.T) {
	var histogram latencyHistogram
	for i := 1; i <= 1000; i++ {
		histogram.record(time.Duration(i) * time.Microsecond)
	}
	if histogram.count != 1000 {
		t.Fatalf("count = %d, want 1000", histogram.count)
	}
	assertApproxDuration(t, histogram.percentile(0.50), 500*time.Microsecond, 15*time.Microsecond)
	assertApproxDuration(t, histogram.percentile(0.99), 990*time.Microsecond, 20*time.Microsecond)
	if histogram.maxNS != uint64(time.Millisecond) {
		t.Fatalf("max = %s, want 1ms", time.Duration(histogram.maxNS))
	}
}

func TestMakeKey(t *testing.T) {
	if actual := string(makeKey("xrc:", 6, 42)); actual != "xrc:000042" {
		t.Fatalf("makeKey() = %q", actual)
	}
}

func TestLoadAndRunAgainstRESPServer(t *testing.T) {
	server := newFakeRESPServer(t)
	defer server.close()

	loadOutput := t.TempDir() + "/load.json"
	err := runLoad([]string{
		"--addr", server.addr(),
		"--start-key", "50",
		"--keys", "100",
		"--value-size", "100B",
		"--clients", "2",
		"--pipeline", "8",
		"--progress-interval", "0s",
		"--output", loadOutput,
	})
	if err != nil {
		t.Fatalf("runLoad(): %v", err)
	}
	if server.keyCount() != 100 {
		t.Fatalf("loaded keys = %d, want 100", server.keyCount())
	}
	server.mu.Lock()
	_, hasFirstKey := server.values["xrc:000000000050"]
	server.mu.Unlock()
	if !hasFirstKey {
		t.Fatal("load did not honor --start-key")
	}
	var loaded loadResult
	readJSONFile(t, loadOutput, &loaded)
	if loaded.LoadedKeys != 100 || loaded.Errors != 0 {
		t.Fatalf("unexpected load result: %+v", loaded)
	}

	runOutput := t.TempDir() + "/run.json"
	err = runWorkload([]string{
		"--addr", server.addr(),
		"--start-key", "50",
		"--keys", "100",
		"--value-size", "100B",
		"--clients", "2",
		"--warmup", "20ms",
		"--duration", "200ms",
		"--report-interval", "50ms",
		"--read-ratio", "95",
		"--distribution", "zipfian",
		"--target-qps", "200",
		"--output", runOutput,
	})
	if err != nil {
		t.Fatalf("runWorkload(): %v", err)
	}
	var result runResult
	readJSONFile(t, runOutput, &result)
	if result.Attempts == 0 || result.SuccessfulOperations == 0 {
		t.Fatalf("workload did not execute operations: %+v", result)
	}
	if result.Errors != 0 {
		t.Fatalf("workload errors = %d, want 0", result.Errors)
	}
	if result.GETMisses != 0 || result.GETHitRatio != 1 {
		t.Fatalf("unexpected misses: misses=%d hit_ratio=%f", result.GETMisses, result.GETHitRatio)
	}
	if result.GET.Count == 0 || len(result.Intervals) == 0 {
		t.Fatalf("missing GET latency or interval data: %+v", result)
	}
}

type fakeRESPServer struct {
	t        *testing.T
	listener net.Listener
	mu       sync.Mutex
	values   map[string][]byte
	conns    map[net.Conn]struct{}
	wg       sync.WaitGroup
}

func newFakeRESPServer(t *testing.T) *fakeRESPServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen(): %v", err)
	}
	server := &fakeRESPServer{
		t:        t,
		listener: listener,
		values:   map[string][]byte{},
		conns:    map[net.Conn]struct{}{},
	}
	server.wg.Add(1)
	go server.accept()
	return server
}

func (s *fakeRESPServer) addr() string {
	return s.listener.Addr().String()
}

func (s *fakeRESPServer) keyCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.values)
}

func (s *fakeRESPServer) accept() {
	defer s.wg.Done()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.conns[conn] = struct{}{}
		s.mu.Unlock()
		s.wg.Add(1)
		go s.serve(conn)
	}
}

func (s *fakeRESPServer) serve(conn net.Conn) {
	defer s.wg.Done()
	defer func() {
		_ = conn.Close()
		s.mu.Lock()
		delete(s.conns, conn)
		s.mu.Unlock()
	}()
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	for {
		args, err := readTestCommand(reader)
		if err != nil {
			return
		}
		if len(args) == 0 {
			return
		}
		switch strings.ToUpper(string(args[0])) {
		case "AUTH", "PING":
			_, err = writer.WriteString("+OK\r\n")
		case "SET":
			if len(args) < 3 {
				_, err = writer.WriteString("-ERR wrong number of arguments\r\n")
				break
			}
			s.mu.Lock()
			s.values[string(args[1])] = append([]byte(nil), args[2]...)
			s.mu.Unlock()
			_, err = writer.WriteString("+OK\r\n")
		case "GET":
			s.mu.Lock()
			value, found := s.values[string(args[1])]
			s.mu.Unlock()
			if !found {
				_, err = writer.WriteString("$-1\r\n")
			} else {
				_, err = fmt.Fprintf(writer, "$%d\r\n%s\r\n", len(value), value)
			}
		default:
			_, err = writer.WriteString("-ERR unsupported test command\r\n")
		}
		if err != nil || writer.Flush() != nil {
			return
		}
	}
}

func (s *fakeRESPServer) close() {
	_ = s.listener.Close()
	s.mu.Lock()
	for conn := range s.conns {
		_ = conn.Close()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

func readTestCommand(reader *bufio.Reader) ([][]byte, error) {
	header, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	if len(header) < 4 || header[0] != '*' {
		return nil, fmt.Errorf("invalid command header %q", header)
	}
	count, err := strconv.Atoi(strings.TrimSpace(header[1:]))
	if err != nil {
		return nil, err
	}
	args := make([][]byte, 0, count)
	for range count {
		bulkHeader, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		if len(bulkHeader) < 4 || bulkHeader[0] != '$' {
			return nil, fmt.Errorf("invalid bulk header %q", bulkHeader)
		}
		length, err := strconv.Atoi(strings.TrimSpace(bulkHeader[1:]))
		if err != nil {
			return nil, err
		}
		value := make([]byte, length)
		if _, err := io.ReadFull(reader, value); err != nil {
			return nil, err
		}
		trailer := make([]byte, 2)
		if _, err := io.ReadFull(reader, trailer); err != nil {
			return nil, err
		}
		if string(trailer) != "\r\n" {
			return nil, errors.New("invalid bulk trailer")
		}
		args = append(args, value)
	}
	return args, nil
}

func readJSONFile(t *testing.T, path string, target any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile(%q): %v", path, err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("json.Unmarshal(%q): %v", path, err)
	}
}

func assertApproxDuration(t *testing.T, actual, expected, tolerance time.Duration) {
	t.Helper()
	if actual < expected-tolerance || actual > expected+tolerance {
		t.Fatalf("duration = %s, want %s +/- %s", actual, expected, tolerance)
	}
}
