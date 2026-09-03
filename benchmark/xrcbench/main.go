package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"math/bits"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	toolVersion      = "0.1.0"
	histogramBuckets = 64 * 64
)

type commonOptions struct {
	addr         string
	password     string
	prefix       string
	keyWidth     int
	startKey     int64
	keys         int64
	datasetSize  string
	valueSize    string
	valuePattern string
	valuePool    int
	seed         int64
	clients      int
	timeout      time.Duration
	ttl          time.Duration
	output       string
}

type resultConfig struct {
	Addr             string  `json:"addr"`
	Prefix           string  `json:"prefix"`
	KeyWidth         int     `json:"key_width"`
	StartKey         int64   `json:"start_key"`
	Keys             int64   `json:"keys"`
	LogicalBytes     int64   `json:"logical_bytes"`
	ValueBytes       int     `json:"value_bytes"`
	ValuePattern     string  `json:"value_pattern"`
	ValuePool        int     `json:"value_pool"`
	Seed             int64   `json:"seed"`
	Clients          int     `json:"clients"`
	TimeoutMS        int64   `json:"timeout_ms"`
	TTLMS            int64   `json:"ttl_ms"`
	ReadRatio        float64 `json:"read_ratio,omitempty"`
	Distribution     string  `json:"distribution,omitempty"`
	ZipfS            float64 `json:"zipf_s,omitempty"`
	ZipfV            float64 `json:"zipf_v,omitempty"`
	TargetQPS        float64 `json:"target_qps,omitempty"`
	DurationSeconds  float64 `json:"duration_seconds,omitempty"`
	WarmupSeconds    float64 `json:"warmup_seconds,omitempty"`
	Pipeline         int     `json:"pipeline,omitempty"`
	ReportIntervalMS int64   `json:"report_interval_ms,omitempty"`
}

type loadResult struct {
	Tool        string       `json:"tool"`
	Version     string       `json:"version"`
	Mode        string       `json:"mode"`
	StartedAt   time.Time    `json:"started_at"`
	CompletedAt time.Time    `json:"completed_at"`
	ElapsedMS   int64        `json:"elapsed_ms"`
	Config      resultConfig `json:"config"`
	LoadedKeys  int64        `json:"loaded_keys"`
	QPS         float64      `json:"qps"`
	Errors      int64        `json:"errors"`
}

type latencySummary struct {
	Count  uint64  `json:"count"`
	MeanMS float64 `json:"mean_ms"`
	P50MS  float64 `json:"p50_ms"`
	P95MS  float64 `json:"p95_ms"`
	P99MS  float64 `json:"p99_ms"`
	P999MS float64 `json:"p999_ms"`
	MaxMS  float64 `json:"max_ms"`
}

type intervalSummary struct {
	Index         int            `json:"index"`
	StartMS       int64          `json:"start_ms"`
	Duration      int64          `json:"duration_ms"`
	GET           latencySummary `json:"get"`
	SET           latencySummary `json:"set"`
	Attempts      uint64         `json:"attempts"`
	Errors        uint64         `json:"errors"`
	GETMisses     uint64         `json:"get_misses"`
	GETHitRatio   float64        `json:"get_hit_ratio"`
	AttemptedQPS  float64        `json:"attempted_qps"`
	SuccessfulQPS float64        `json:"qps"`
	ReadRatio     float64        `json:"actual_read_ratio"`
}

type runResult struct {
	Tool                 string            `json:"tool"`
	Version              string            `json:"version"`
	Mode                 string            `json:"mode"`
	StartedAt            time.Time         `json:"started_at"`
	CompletedAt          time.Time         `json:"completed_at"`
	Config               resultConfig      `json:"config"`
	Attempts             uint64            `json:"attempts"`
	SuccessfulOperations uint64            `json:"successful_operations"`
	Errors               uint64            `json:"errors"`
	GETMisses            uint64            `json:"get_misses"`
	GETHitRatio          float64           `json:"get_hit_ratio"`
	AttemptedQPS         float64           `json:"attempted_qps"`
	QPS                  float64           `json:"qps"`
	GET                  latencySummary    `json:"get"`
	SET                  latencySummary    `json:"set"`
	Intervals            []intervalSummary `json:"intervals"`
}

