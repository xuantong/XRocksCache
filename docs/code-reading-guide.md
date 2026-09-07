# XRocksCache Go Code Reading Guide / XRocksCache Go 代码阅读指南

## 中文

主干已经从 C++ 切换为 Go。切换前的 C++ 精简版保存在 `release_tag_cpp_baseline_20260907`。

建议按这个顺序阅读：

1. `cmd/xrockscache/main.go`：进程入口，负责读取配置、打开存储、启动服务。
2. `internal/config/config.go`：解析 `xrockscache.conf`，兼容忽略旧配置中的无关字段。
3. `internal/server/server.go`：TCP 监听、连接生命周期、认证状态。
4. `internal/server/commands.go`：Redis RESP 命令分发和业务语义。
5. `internal/logging/logging.go`：基于 `log/slog` 的结构化日志初始化。
6. `internal/resp/resp.go`：RESP2 协议读写。
7. `internal/store/store.go`：带 TTL 的内存索引和追加日志持久化。

当前存储是标准库追加日志实现，目标是先保证 Go 版服务端可运行、可测试、可压测。后续如果需要更换存储后端，应把变更限制在 `internal/store` 或新增存储适配层中，不要污染命令层。

当前主干服务端与压测工具均只使用 Go 标准库，因此不再保留旧 C++ 版本的第三方依赖许可证目录。

## English

The main branch has been switched from C++ to Go. The minimal C++ baseline before the switch is preserved in `release_tag_cpp_baseline_20260907`.

Suggested reading order:

1. `cmd/xrockscache/main.go`: process entry point; loads config, opens storage, and starts the server.
2. `internal/config/config.go`: parses `xrockscache.conf` and ignores unrelated legacy fields.
3. `internal/server/server.go`: TCP listener, connection lifecycle, and authentication state.
4. `internal/server/commands.go`: Redis RESP command dispatch and business semantics.
5. `internal/logging/logging.go`: structured logging initialization based on `log/slog`.
6. `internal/resp/resp.go`: RESP2 protocol reader and writer.
7. `internal/store/store.go`: TTL-aware in-memory index plus append-only persistence.

The current storage layer uses a standard-library append-only log so the Go server is runnable, testable, and benchmark-ready first. If the storage backend is replaced later, keep the change inside `internal/store` or a new storage adapter layer instead of leaking storage details into the command layer.

The current server and benchmark tool use only the Go standard library, so the third-party dependency license directory from the previous C++ version is no longer kept.
