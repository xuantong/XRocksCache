# XRocksCache

XRocksCache is a lightweight, single-node, Redis RESP-compatible String K/V cache service. The current production baseline is Alibaba Cloud `cloud_essd / PL1`, with 4C8G and a 100GiB data disk as the first validation target. PL0 is outside the supported production scope.

The production storage path uses RocksDB. XRocksCache delegates WAL, memtables, SST files, blob files, compaction, and blob GC to RocksDB instead of implementing its own segment, SST, or blob file format.

Chinese documentation is available in [README.md](README.md).

## Current features

- Server binary: `xrockscache`
- Protocol: common Redis RESP2 commands
- Commands: `GET`, `MGET`, `SET`, `MSET`, `DEL`, `EXISTS`, `EXPIRE`, `PEXPIRE`, `TTL`, `PTTL`, `INCR`, `DECR`, `INCRBY`, `DECRBY`, `PING`, `AUTH`, `INFO`, `DBSIZE`, `CLIENT`, `COMMAND`
- Production storage: RocksDB with LZ4, leveled compaction, blob files, blob GC, and a TTL compaction filter
- The default build depends on RocksDB directly: requires `CGO_ENABLED=1` and a discoverable RocksDB library. The old AOF plus in-memory index has been removed.
- Limits: key <= 512KiB, value <= 5MiB, write TTL <= 15 days
- Expiration semantics: the read path guarantees expired keys are invisible; the RocksDB compaction filter performs delayed physical cleanup
- Configuration strategy: the main config keeps business-facing settings, while RocksDB internals are derived automatically from CPU, memory, and data-disk free space

## Build

See [deployment boundaries and release acceptance](docs/production-readiness_EN.md) for resource limits, upgrade notes, and pending environment validation. Passing tests does not establish the 100G / 10k QPS / 100ms targets.

`go build`/`go test` use RocksDB by default, no extra build tag needed — but `CGO_ENABLED=1` and a discoverable RocksDB header/library are required:

```bash
CGO_ENABLED=1 go build -trimpath -o build/xrockscache ./cmd/xrockscache
CGO_ENABLED=1 go test ./...
```

This works directly when the RocksDB development package (e.g. `librocksdb-dev`) is installed system-wide.

On Linux / WSL without a system RocksDB library, build RocksDB from source and set `CGO_CFLAGS`/`CGO_LDFLAGS` first:

```bash
bash dev/build-rocksdb-wsl.sh
```

The script uses the sibling RocksDB repository by default:

```text
../rocksdb
```

You can override it manually:

```bash
ROCKSDB_DIR=/path/to/rocksdb bash dev/build-rocksdb-wsl.sh
```

## Run

```bash
./build/xrockscache -c xrockscache.conf
```

## Connect

```bash
redis-cli -p 6666 PING
redis-cli -p 6666 SET hello world EX 60
redis-cli -p 6666 GET hello
```

## C++ baseline

The minimal C++ version before the Go rewrite is preserved in the local Git tag:

```bash
git checkout release_tag_cpp_baseline_20260907
```