type latencyHistogram struct {
	buckets [histogramBuckets]uint64
	count   uint64
	sumNS   uint64
	maxNS   uint64
}

func (h *latencyHistogram) record(d time.Duration) {
	ns := uint64(max(d.Nanoseconds(), 64))
	exponent := bits.Len64(ns) - 1
	base := uint64(1) << exponent
	subBucket := int((ns - base) * 64 / base)
	if subBucket > 63 {
		subBucket = 63
	}
	index := exponent*64 + subBucket
	if index >= histogramBuckets {
		index = histogramBuckets - 1
	}
	h.buckets[index]++
	h.count++
	h.sumNS += ns
	if ns > h.maxNS {
		h.maxNS = ns
	}
}

func (h *latencyHistogram) merge(other *latencyHistogram) {
	for i, count := range other.buckets {
		h.buckets[i] += count
	}
	h.count += other.count
	h.sumNS += other.sumNS
	if other.maxNS > h.maxNS {
		h.maxNS = other.maxNS
	}
}

func (h *latencyHistogram) percentile(p float64) time.Duration {
	if h.count == 0 {
		return 0
	}
	target := uint64(math.Ceil(float64(h.count) * p))
	var cumulative uint64
	for index, count := range h.buckets {
		cumulative += count
		if cumulative >= target {
			exponent := index / 64
			subBucket := index % 64
			base := uint64(1) << exponent
			width := max(base/64, 1)
			return time.Duration(base + uint64(subBucket+1)*width)
		}
	}
	return time.Duration(h.maxNS)
}

func (h *latencyHistogram) summary() latencySummary {
	if h.count == 0 {
		return latencySummary{}
	}
	return latencySummary{
		Count:  h.count,
		MeanMS: float64(h.sumNS) / float64(h.count) / float64(time.Millisecond),
		P50MS:  durationMS(h.percentile(0.50)),
		P95MS:  durationMS(h.percentile(0.95)),
		P99MS:  durationMS(h.percentile(0.99)),
		P999MS: durationMS(h.percentile(0.999)),
		MaxMS:  float64(h.maxNS) / float64(time.Millisecond),
	}
}

type respClient struct {
	conn    net.Conn
	reader  *bufio.Reader
	writer  *bufio.Writer
	timeout time.Duration
}

func dialRESP(addr, password string, timeout time.Duration) (*respClient, error) {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, err
	}
	client := &respClient{
		conn:    conn,
		reader:  bufio.NewReaderSize(conn, 64*1024),
		writer:  bufio.NewWriterSize(conn, 64*1024),
		timeout: timeout,
	}
	if password != "" {
		if _, err := client.command([]byte("AUTH"), []byte(password)); err != nil {
			client.close()
			return nil, fmt.Errorf("AUTH failed: %w", err)
		}
	}
	return client, nil
}

func (c *respClient) close() {
	if c != nil && c.conn != nil {
		_ = c.conn.Close()
	}
}

func (c *respClient) command(args ...[]byte) (bool, error) {
	if err := c.setDeadline(); err != nil {
		return false, err
	}
	if err := writeRESPCommand(c.writer, args...); err != nil {
		return false, err
	}
	if err := c.writer.Flush(); err != nil {
		return false, err
	}
	return readRESPReply(c.reader)
}

func (c *respClient) pipelineSET(keys, values [][]byte, ttl time.Duration) error {
	if err := c.setDeadline(); err != nil {
		return err
	}
	for i := range keys {
		args := [][]byte{[]byte("SET"), keys[i], values[i]}
		if ttl > 0 {
			args = append(args, []byte("PX"), []byte(strconv.FormatInt(ttl.Milliseconds(), 10)))
		}
		if err := writeRESPCommand(c.writer, args...); err != nil {
			return err
		}
	}
	if err := c.writer.Flush(); err != nil {
		return err
	}
	for range keys {
		if _, err := readRESPReply(c.reader); err != nil {
			return err
		}
	}
	return nil
}

