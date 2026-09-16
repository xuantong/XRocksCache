# XRocksCache Go + RocksDB Implementation Plan

Chinese documentation is available in [PLAN.md](PLAN.md).

## Product scope

XRocksCache is a lightweight, single-node, Redis RESP-compatible String K/V cache service. Its core goal is large local-disk cache capacity on low-cost 2C4G / 4C8G machines. It is not full Redis and not a primary database.

- One process and one local data directory.
- Production storage uses RocksDB; XRocksCache does not implement its own segment, SST, or blob file format.
- String key/value operations with TTL semantics only.
- Maximum key size: 512KiB; maximum value size: 1MiB.
- Maximum write TTL: 15 days; longer TTLs are rejected.
- Expiration visibility is guaranteed by the read path; physical cleanup is delayed to the RocksDB compaction filter.
- No cluster, replication, namespace isolation, transactions, Lua, Pub/Sub, search, or complex data structures.
- Nodes are disposable; cache-miss recovery belongs to callers.

## Completed

1. Created local release tag `release_tag_cpp_baseline_20260907` to preserve the minimal C++ baseline.
2. Removed the C++/CMake server source tree and added the Go module.
3. Implemented RESP2 parsing/writing, TCP serving, basic authentication, and command dispatch.
4. Kept the `xrockscache` binary target and `xrockscache.conf` configuration entry.
5. Added structured logging with `log/slog`, daily rotation, and retention cleanup.
6. Added RocksDB auto-tuning based on CPU, memory, and data-disk free space. RocksDB details are not exposed in the main config.
7. Added the production RocksDB Store with RocksDB WAL, memtables, SST files, blob files, LZ4, leveled compaction, blob GC, and a TTL compaction filter.
8. Added the WSL/Linux RocksDB build validation script `dev/build-rocksdb-wsl.sh`.
9. Added RocksDB-specific tests for expiration invisibility, restart recovery, conditional SET, KEEPTTL, large values, Stats/INFO shape, and disk watermarks.
10. Changed `DBSIZE` in the RocksDB build to return an estimate instead of scanning the whole keyspace.
11. Changed the common `SET` path to write directly; old values are read only for `NX`, `XX`, `GET`, and `KEEPTTL`.
12. Wired disk watermarks into writes: warn logs, slowdown delays briefly, and reject blocks new keys while allowing overwrites.
13. Removed the old AOF plus in-memory Store and the explicit-failure stub for builds without RocksDB; `internal/store` now keeps only the production Store, which depends on cgo + RocksDB directly. The default build already uses RocksDB — the `rocksdb` build tag is no longer needed.
14. Moved main-module Go tests into the `test/` directory and changed server tests to real RESP/TCP black-box tests.

## Current priorities

- Continue tightening RocksDB concurrency semantics and reducing command-level global lock scope.
- Expand end-to-end Redis protocol compatibility test coverage.
- Re-run 10k QPS and 100ms RT baselines on real 2C4G / 4C8G cloud servers.
- Use benchmark results to decide whether finer disk-watermark logic and compaction observability are needed.
