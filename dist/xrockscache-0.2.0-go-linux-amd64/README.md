# XRocksCache

XRocksCache 是一个轻量级、单机、Redis RESP 兼容的 String K/V 缓存服务，目标是在低成本 2C4G / 4C8G 云服务器上提供大容量本地磁盘缓存能力。

当前生产存储路径使用 RocksDB。XRocksCache 不自研 segment、SST 或 blob 文件格式，而是把 WAL、memtable、SST、blob files、compaction 和 blob GC 交给 RocksDB。

英文文档见 [README_EN.md](README_EN.md)。

## 当前能力

- 服务端二进制：`xrockscache`
- 协议：Redis RESP2 常用命令
- 命令：`GET`、`MGET`、`SET`、`MSET`、`DEL`、`EXISTS`、`EXPIRE`、`PEXPIRE`、`TTL`、`PTTL`、`INCR`、`DECR`、`INCRBY`、`DECRBY`、`PING`、`AUTH`、`INFO`、`DBSIZE`、`CLIENT`、`COMMAND`
- 生产存储：RocksDB，启用 LZ4、leveled compaction、blob files、blob GC、TTL compaction filter
- 默认构建即直接依赖 RocksDB：需要 `CGO_ENABLED=1` 并能找到 RocksDB 本地库；旧 AOF + 内存索引已删除
- 约束：key <= 512KiB，value <= 1MiB，写入 TTL <= 15 天
- 过期语义：读路径保证过期 key 不可见；RocksDB compaction filter 负责延迟物理清理
- 配置策略：主配置只保留业务参数，RocksDB 细节由程序根据 CPU、内存和数据盘剩余空间自动计算

## 构建

`go build`/`go test` 默认直接使用 RocksDB，不需要额外 build tag，但必须满足 `CGO_ENABLED=1` 且能找到 RocksDB 头文件和链接库：

```bash
CGO_ENABLED=1 go build -trimpath -o build/xrockscache ./cmd/xrockscache
CGO_ENABLED=1 go test ./...
```

系统已安装 RocksDB 开发库（如 `librocksdb-dev`）时，上述命令可直接使用。

Linux / WSL 下没有系统 RocksDB 库时，使用本地 RocksDB 源码构建并设置好 `CGO_CFLAGS`/`CGO_LDFLAGS` 后再构建：

```bash
bash dev/build-rocksdb-wsl.sh
```

该脚本默认使用同级目录的 RocksDB 仓库：

```text
../rocksdb
```

也可以手动指定：

```bash
ROCKSDB_DIR=/path/to/rocksdb bash dev/build-rocksdb-wsl.sh
```

## 运行

```bash
./build/xrockscache -c xrockscache.conf
```

## 连接

```bash
redis-cli -p 6666 PING
redis-cli -p 6666 SET hello world EX 60
redis-cli -p 6666 GET hello
```

## C++ 基线

切换到 Go 前的 C++ 精简版保存在本地 Git tag：

```bash
git checkout release_tag_cpp_baseline_20260907
```