func (c *respClient) setDeadline() error {
	return c.conn.SetDeadline(time.Now().Add(c.timeout))
}

func writeRESPCommand(w *bufio.Writer, args ...[]byte) error {
	if _, err := fmt.Fprintf(w, "*%d\r\n", len(args)); err != nil {
		return err
	}
	for _, arg := range args {
		if _, err := fmt.Fprintf(w, "$%d\r\n", len(arg)); err != nil {
			return err
		}
		if _, err := w.Write(arg); err != nil {
			return err
		}
		if _, err := w.WriteString("\r\n"); err != nil {
			return err
		}
	}
	return nil
}

func readRESPReply(r *bufio.Reader) (bool, error) {
	prefix, err := r.ReadByte()
	if err != nil {
		return false, err
	}
	line, err := readRESPLine(r)
	if err != nil {
		return false, err
	}
	switch prefix {
	case '+', ':':
		return false, nil
	case '-':
		return false, errors.New(string(line))
	case '$':
		length, err := strconv.ParseInt(string(line), 10, 64)
		if err != nil {
			return false, err
		}
		if length < 0 {
			return true, nil
		}
		_, err = io.CopyN(io.Discard, r, length+2)
		return false, err
	default:
		return false, fmt.Errorf("unsupported RESP reply prefix %q", prefix)
	}
}

func readRESPLine(r *bufio.Reader) ([]byte, error) {
	line, err := r.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	if len(line) < 2 || line[len(line)-2] != '\r' {
		return nil, errors.New("malformed RESP line")
	}
	return line[:len(line)-2], nil
}

func addCommonFlags(fs *flag.FlagSet, options *commonOptions) {
	fs.StringVar(&options.addr, "addr", "127.0.0.1:6379", "Redis-compatible server address")
	fs.StringVar(&options.password, "password", os.Getenv("XRC_PASSWORD"), "Password (prefer XRC_PASSWORD)")
	fs.StringVar(&options.prefix, "prefix", "xrc:", "Key prefix")
	fs.IntVar(&options.keyWidth, "key-width", 12, "Zero-padded numeric key width")
	fs.Int64Var(&options.startKey, "start-key", 0, "First numeric key index")
	fs.Int64Var(&options.keys, "keys", 0, "Number of preloaded keys")
	fs.StringVar(&options.datasetSize, "dataset-size", "", "Logical value bytes, for example 10GiB")
	fs.StringVar(&options.valueSize, "value-size", "1KiB", "Value size, for example 100B or 4KiB")
	fs.StringVar(&options.valuePattern, "value-pattern", "random", "Value pattern: random or repeated")
	fs.IntVar(&options.valuePool, "value-pool", 1024, "Number of reusable values")
	fs.Int64Var(&options.seed, "seed", 1, "Deterministic random seed")
	fs.IntVar(&options.clients, "clients", 16, "Concurrent connections")
	fs.DurationVar(&options.timeout, "timeout", 10*time.Second, "Per-request timeout")
	fs.DurationVar(&options.ttl, "ttl", 0, "SET TTL; zero disables expiration")
	fs.StringVar(&options.output, "output", "", "JSON result path; stdout when empty")
}

