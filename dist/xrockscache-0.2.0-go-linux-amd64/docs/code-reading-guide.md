# XRocksCache Go 代码阅读指南

英文文档见 [code-reading-guide_EN.md](code-reading-guide_EN.md)。

主干已经从 C++ 切换为 Go。切换前的 C++ 精简版保存在 `release_tag_cpp_baseline_20260907`。

建议按这个顺序阅读：

1. `cmd/xrockscache/main.go`：进程入口，负责读取配置、初始化日志、计算 RocksDB 自动参数、打开存储并启动服务。
2. `internal/config/config.go`：解析 `xrockscache.conf`。主配置只保留业务参数，不暴露 RocksDB 内部细节。
3. `internal/server/server.go`：TCP 监听、连接生命周期和认证状态。
4. `internal/server/commands.go`：Redis RESP 命令分发和业务语义。
5. `internal/resp/resp.go`：RESP2 协议读写。
6. `internal/logging/logging.go`：基于 `log/slog` 的结构化日志初始化、按天切换和保留期清理。
7. `internal/store/tuning.go`：根据 CPU、内存和数据盘剩余空间推导 RocksDB 内部参数。
8. `internal/store/store_rocksdb.go`：生产 RocksDB Store，直接依赖 cgo + RocksDB，是包内唯一的 Store 实现。
9. `internal/store/types.go`：存储层公共类型和产品约束。
10. `dev/build-rocksdb-wsl.sh`：WSL/Linux 下没有系统 RocksDB 库时，从源码构建本地 RocksDB 并设置好 `CGO_CFLAGS`/`CGO_LDFLAGS` 后再验证 XRocksCache 构建路径。
11. `test/`：主模块测试目录，包含 config、logging、resp、server、store 和 benchmark 黑盒测试。

## 存储语义

生产路径中，RocksDB 是主存储。XRocksCache 不维护全量内存索引，也不再使用应用层 AOF 恢复 100G 级数据。旧 AOF + 内存 Store 已删除，持久化由 RocksDB WAL、memtable、SST 和 blob files 负责。

每个 RocksDB value 包含 XRocksCache 头部和用户 value。头部中保存 `expiresAtMs`，因此读路径可以立即隐藏过期 key，compaction filter 可以在后台压缩时物理丢弃过期记录。

## 构建路径

`go build`/`go test` 默认直接使用 RocksDB，不需要额外 build tag，但必须满足 `CGO_ENABLED=1` 且能找到 RocksDB 头文件和链接库：

```bash
CGO_ENABLED=1 go test ./...
CGO_ENABLED=1 go build -trimpath -o build/xrockscache ./cmd/xrockscache
```

系统未安装 RocksDB 开发库时，先用以下脚本从源码构建并设置好 cgo 环境变量：

```bash
bash dev/build-rocksdb-wsl.sh
```
