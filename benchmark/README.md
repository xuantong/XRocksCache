# XRocksCache Benchmark Suite / XRocksCache 压测套件

## 中文

本目录用于验证 XRocksCache 在低成本 2c4g 与 4c8g 机器上的容量、QPS 与延迟边界。压测客户端使用 Go 标准库直接发送 RESP 请求，不依赖服务端运行时。

### 工具

- `xrcbench load`：按确定性 key 范围装载数据。
- `xrcbench run`：执行 GET/SET 压测，输出 QPS、p50/p95/p99/p999、错误数、miss 数和时间序列 JSON。
- `run_case.sh`：按一个数据集/value 大小运行矩阵。
- `collect_metrics.sh`：采集进程 RSS、CPU、I/O、磁盘容量和 `INFO ALL`。
- `summarize.py`：把单个 JSON 或目录汇总成 CSV。

### 构建压测工具

```bash
cd benchmark
bash ./build.sh
```

### 装载数据

```bash
./bin/xrcbench load \
  --addr 10.0.0.10:6666 \
  --dataset-size 10GiB \
  --value-size 1KiB \
  --clients 8 \
  --pipeline 64 \
  --output results/10g_1k/load.json
```

`dataset-size` 表示逻辑 value 总量，不包含 key、metadata、WAL、SST 索引、Bloom filter 和写放大。容量结论必须同时记录实际落盘大小。

### 闭环压测

闭环模式用于观察固定客户端数量下服务端能跑到哪里：

```bash
./bin/xrcbench run \
  --addr 10.0.0.10:6666 \
  --dataset-size 10GiB \
  --value-size 1KiB \
  --clients 16 \
  --read-ratio 95 \
  --distribution zipfian \
  --warmup 30s \
  --duration 5m \
  --output results/10g_1k/r95_zipfian_closed.json
```

### 开放环压测

开放环模式用于 SLA 验收。延迟从计划发送时间开始计算，因此包含客户端排队，不隐藏 coordinated omission：

```bash
./bin/xrcbench run \
  --addr 10.0.0.10:6666 \
  --dataset-size 10GiB \
  --value-size 1KiB \
  --clients 16 \
  --read-ratio 95 \
  --distribution zipfian \
  --target-qps 10000 \
  --warmup 30s \
  --duration 10m \
  --output results/10g_1k/r95_zipfian_10000qps.json
```

主验收门槛：2c4g 与 4c8g 均需要在 95% GET / 5% SET、Zipfian、1KiB 随机 value、开放环 10,000 QPS 下，GET/SET p99 均不超过 100ms，且无错误、无非预期 miss、无 OOM、无持续 write stall。

### 采集服务端指标

```bash
XRC_HOST=127.0.0.1 XRC_PORT=6666 \
  bash ./collect_metrics.sh "$(pidof xrockscache)" results/10g_1k/server /data/xrockscache
```

启用认证时通过环境变量设置 `XRC_PASSWORD`。不要把密码写入压测结果文件。

### 汇总

```bash
python3 summarize.py results --output results/summary.csv
```

### 结果规则

- 原始 JSON、`INFO ALL`、进程指标、服务端配置、commit、命令行必须一起归档。
- 任何客户端错误都先视为失败，直到解释清楚。
- 非预期 GET miss 会让本轮结果无效。
- GET 和 SET 延迟必须分开看，整体平均值不能作为验收依据。
- 1MiB value 是边界稳定性测试，不是 1w QPS 验收场景。

## English

This directory validates XRocksCache capacity, QPS, and latency boundaries on low-cost 2c4g and 4c8g machines. The benchmark client is written in Go and sends RESP requests directly; it does not add runtime dependencies to the server.

### Tools

- `xrcbench load`: load data with deterministic key ranges.
- `xrcbench run`: run GET/SET workloads and emit QPS, p50/p95/p99/p999, errors, misses, and interval JSON.
- `run_case.sh`: run a matrix for one dataset/value-size pair.
- `collect_metrics.sh`: collect process RSS, CPU, I/O, disk capacity, and `INFO ALL`.
- `summarize.py`: summarize one JSON file or a result directory into CSV.

### Build the benchmark tool

```bash
cd benchmark
bash ./build.sh
```

### Load data

```bash
./bin/xrcbench load \
  --addr 10.0.0.10:6666 \
  --dataset-size 10GiB \
  --value-size 1KiB \
  --clients 8 \
  --pipeline 64 \
  --output results/10g_1k/load.json
```

`dataset-size` is the logical sum of value bytes. It excludes keys, metadata, WAL, SST indexes, Bloom filters, and write amplification. Capacity claims must also record actual on-disk bytes.

### Closed-loop benchmark

Closed-loop mode observes how far the server can go with a fixed client count:

```bash
./bin/xrcbench run \
  --addr 10.0.0.10:6666 \
  --dataset-size 10GiB \
  --value-size 1KiB \
  --clients 16 \
  --read-ratio 95 \
  --distribution zipfian \
  --warmup 30s \
  --duration 5m \
  --output results/10g_1k/r95_zipfian_closed.json
```

### Open-loop benchmark

Open-loop mode is used for SLA validation. Latency starts at the scheduled send time, so client-side backlog is included instead of hidden by coordinated omission:

```bash
./bin/xrcbench run \
  --addr 10.0.0.10:6666 \
  --dataset-size 10GiB \
  --value-size 1KiB \
  --clients 16 \
  --read-ratio 95 \
  --distribution zipfian \
  --target-qps 10000 \
  --warmup 30s \
  --duration 10m \
  --output results/10g_1k/r95_zipfian_10000qps.json
```

Primary acceptance gate: on both 2c4g and 4c8g, open-loop 10,000 QPS, 95% GET / 5% SET, Zipfian keys, 1KiB random values, GET/SET p99 no greater than 100ms, zero errors, no unexpected misses, no OOM, and no sustained write stalls.

### Collect server metrics

```bash
XRC_HOST=127.0.0.1 XRC_PORT=6666 \
  bash ./collect_metrics.sh "$(pidof xrockscache)" results/10g_1k/server /data/xrockscache
```

When authentication is enabled, set `XRC_PASSWORD` in the environment. Do not write passwords into result files.

### Summarize

```bash
python3 summarize.py results --output results/summary.csv
```

### Result rules

- Keep raw JSON, `INFO ALL`, process metrics, server configuration, commit, and command line together.
- Treat any client error as a failed run until explained.
- Unexpected GET misses invalidate the run.
- Report GET and SET latency separately; overall averages are not acceptance criteria.
- 1MiB values are boundary stability tests, not the 10k-QPS acceptance case.