func validateCommon(options commonOptions) (int64, int, error) {
	if options.clients <= 0 || options.keyWidth <= 0 || options.valuePool <= 0 || options.timeout <= 0 {
		return 0, 0, errors.New("clients, key-width, value-pool, and timeout must be positive")
	}
	if options.startKey < 0 {
		return 0, 0, errors.New("start-key cannot be negative")
	}
	valueBytes64, err := parseBytes(options.valueSize)
	if err != nil || valueBytes64 <= 0 || valueBytes64 > math.MaxInt {
		return 0, 0, fmt.Errorf("invalid value-size %q", options.valueSize)
	}
	keys := options.keys
	if keys == 0 {
		if options.datasetSize == "" {
			return 0, 0, errors.New("either keys or dataset-size is required")
		}
		datasetBytes, err := parseBytes(options.datasetSize)
		if err != nil || datasetBytes <= 0 {
			return 0, 0, fmt.Errorf("invalid dataset-size %q", options.datasetSize)
		}
		keys = (datasetBytes + valueBytes64 - 1) / valueBytes64
	}
	if keys <= 0 {
		return 0, 0, errors.New("keys must be positive")
	}
	if options.startKey > math.MaxInt64-keys {
		return 0, 0, errors.New("start-key plus keys overflows int64")
	}
	if options.valuePattern != "random" && options.valuePattern != "repeated" {
		return 0, 0, errors.New("value-pattern must be random or repeated")
	}
	return keys, int(valueBytes64), nil
}

func makeValuePool(valueBytes, poolSize int, pattern string, seed int64) [][]byte {
	pool := make([][]byte, poolSize)
	rng := rand.New(rand.NewSource(seed))
	for i := range pool {
		pool[i] = make([]byte, valueBytes)
		if pattern == "random" {
			_, _ = rng.Read(pool[i])
		} else {
			for j := range pool[i] {
				pool[i][j] = byte('a' + i%26)
			}
		}
	}
	return pool
}

func makeKey(prefix string, width int, index int64) []byte {
	number := strconv.FormatInt(index, 10)
	padding := max(width-len(number), 0)
	key := make([]byte, len(prefix)+padding+len(number))
	copy(key, prefix)
	for i := len(prefix); i < len(prefix)+padding; i++ {
		key[i] = '0'
	}
	copy(key[len(prefix)+padding:], number)
	return key
}

func runLoad(args []string) error {
	fs := flag.NewFlagSet("load", flag.ContinueOnError)
	var options commonOptions
	var pipeline int
	var progressInterval time.Duration
	addCommonFlags(fs, &options)
	fs.IntVar(&pipeline, "pipeline", 64, "SET commands per pipeline")
	fs.DurationVar(&progressInterval, "progress-interval", 5*time.Second, "Progress output interval")
	if err := fs.Parse(args); err != nil {
		return err
	}
	keys, valueBytes, err := validateCommon(options)
	if err != nil {
		return err
	}
	if pipeline <= 0 {
		return errors.New("pipeline must be positive")
	}

	values := makeValuePool(valueBytes, options.valuePool, options.valuePattern, options.seed)
	startedAt := time.Now()
	var nextKey atomic.Int64
	nextKey.Store(options.startKey)
	endKey := options.startKey + keys
	var loaded atomic.Int64
	var loadErrors atomic.Int64
	var abort atomic.Bool
	stopProgress := make(chan struct{})
	if progressInterval > 0 {
		go printLoadProgress(&loaded, keys, startedAt, progressInterval, stopProgress)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 1)
	for workerID := 0; workerID < options.clients; workerID++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			client, err := dialRESP(options.addr, options.password, options.timeout)
			if err != nil {
				reportFirstError(errCh, fmt.Errorf("worker %d connect: %w", workerID, err))
				return
			}
			defer client.close()

			for {
				if abort.Load() {
					return
				}
				start := nextKey.Add(int64(pipeline)) - int64(pipeline)
				if start >= endKey {
					return
				}
				end := min(start+int64(pipeline), endKey)
				batchKeys := make([][]byte, 0, end-start)
				batchValues := make([][]byte, 0, end-start)
				for keyIndex := start; keyIndex < end; keyIndex++ {
					batchKeys = append(batchKeys, makeKey(options.prefix, options.keyWidth, keyIndex))
					batchValues = append(batchValues, values[int(keyIndex%int64(len(values)))])
				}

				var batchErr error
				for attempt := 0; attempt < 3; attempt++ {
					batchErr = client.pipelineSET(batchKeys, batchValues, options.ttl)
					if batchErr == nil {
						break
					}
					client.close()
					client, batchErr = dialRESP(options.addr, options.password, options.timeout)
					if batchErr != nil {
						time.Sleep(100 * time.Millisecond)
					}
				}
				if batchErr != nil {
					loadErrors.Add(end - start)
					abort.Store(true)
					reportFirstError(errCh, fmt.Errorf("worker %d keys [%d,%d): %w", workerID, start, end, batchErr))
					return
				}
				loaded.Add(end - start)
			}
		}(workerID)
	}
	wg.Wait()
	close(stopProgress)
	completedAt := time.Now()

	select {
	case loadErr := <-errCh:
		return loadErr
	default:
	}
	elapsed := completedAt.Sub(startedAt)
	result := loadResult{
		Tool:        "xrcbench",
		Version:     toolVersion,
		Mode:        "load",
		StartedAt:   startedAt,
		CompletedAt: completedAt,
		ElapsedMS:   elapsed.Milliseconds(),
		Config:      makeResultConfig(options, keys, valueBytes),
		LoadedKeys:  loaded.Load(),
		QPS:         float64(loaded.Load()) / elapsed.Seconds(),
		Errors:      loadErrors.Load(),
	}
	result.Config.Pipeline = pipeline
	return writeJSONResult(options.output, result)
}

