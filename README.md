# XRocksCache

## 中文

XRocksCache 是一个用 Go 重新实现的轻量级、单机 K/V 缓存服务，面向低成本 2C4G / 4C8G 云服务器。它保留 Redis RESP 协议中最常用的字符串读写命令，并内置本项目的缓存边界：key 最大 512KiB、value 最大 1MiB、写入 TTL 最长 15 天。

当前 Go 版目标不是复刻完整 Redis 或旧 C++ 实现，而是先提供一个可运行、可测试、可继续压测的低成本缓存内核。

### 当前能力

- 服务端二进制：`xrockscache`
- 协议：Redis RESP2 常用命令
- 命令：`GET`、`MGET`、`SET`、`MSET`、`DEL`、`EXISTS`、`EXPIRE`、`PEXPIRE`、`TTL`、`PTTL`、`INCR`、`DECR`、`INCRBY`、`DECRBY`、`PING`、`AUTH`、`INFO`、`DBSIZE`、`CLIENT`、`COMMAND`
- 存储：标准库追加日志文件 `xrockscache.aof` + 内存索引
- 日志：使用 Go 标准库 `log/slog` 输出结构化日志，支持 `text` / `json` 格式
- 过期：读路径保证过期 key 不可见，后台时间桶清理器延迟释放内存并持久化 `DEL` 删除记录
- 约束：key <= 512KiB，value <= 1MiB，TTL <= 15 天
- 依赖：当前服务端与压测工具均只使用 Go 标准库，根目录仅保留 Apache License 2.0

### 构建

```bash
go build -trimpath -o build/xrockscache ./cmd/xrockscache
```

### 运行

```bash
./build/xrockscache -c xrockscache.conf
```

### 连接

```bash
redis-cli -p 6666 PING
redis-cli -p 6666 SET hello world EX 60
redis-cli -p 6666 GET hello
```

### 测试

```bash
go test ./...
```

### C++ 基线

切换到 Go 前的 C++ 精简版已封存在本地 Git tag：

```bash
git checkout release_tag_cpp_baseline_20260907
```

## English

XRocksCache is a lightweight single-node K/V cache service rewritten in Go for low-cost 2C4G / 4C8G cloud servers. It keeps the most common Redis RESP string commands and enforces the product boundaries: maximum key size 512KiB, maximum value size 1MiB, and maximum write TTL 15 days.

The current Go implementation does not try to clone full Redis or the previous C++ implementation. It provides a runnable, testable, benchmark-ready low-cost cache kernel first.

### Current features

- Server binary: `xrockscache`
- Protocol: common Redis RESP2 commands
- Commands: `GET`, `MGET`, `SET`, `MSET`, `DEL`, `EXISTS`, `EXPIRE`, `PEXPIRE`, `TTL`, `PTTL`, `INCR`, `DECR`, `INCRBY`, `DECRBY`, `PING`, `AUTH`, `INFO`, `DBSIZE`, `CLIENT`, `COMMAND`
- Storage: standard-library append-only log file `xrockscache.aof` plus in-memory index
- Logging: structured logs through Go standard-library `log/slog`, with `text` / `json` formats; directory mode rotates daily and cleans old files by retention days
- Limits: key <= 512KiB, value <= 1MiB, TTL <= 15 days
- Dependencies: the current server and benchmark tool use only the Go standard library; the source tree keeps only the root Apache License 2.0 file

### Build

```bash
go build -trimpath -o build/xrockscache ./cmd/xrockscache
```

中文：主模块名为 `xrockscache`，内部包使用 `xrockscache/internal/...`。这样代码可以同时镜像到 Gitee 和 GitHub，不把模块身份绑定到某一个远端仓库。

English: The main module path is `xrockscache`, and internal packages use `xrockscache/internal/...`. This keeps the code mirror-friendly across both Gitee and GitHub without binding the module identity to one remote repository.

### Run

```bash
./build/xrockscache -c xrockscache.conf
```

### Connect

```bash
redis-cli -p 6666 PING
redis-cli -p 6666 SET hello world EX 60
redis-cli -p 6666 GET hello
```

### Logging / 日志

```conf
log-dir stdout
log-level info
log-format text
log-retention-days 15
```

中文：`log-dir stdout/stderr` 不做应用内轮转，适合容器、systemd 或云日志 Agent 接管。`log-dir <目录>` 时写入 `xrockscache-YYYY-MM-DD.log`，跨天自动切换，并删除超过 `log-retention-days` 的旧日志；`0` 表示不自动清理。

English: `log-dir stdout/stderr` does not rotate inside the application and is suitable for containers, systemd, or cloud logging agents. With `log-dir <directory>`, logs are written to `xrockscache-YYYY-MM-DD.log`, rotated automatically at day boundaries, and old files beyond `log-retention-days` are removed; `0` disables automatic cleanup.

### Active expiration / 主动过期

```conf
active-expire-enabled yes
active-expire-bucket-seconds 30
active-expire-interval-seconds 10
active-expire-cycle-budget-ms 10
active-expire-max-deletes-per-cycle 1000
```

中文：XRocksCache 使用时间桶维护 TTL key。读路径始终检查 `ExpiresAt`，所以过期 key 即使还没被后台物理删除，也不会被 `GET`、`EXISTS`、`TTL` 等命令看见。后台清理器按低频、限时、限量方式处理到期桶，删除内存记录，并向 AOF 追加 `DEL`，用于减少重启恢复和后续压缩压力。

English: XRocksCache tracks TTL keys with time buckets. The read path always checks `ExpiresAt`, so expired keys are invisible to commands such as `GET`, `EXISTS`, and `TTL` even before physical cleanup. The background cleaner processes due buckets with bounded time and delete budgets, removes expired entries from memory, and appends `DEL` records to the AOF to reduce recovery and future compaction pressure.

### Test

```bash
go test ./...
```

### C++ baseline

The minimal C++ baseline before the Go rewrite is preserved in the local Git tag:

```bash
git checkout release_tag_cpp_baseline_20260907
```
