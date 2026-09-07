# XRocksCache

XRocksCache is a single-node K/V cache for large-capacity, low-cost cache workloads. It uses RocksDB as the local storage engine and keeps the most common Redis-compatible string operations, with the goal of serving 100GiB-scale cache data on inexpensive cloud instances.

Current project scope:

- Server binary target: `xrockscache`
- Single-node deployment only; no cluster, replication, Lua, search, Pub/Sub, or complex Redis data structures
- Core commands: `GET`, `MGET`, `SET`, `MSET`, `DEL`, `EXISTS`, `EXPIRE`, `PEXPIRE`, `TTL`, `PTTL`, `PING`, `AUTH`, `INFO`, `DBSIZE`
- Maximum key size: 512KiB; maximum value size: 1MiB
- Maximum value TTL: 15 days
- Baseline goal: keep request latency within 100ms at 10k QPS on low-cost 2c4g/4c8g machines

Build:

```bash
cmake -S . -B build -G Ninja -DCMAKE_BUILD_TYPE=Release -DDISABLE_JEMALLOC=ON
cmake --build build --target xrockscache -j4
```

Run:

```bash
./build/xrockscache -c xrockscache.conf
```

Connect:

```bash
redis-cli -p 6666 PING
redis-cli -p 6666 SET hello world EX 60
redis-cli -p 6666 GET hello
```

Benchmark scripts live in `benchmark/` and cover data loading, closed-loop tests, open-loop tests, and result summaries for 2c4g and 4c8g profiles.

License: Apache License 2.0. See `LICENSE`.