func printLoadProgress(loaded *atomic.Int64, total int64, startedAt time.Time, interval time.Duration, stop <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			count := loaded.Load()
			elapsed := time.Since(startedAt).Seconds()
			fmt.Fprintf(os.Stderr, "loaded=%d/%d progress=%.2f%% qps=%.0f\n", count, total,
				float64(count)*100/float64(total), float64(count)/elapsed)
		case <-stop:
			return
		}
	}
}

type runOptions struct {
	commonOptions
	duration       time.Duration
	warmup         time.Duration
	readRatio      float64
	distribution   string
	zipfS          float64
	zipfV          float64
	targetQPS      float64
	reportInterval time.Duration
}

type workerResult struct {
	getAttempts uint64
	setAttempts uint64
	getMisses   uint64
	get         latencyHistogram
	set         latencyHistogram
	errors      uint64
}

type workerInterval struct {
	index       int
	getAttempts uint64
	setAttempts uint64
	getMisses   uint64
	get         latencyHistogram
	set         latencyHistogram
	errors      uint64
}

type aggregateInterval struct {
	getAttempts uint64
	setAttempts uint64
	getMisses   uint64
	get         latencyHistogram
	set         latencyHistogram
	errors      uint64
}

type runWindow struct {
	measurementStart time.Time
	measurementEnd   time.Time
}

