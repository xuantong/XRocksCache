# XRocksCache Go Code Reading Guide

Chinese documentation is available in [code-reading-guide.md](code-reading-guide.md).

The main branch has been switched from C++ to Go. The minimal C++ baseline before the switch is preserved in `release_tag_cpp_baseline_20260907`.

Suggested reading order:

1. `cmd/xrockscache/main.go`: process entry point; loads config, initializes logging, calculates RocksDB auto-tuning, opens storage, and starts the server.
2. `internal/config/config.go`: parses `xrockscache.conf`. The main config keeps business-facing settings and does not expose RocksDB internals.
3. `internal/server/server.go`: TCP listener, connection lifecycle, and authentication state.
4. `internal/server/commands.go`: Redis RESP command dispatch and business semantics.
5. `internal/resp/resp.go`: RESP2 protocol reader and writer.
6. `internal/logging/logging.go`: structured logging initialization, daily rotation, and retention cleanup based on `log/slog`.
7. `internal/store/tuning.go`: derives RocksDB internal parameters from CPU, memory, and data-disk free space.
8. `internal/store/store_rocksdb.go`: production RocksDB Store, depends on cgo + RocksDB directly, and is the only Store implementation in the package.
9. `internal/store/types.go`: shared storage types and product limits.
10. `dev/build-rocksdb-wsl.sh`: when no system RocksDB library is available on WSL/Linux, builds RocksDB from source and sets `CGO_CFLAGS`/`CGO_LDFLAGS` before validating the XRocksCache build.
11. `test/`: main-module test directory, covering config, logging, resp, server, store, and benchmark black-box tests.

## Storage semantics

In the production path, RocksDB is the primary store. XRocksCache does not keep a full in-memory index and no longer recovers 100G-class data by replaying an application-level AOF. The old AOF plus in-memory Store has been removed. Persistence is handled by RocksDB WAL, memtables, SST files, and blob files.

Each RocksDB value contains an XRocksCache header followed by the user value. The header stores `expiresAtMs`, so the read path can hide expired keys immediately and the compaction filter can physically drop expired records during background compaction.

## Build paths

`go build`/`go test` use RocksDB by default, no extra build tag needed — but `CGO_ENABLED=1` and a discoverable RocksDB header/library are required:

```bash
CGO_ENABLED=1 go test ./...
CGO_ENABLED=1 go build -trimpath -o build/xrockscache ./cmd/xrockscache
```

When no system RocksDB development package is installed, build it from source and set the cgo environment variables first:

```bash
bash dev/build-rocksdb-wsl.sh
```
