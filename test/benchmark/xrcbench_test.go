package benchmark_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestXRCBenchLoadAndRunAgainstRESPServer(t *testing.T) {
	server := newFakeRESPServer(t)
	defer server.close()

	loadOutput := filepath.Join(t.TempDir(), "load.json")
	runXRCBench(t,
		"load",
		"--addr", server.addr(),
		"--start-key", "50",
		"--keys", "100",
		"--value-size", "100B",
		"--clients", "2",
		"--pipeline", "8",
		"--progress-interval", "0s",
		"--output", loadOutput,
	)
	if server.keyCount() != 100 {
		t.Fatalf("loaded keys = %d, want 100", server.keyCount())
	}
	if !server.hasKey("xrc:000000000050") {
		t.Fatal("load did not honor --start-key")
	}

	var loaded struct {
		LoadedKeys int64 `json:"loaded_keys"`
		Errors     int64 `json:"errors"`
	}
	readJSONFile(t, loadOutput, &loaded)
	if loaded.LoadedKeys != 100 || loaded.Errors != 0 {
		t.Fatalf("unexpected load result: %+v", loaded)
	}

	runOutput := filepath.Join(t.TempDir(), "run.json")
	runXRCBench(t,
		"run",
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
	)

	var result struct {
		Attempts             uint64  `json:"attempts"`
		SuccessfulOperations uint64  `json:"successful_operations"`
		Errors               uint64  `json:"errors"`
		GETMisses            uint64  `json:"get_misses"`
		GETHitRatio          float64 `json:"get_hit_ratio"`
		GET                  struct {
			Count uint64 `json:"count"`
		} `json:"get"`
		Intervals []struct{} `json:"intervals"`
	}
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

func runXRCBench(t *testing.T, args ...string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	commandArgs := append([]string{"run", "./xrcbench"}, args...)
	cmd := exec.CommandContext(ctx, "go", commandArgs...)
	cmd.Dir = filepath.Join("..", "..", "benchmark")
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("xrcbench timed out: %s", output)
	}
	if err != nil {
		t.Fatalf("xrcbench failed: %v\n%s", err, output)
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

func (s *fakeRESPServer) hasKey(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.values[key]
	return ok
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
	first, err := reader.ReadByte()
	if err != nil {
		return nil, err
	}
	if first != '*' {
		return nil, errors.New("expected array")
	}
	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	count, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || count < 0 {
		return nil, errors.New("invalid array length")
	}

	args := make([][]byte, 0, count)
	for i := 0; i < count; i++ {
		prefix, err := reader.ReadByte()
		if err != nil {
			return nil, err
		}
		if prefix != '$' {
			return nil, errors.New("expected bulk string")
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		size, err := strconv.Atoi(strings.TrimSpace(line))
		if err != nil || size < 0 {
			return nil, errors.New("invalid bulk string size")
		}
		buf := make([]byte, size+2)
		if _, err := io.ReadFull(reader, buf); err != nil {
			return nil, err
		}
		args = append(args, append([]byte(nil), buf[:size]...))
	}
	return args, nil
}

func readJSONFile(t *testing.T, path string, out any) {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := json.NewDecoder(file).Decode(out); err != nil {
		t.Fatal(err)
	}
}