func runWorkload(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	var options runOptions
	addCommonFlags(fs, &options.commonOptions)
	fs.DurationVar(&options.duration, "duration", 5*time.Minute, "Measured duration")
	fs.DurationVar(&options.warmup, "warmup", 30*time.Second, "Unmeasured warmup duration")
	fs.Float64Var(&options.readRatio, "read-ratio", 95, "GET percentage from 0 to 100")
	fs.StringVar(&options.distribution, "distribution", "uniform", "uniform or zipfian")
	fs.Float64Var(&options.zipfS, "zipf-s", 1.2, "Zipf exponent, greater than 1")
	fs.Float64Var(&options.zipfV, "zipf-v", 1, "Zipf rank shift, at least 1")
	fs.Float64Var(&options.targetQPS, "target-qps", 0, "Open-loop target QPS; zero uses closed loop")
	fs.DurationVar(&options.reportInterval, "report-interval", 10*time.Second, "Time-series interval")
	if err := fs.Parse(args); err != nil {
		return err
	}
	keys, valueBytes, err := validateCommon(options.commonOptions)
	if err != nil {
		return err
	}
	if options.duration <= 0 || options.warmup < 0 || options.reportInterval <= 0 {
		return errors.New("duration and report-interval must be positive; warmup cannot be negative")
	}
	if options.readRatio < 0 || options.readRatio > 100 {
		return errors.New("read-ratio must be between 0 and 100")
	}
	if options.distribution != "uniform" && options.distribution != "zipfian" {
		return errors.New("distribution must be uniform or zipfian")
	}
	if options.distribution == "zipfian" && (options.zipfS <= 1 || options.zipfV < 1) {
		return errors.New("zipf-s must be greater than 1 and zipf-v must be at least 1")
	}
	if options.targetQPS < 0 {
		return errors.New("target-qps cannot be negative")
	}

	values := makeValuePool(valueBytes, options.valuePool, options.valuePattern, options.seed)
	ready := sync.WaitGroup{}
	ready.Add(options.clients)
	startSignal := make(chan struct{})
	window := &runWindow{}
	workerResults := make(chan *workerResult, options.clients)
	intervalReports := make(chan *workerInterval, options.clients*2)
	var workers sync.WaitGroup
	for workerID := 0; workerID < options.clients; workerID++ {
		workers.Add(1)
		go runWorker(workerID, options, keys, values, &ready, startSignal, window, &workers, workerResults,
			intervalReports)
	}
	ready.Wait()
	window.measurementStart = time.Now().Add(options.warmup)
	window.measurementEnd = window.measurementStart.Add(options.duration)
	close(startSignal)

	intervalsDone := make(chan map[int]*aggregateInterval, 1)
	go func() {
		intervals := map[int]*aggregateInterval{}
		for report := range intervalReports {
			aggregate := intervals[report.index]
			if aggregate == nil {
				aggregate = &aggregateInterval{}
				intervals[report.index] = aggregate
			}
			aggregate.get.merge(&report.get)
			aggregate.set.merge(&report.set)
			aggregate.getAttempts += report.getAttempts
			aggregate.setAttempts += report.setAttempts
			aggregate.getMisses += report.getMisses
			aggregate.errors += report.errors
		}
		intervalsDone <- intervals
	}()

	workers.Wait()
	close(workerResults)
	close(intervalReports)
	intervals := <-intervalsDone

	var total workerResult
	for result := range workerResults {
		total.get.merge(&result.get)
		total.set.merge(&result.set)
		total.getAttempts += result.getAttempts
		total.setAttempts += result.setAttempts
		total.getMisses += result.getMisses
		total.errors += result.errors
	}
	attempts := total.getAttempts + total.setAttempts
	successful := total.get.count + total.set.count
	result := runResult{
		Tool:                 "xrcbench",
		Version:              toolVersion,
		Mode:                 "run",
		StartedAt:            window.measurementStart,
		CompletedAt:          time.Now(),
		Config:               makeResultConfig(options.commonOptions, keys, valueBytes),
		Attempts:             attempts,
		SuccessfulOperations: successful,
		Errors:               total.errors,
		GETMisses:            total.getMisses,
		GETHitRatio:          hitRatio(total.get.count, total.getMisses),
		AttemptedQPS:         float64(attempts) / options.duration.Seconds(),
		QPS:                  float64(successful) / options.duration.Seconds(),
		GET:                  total.get.summary(),
		SET:                  total.set.summary(),
		Intervals:            summarizeIntervals(intervals, options.duration, options.reportInterval),
	}
	result.Config.ReadRatio = options.readRatio
	result.Config.Distribution = options.distribution
	result.Config.ZipfS = options.zipfS
	result.Config.ZipfV = options.zipfV
	result.Config.TargetQPS = options.targetQPS
	result.Config.DurationSeconds = options.duration.Seconds()
	result.Config.WarmupSeconds = options.warmup.Seconds()
	result.Config.ReportIntervalMS = options.reportInterval.Milliseconds()
	return writeJSONResult(options.output, result)
}

