# XRocksCache Benchmark Suite

Chinese documentation is available in [README.md](README.md).

This directory validates XRocksCache capacity, QPS, and latency boundaries on low-cost 2C4G and 4C8G machines. The benchmark client sends RESP requests directly with the Go standard library and does not depend on server runtime code.

## Tools

`qps` and `attempted_qps` use actual measurement elapsed time, including the final in-flight request, recorded as `elapsed_seconds`. Target-rate mode stops sending accumulated work after the window ends while measuring latency from scheduled send time. Under overload, actual throughput falls below the target. Loading counts a batch only after successful responses; reconnecting is not a successful write.

- `xrcbench load`: load data across a deterministic key range.
- `xrcbench run`: run GET/SET benchmarks and output QPS, p50/p95/p99/p999, errors, misses, and time-series JSON.
- `run_case.sh`: run a matrix for one dataset and value size.
- `collect_metrics.sh`: collect process RSS, CPU, I/O, disk capacity, and `INFO ALL`.
- `summarize.py`: summarize one JSON file or a directory into CSV.

## Build benchmark tool

```bash
cd benchmark
bash ./build.sh
```

## Load data

```bash
./bin/xrcbench load \
  --addr 10.0.0.10:6666 \
  --dataset-size 10GiB \
  --value-size 1KiB \
  --clients 8 \
  --pipeline 64 \
  --output results/10g_1k/load.json
```

`dataset-size` is the logical value size. It does not include keys, metadata, WAL, SST indexes, Bloom filters, or write amplification. Capacity conclusions must also record the actual on-disk size.

## Closed-loop benchmark

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

## Open-loop benchmark

Open-loop mode is used for SLA acceptance. Latency is measured from the scheduled send time, so client-side queuing is included and coordinated omission is not hidden:

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

Primary acceptance gate: on both 2C4G and 4C8G, open-loop 10,000 QPS, 95% GET / 5% SET, Zipfian keys, 1KiB random values, GET/SET p99 no greater than 100ms, zero errors, no unexpected misses, no OOM, and no sustained write stalls.

## Collect server metrics

```bash
XRC_HOST=127.0.0.1 XRC_PORT=6666 \
  bash ./collect_metrics.sh "$(pidof xrockscache)" results/10g_1k/server /data/xrockscache
```

When authentication is enabled, set `XRC_PASSWORD` through the environment. Do not write passwords into benchmark result files.
