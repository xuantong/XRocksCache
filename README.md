# XRocksCache

## 中文

XRocksCache 是一个面向大容量、低成本缓存场景的单机 K/V 缓存服务。它使用 RocksDB 作为本地存储引擎，保留 Redis 协议中最常用的字符串读写能力，目标是在便宜云服务器上承载 100GiB 级缓存数据。

当前项目边界：

- 服务端二进制目标：`xrockscache`
- 单机部署，不提供集群、复制、Lua、搜索、Pub/Sub、复杂 Redis 数据结构
- 支持核心命令：`GET`、`MGET`、`SET`、`MSET`、`DEL`、`EXISTS`、`EXPIRE`、`PEXPIRE`、`TTL`、`PTTL`、`PING`、`AUTH`、`INFO`、`DBSIZE`
- key 最大 512KiB，value 最大 1MiB
- value 最长过期时间 15 天
- 基线目标：2c4g/4c8g 低成本机器，1w QPS 下请求延迟保持在 100ms 内

构建：

```bash
cmake -S . -B build -G Ninja -DCMAKE_BUILD_TYPE=Release -DDISABLE_JEMALLOC=ON
cmake --build build --target xrockscache -j4
```

运行：

```bash
./build/xrockscache -c xrockscache.conf
```

连接：

```bash
redis-cli -p 6666 PING
redis-cli -p 6666 SET hello world EX 60
redis-cli -p 6666 GET hello
```

压测脚本位于 `benchmark/`，用于验证 2c4g 与 4c8g 场景下的数据装载、闭环压测、开放环压测与结果汇总。

许可证：Apache License 2.0。详见 `LICENSE`。

## English

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