func runWorker(workerID int, options runOptions, keys int64, values [][]byte, ready *sync.WaitGroup,
	startSignal <-chan struct{}, window *runWindow, workers *sync.WaitGroup, results chan<- *workerResult,
	reports chan<- *workerInterval) {
	defer workers.Done()
	rng := rand.New(rand.NewSource(options.seed + int64(workerID)*7919))
	var zipf *rand.Zipf
	if options.distribution == "zipfian" {
		zipf = rand.NewZipf(rng, options.zipfS, options.zipfV, uint64(keys-1))
	}
	client, _ := dialRESP(options.addr, options.password, options.timeout)
	ready.Done()
	<-startSignal

	for time.Now().Before(window.measurementStart) {
		if client == nil {
			client, _ = dialRESP(options.addr, options.password, options.timeout)
			if client == nil {
				time.Sleep(10 * time.Millisecond)
				continue
			}
		}
		_, _, err := executeOperation(client, options, keys, values, rng, zipf)
		if err != nil {
			client.close()
			client = nil
		}
	}

	measurementStart := window.measurementStart
	measurementEnd := window.measurementEnd
	var total workerResult
	currentInterval := 0
	interval := &workerInterval{index: currentInterval}
	var operationIndex int64

	for {
		var latencyStart time.Time
		if options.targetQPS > 0 {
			globalSequence := operationIndex*int64(options.clients) + int64(workerID)
			scheduledOffset := time.Duration(float64(globalSequence) / options.targetQPS * float64(time.Second))
			latencyStart = measurementStart.Add(scheduledOffset)
			if !latencyStart.Before(measurementEnd) {
				break
			}
			if wait := time.Until(latencyStart); wait > 0 {
				time.Sleep(wait)
			}
		} else {
			if time.Now().After(measurementEnd) {
				break
			}
			latencyStart = time.Now()
		}
		operationIndex++

		if client == nil {
			client, _ = dialRESP(options.addr, options.password, options.timeout)
		}
		isGET, miss, err := executeOperation(client, options, keys, values, rng, zipf)
		latency := time.Since(latencyStart)
		intervalIndex := min(int(time.Since(measurementStart)/options.reportInterval),
			int((options.duration-time.Nanosecond)/options.reportInterval))
		if intervalIndex != currentInterval {
			reports <- interval
			currentInterval = intervalIndex
			interval = &workerInterval{index: currentInterval}
		}

		if isGET {
			total.getAttempts++
			interval.getAttempts++
		} else {
			total.setAttempts++
			interval.setAttempts++
		}
		if err != nil {
			total.errors++
			interval.errors++
			if client != nil {
				client.close()
				client = nil
			}
		} else if isGET {
			total.get.record(latency)
			interval.get.record(latency)
			if miss {
				total.getMisses++
				interval.getMisses++
			}
		} else {
			total.set.record(latency)
			interval.set.record(latency)
		}
	}
	if client != nil {
		client.close()
	}
	reports <- interval
	results <- &total
}

func executeOperation(client *respClient, options runOptions, keys int64, values [][]byte, rng *rand.Rand,
	zipf *rand.Zipf) (bool, bool, error) {
	isGET := rng.Float64()*100 < options.readRatio
	var keyIndex int64
	if zipf != nil {
		keyIndex = int64(zipf.Uint64())
	} else {
		keyIndex = rng.Int63n(keys)
	}
	key := makeKey(options.prefix, options.keyWidth, options.startKey+keyIndex)
	if client == nil {
		return isGET, false, errors.New("connection unavailable")
	}
	if isGET {
		miss, err := client.command([]byte("GET"), key)
		return true, miss, err
	}
	value := values[rng.Intn(len(values))]
	args := [][]byte{[]byte("SET"), key, value}
	if options.ttl > 0 {
		args = append(args, []byte("PX"), []byte(strconv.FormatInt(options.ttl.Milliseconds(), 10)))
	}
	_, err := client.command(args...)
	return false, false, err
}

