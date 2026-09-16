# XRocksCache 智能体协作指南

本仓库当前主干已切换为 Go 实现。切换前的 C++ 精简版保存在本地 Git tag `release_tag_cpp_baseline_20260907`。

## 项目边界

- 服务端二进制目标固定为 `xrockscache`。
- 默认配置文件为 `xrockscache.conf`，4C8G 基线配置为 `xrockscache-4c8g.conf`。
- 项目聚焦单机 String K/V 缓存，不提供 Cluster、Replication、Lua、Search、Pub/Sub 或复杂 Redis 数据结构。
- 生产存储必须使用 RocksDB；旧 AOF + 内存实现已删除，默认构建即直接依赖 RocksDB，不再提供无 RocksDB 库时的编译检查占位实现。
- key 最大 512KiB，value 最大 1MiB，写入 TTL 最长 15 天。
- 优先保证低成本 2C4G / 4C8G 云服务器上的可部署性、可压测性和 100ms 内延迟目标。

## 构建与测试

`go build`/`go test` 默认直接使用 RocksDB，不需要额外 build tag，但必须满足 `CGO_ENABLED=1` 且能找到 RocksDB 本地库（头文件与链接库），否则会编译失败：

```bash
CGO_ENABLED=1 go build -trimpath -o build/xrockscache ./cmd/xrockscache
CGO_ENABLED=1 go test ./...
```

系统已安装 `librocksdb-dev`（如 Dockerfile 中的构建镜像）时，上述命令即可直接使用系统库。WSL/Linux 下没有系统库时，用以下脚本从源码构建 RocksDB 并设置 `CGO_CFLAGS`/`CGO_LDFLAGS` 后再执行构建：

```bash
bash dev/build-rocksdb-wsl.sh
```

## 文档规则

- `AGENTS.md` 始终保持中文，不生成英文副本。
- 用户文档必须拆分为两份：中文主文件使用原文件名，例如 `README.md`；英文文件使用 `_EN.md` 结尾，例如 `README_EN.md`。
- 禁止在同一个用户文档中用 `## 中文` / `## English` 形式混写双语内容。
- 代码标识符、Go 包名、命令、配置键、日志字段、协议命令保持英文，不强行翻译。
- 修改中文用户文档时，必须同步修改对应 `_EN.md` 英文文档；新增中文用户文档时，也必须新增对应英文文档。

## Go 语言开发规则

- 主模块路径保持为 `xrockscache`，内部包使用 `xrockscache/internal/...`，避免绑定到 Gitee 或 GitHub 单一远端。
- Go 文件必须先执行 `gofmt`，测试文件命名使用 `*_test.go`，并统一放在仓库根目录 `test/` 下。
- `internal/store` 的生产 Store 实现（`store_rocksdb.go`）直接依赖 cgo + RocksDB，不使用 `rocksdb` build tag 做可选开关；不允许重新引入 AOF + 内存 Store 或无 RocksDB 库时的占位实现。
- 包内职责保持清晰：`cmd/xrockscache` 只做启动编排，`internal/server` 处理 RESP 命令语义，`internal/store` 处理存储语义，`internal/logging` 处理日志。
- 存储层升级优先通过 `internal/store` 抽象扩展，不要把 RocksDB 细节泄漏到 RESP 命令层。
- 服务端日志必须使用 `internal/logging` / `log/slog`，不要用 `fmt.Printf` 打业务运行日志；文件日志必须保持按天切换和保留期清理语义。
- 错误信息要保持 Redis 兼容风格时，继续使用 `ERR ...` / `WRONGPASS ...` 等英文协议文本。
- 修改命令协议、TTL 语义、大小限制、磁盘水位线或 RocksDB 调参逻辑后，必须补充或更新 Go 测试。
