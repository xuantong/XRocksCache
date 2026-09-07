# XRocksCache Code Reading Guide / XRocksCache 代码阅读指南

> 本文档帮助你按推荐顺序通读 XRocksCache 源码（约 1 万行）。
> This document helps you read through the XRocksCache source code (about 10k lines) in a recommended order.

## 中文

### 程序入口

程序入口是 `src/cli/main.cc` 中的 `main()` 函数（约 214 行，建议从这里开始）。
它做 6 件事：

1. 解析命令行参数（`-c` 配置文件、`-v` 版本、`--<key> <value>` 覆盖配置项）
2. 加载配置（`Config::Load`）
3. 初始化日志（`InitSpdlog`）
4. 端口冲突检查
5. 打开存储引擎（`engine::Storage::Open`，即打开 RocksDB）
6. 创建并启动 `Server`（`Server::Start`），然后 `Server::Join` 阻塞等待退出

### 推荐阅读顺序

#### 第一层：启动与生命周期

1. `src/cli/main.cc` — 入口
2. `src/server/server.h` — `Server` 类定义，先看成员再看实现
3. `src/server/server.cc` 的 `Server::Start()` — 创建 Worker 线程、启动 cron 后台任务（过期清理、压缩检查等）、监听端口
4. `src/config/config.h` / `config.cc` — 所有配置项定义与加载

#### 第二层：请求处理（一次 `SET key value` 的完整旅程）

5. `src/server/worker.cc` 的 `Worker::Run()` — libevent 事件循环，接受连接
6. `src/server/redis_request.cc` 的 `Request::Tokenize()` — RESP 协议解析，把字节流切成命令令牌
7. `src/server/redis_connection.cc` 的 `Connection::ExecuteCommands()` — 命令分发主循环（鉴权检查、查找命令表、执行）
8. `src/commands/commander.h` 的 `Commander` 基类 + 命令注册表 `CommandTable`（含 `EnableXRocksCacheProfile` 的 22 命令白名单）

#### 第三层：命令实现

9. `src/commands/cmd_xrockscache_string.cc`（约 207 行，最短，推荐先读）— `GET/SET/MGET/MSET/INCR/DECR` 的实现
10. `src/commands/cmd_xrockscache_key.cc` — `DEL/EXISTS/EXPIRE/TTL` 等
11. `src/commands/cmd_xrockscache_server.cc` — `AUTH/PING/INFO/DBSIZE` 等

#### 第四层：存储引擎

12. `src/types/redis_string.cc` — 字符串类型的具体读写逻辑（命令层与存储层之间的桥梁），包含 key/value 大小、TTL 上限的校验
13. `src/storage/redis_db.cc` — DB 级操作（过期检查、删除、扫描）
14. `src/storage/redis_metadata.cc` — 元数据编码（TTL、版本）
15. `src/storage/storage.cc` — RocksDB 封装：`Open()`、读写选项、WriteBatch、备份

### 一条请求的调用链（便于对照）

```
客户端字节流
  → Worker::Run (libevent 回调)
    → Request::Tokenize (RESP 解析)
      → Connection::ExecuteCommands
        → Server::LookupAndCreateCommand (命令表查找)
          → Commander::Execute (如 CommandSet::Execute)
            → redis::String::Set (src/types/redis_string.cc)
              → redis::Database / redis::Metadata (src/storage/)
                → RocksDB (engine::Storage::WriteToDB)
```

### 辅助小文件（随读随查即可）

- `src/common/encoding.h/cc` — 定长整数的紧凑编解码
- `src/common/parse_util.h` — 类型安全解析
- `src/server/redis_reply.cc` — RESP 应答格式化
- `src/common/cron.cc` — 后台周期任务框架

### 阅读建议

从 `main.cc` 顺着调用链往下走。每个文件都不大（最大的 `server.cc` 约 1700 行），
整体源码约 1 万行，半天可以通读一遍。

## English

### Entry point

The entry point is the `main()` function in `src/cli/main.cc` (about 214 lines; start here).
It does 6 things:

1. Parse command-line options (`-c` config file, `-v` version, `--<key> <value>` overrides)
2. Load configuration (`Config::Load`)
3. Initialize logging (`InitSpdlog`)
4. Check for port conflicts
5. Open the storage engine (`engine::Storage::Open`, i.e. open RocksDB)
6. Create and start the `Server` (`Server::Start`), then block in `Server::Join` until shutdown

### Recommended reading order

#### Layer 1: startup and lifecycle

1. `src/cli/main.cc` — entry point
2. `src/server/server.h` — the `Server` class definition; read members before implementations
3. `Server::Start()` in `src/server/server.cc` — creates worker threads, starts cron background tasks (expiry cleanup, compaction checks, etc.), listens on ports
4. `src/config/config.h` / `config.cc` — all config option definitions and loading

#### Layer 2: request handling (the full journey of one `SET key value`)

5. `Worker::Run()` in `src/server/worker.cc` — the libevent event loop that accepts connections
6. `Request::Tokenize()` in `src/server/redis_request.cc` — RESP protocol parsing; turns the byte stream into command tokens
7. `Connection::ExecuteCommands()` in `src/server/redis_connection.cc` — the main command dispatch loop (auth checks, command table lookup, execution)
8. The `Commander` base class and the `CommandTable` registry in `src/commands/commander.h` (includes the 22-command whitelist in `EnableXRocksCacheProfile`)

#### Layer 3: command implementations

9. `src/commands/cmd_xrockscache_string.cc` (about 207 lines, the shortest; read it first) — implementations of `GET/SET/MGET/MSET/INCR/DECR`
10. `src/commands/cmd_xrockscache_key.cc` — `DEL/EXISTS/EXPIRE/TTL`, etc.
11. `src/commands/cmd_xrockscache_server.cc` — `AUTH/PING/INFO/DBSIZE`, etc.

#### Layer 4: storage engine

12. `src/types/redis_string.cc` — concrete read/write logic for the string type (the bridge between the command layer and the storage layer); includes key/value size and TTL cap validation
13. `src/storage/redis_db.cc` — DB-level operations (expiry checks, deletion, scanning)
14. `src/storage/redis_metadata.cc` — metadata encoding (TTL, version)
15. `src/storage/storage.cc` — RocksDB wrapper: `Open()`, read/write options, WriteBatch, backup

### Call chain of a single request (for reference)

```
client byte stream
  → Worker::Run (libevent callback)
    → Request::Tokenize (RESP parsing)
      → Connection::ExecuteCommands
        → Server::LookupAndCreateCommand (command table lookup)
          → Commander::Execute (e.g. CommandSet::Execute)
            → redis::String::Set (src/types/redis_string.cc)
              → redis::Database / redis::Metadata (src/storage/)
                → RocksDB (engine::Storage::WriteToDB)
```

### Auxiliary small files (consult as needed)

- `src/common/encoding.h/cc` — compact encoding/decoding of fixed-size integers
- `src/common/parse_util.h` — type-safe parsing
- `src/server/redis_reply.cc` — RESP reply formatting
- `src/common/cron.cc` — background periodic task framework

### Reading tips

Follow the call chain downward from `main.cc`. Every file is small (the largest,
`server.cc`, is about 1700 lines), and the whole codebase is around 10k lines —
a full pass takes about half a day.