func summarizeIntervals(intervals map[int]*aggregateInterval, duration, reportInterval time.Duration) []intervalSummary {
	indexes := make([]int, 0, len(intervals))
	for index := range intervals {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	result := make([]intervalSummary, 0, len(indexes))
	for _, index := range indexes {
		aggregate := intervals[index]
		intervalDuration := min(reportInterval, duration-time.Duration(index)*reportInterval)
		attempts := aggregate.getAttempts + aggregate.setAttempts
		successful := aggregate.get.count + aggregate.set.count
		actualReadRatio := 0.0
		if attempts > 0 {
			actualReadRatio = float64(aggregate.getAttempts) * 100 / float64(attempts)
		}
		result = append(result, intervalSummary{
			Index:         index,
			StartMS:       (time.Duration(index) * reportInterval).Milliseconds(),
			Duration:      intervalDuration.Milliseconds(),
			GET:           aggregate.get.summary(),
			SET:           aggregate.set.summary(),
			Attempts:      attempts,
			Errors:        aggregate.errors,
			GETMisses:     aggregate.getMisses,
			GETHitRatio:   hitRatio(aggregate.get.count, aggregate.getMisses),
			AttemptedQPS:  float64(attempts) / intervalDuration.Seconds(),
			SuccessfulQPS: float64(successful) / intervalDuration.Seconds(),
			ReadRatio:     actualReadRatio,
		})
	}
	return result
}

func makeResultConfig(options commonOptions, keys int64, valueBytes int) resultConfig {
	return resultConfig{
		Addr:         options.addr,
		Prefix:       options.prefix,
		KeyWidth:     options.keyWidth,
		StartKey:     options.startKey,
		Keys:         keys,
		LogicalBytes: keys * int64(valueBytes),
		ValueBytes:   valueBytes,
		ValuePattern: options.valuePattern,
		ValuePool:    options.valuePool,
		Seed:         options.seed,
		Clients:      options.clients,
		TimeoutMS:    options.timeout.Milliseconds(),
		TTLMS:        options.ttl.Milliseconds(),
	}
}

func reportFirstError(ch chan<- error, err error) {
	select {
	case ch <- err:
	default:
	}
}

func writeJSONResult(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if path == "" {
		_, err = os.Stdout.Write(data)
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && filepath.Dir(path) != "." {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func parseBytes(input string) (int64, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return 0, errors.New("empty size")
	}
	upper := strings.ToUpper(value)
	units := []struct {
		suffix     string
		multiplier float64
	}{
		{"TIB", 1 << 40}, {"GIB", 1 << 30}, {"MIB", 1 << 20}, {"KIB", 1 << 10},
		{"TB", 1e12}, {"GB", 1e9}, {"MB", 1e6}, {"KB", 1e3}, {"B", 1},
	}
	for _, unit := range units {
		if strings.HasSuffix(upper, unit.suffix) {
			number := strings.TrimSpace(value[:len(value)-len(unit.suffix)])
			parsed, err := strconv.ParseFloat(number, 64)
			if err != nil || parsed < 0 || parsed*unit.multiplier > math.MaxInt64 {
				return 0, fmt.Errorf("invalid size %q", input)
			}
			return int64(math.Round(parsed * unit.multiplier)), nil
		}
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 0 {
		return 0, fmt.Errorf("invalid size %q", input)
	}
	return parsed, nil
}

func durationMS(value time.Duration) float64 {
	return float64(value) / float64(time.Millisecond)
}

func hitRatio(successfulGETs, misses uint64) float64 {
	if successfulGETs == 0 {
		return 0
	}
	return float64(successfulGETs-misses) / float64(successfulGETs)
}

func usage() {
	fmt.Fprintln(os.Stderr, "Usage: xrcbench <load|run> [options]")
	fmt.Fprintln(os.Stderr, "  load  Populate one logical dataset using pipelined SET")
	fmt.Fprintln(os.Stderr, "  run   Execute a measured GET/SET workload")
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "load":
		err = runLoad(os.Args[2:])
	case "run":
		err = runWorkload(os.Args[2:])
	case "version":
		fmt.Println(toolVersion)
		return
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "xrcbench:", err)
		os.Exit(1)
	}
}
