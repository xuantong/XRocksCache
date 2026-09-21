# Deployment boundaries and release acceptance

Chinese documentation: [production-readiness.md](production-readiness.md).

## Current behavior

- The production disk baseline is Alibaba Cloud `cloud_essd / PL1`; PL0 is not accepted. 100GiB applies only to PL1 validation; PL2/PL3 require their own minimum-capacity acceptance.

- All new writes expire within 15 days, with a 15-day default. Increments preserve existing TTLs. Startup does not rewrite legacy records without TTL; rebuild the cache or assign TTLs during upgrade.
- Requests allow 1024 arguments, 5MiB per argument, and approximately 8MiB total payload; keys remain limited to 512KiB. Connections share a 64MiB request allocation budget including string-copy reservations.
- MGET allows 64 keys and 16MiB response payload. The workers setting bounds concurrent command execution (maximum 32). Connection read/write deadlines are 30 seconds.
- Expired reads do not delete records. Storage faults return errors rather than cache misses. DBSIZE remains an estimate that may include unreclaimed expired records.
- SIGTERM/SIGINT stop admission and wait for in-flight connections, closing remaining connections after 30 seconds. Uninterruptible storage I/O is still subject to the service manager's stop deadline, set to 40 seconds for systemd.
- Missing configuration files and unknown keys fail startup. Only whole-line comments are supported, preserving # in passwords. Only RESP2 is supported; authenticate using AUTH, not HELLO AUTH.
- Physical reclamation depends on RocksDB compaction and has no 60-second completion guarantee. Watermarks do not implement eviction: a full cache requires reclamation, deletion, or additional capacity.

## Build and install

File logs rotate daily and at 16MiB, keeping up to 3 size-rotation backups per day and applying the configured retention period. RocksDB diagnostic logs retain up to 4 files of approximately 16MiB each.

Build with `bash dev/build-rocksdb-wsl.sh`; run real RocksDB tests with race detection using `bash dev/test-rocksdb-wsl.sh -race`. Default Go builds require cgo and RocksDB and never produce a stub Store.

Packaging builds RocksDB for a baseline CPU and runs tests and real-command smoke checks before archiving. systemd uses the absolute data directory `/var/lib/xrockscache`. Installation does not start the service or overwrite existing main configuration. Custom installation paths require a custom service unit.

A baseline CPU build does not guarantee compatibility with every Linux distribution. C/C++ and LZ4 runtimes still have version requirements. The package records dependencies and build environment in `install/runtime-deps.txt` and `install/build-platform.txt`. Validate startup on the target OS or its baseline image before treating an artifact as deployable there.

## Required environment acceptance

The current regression patch has not been repackaged; existing `dist/` artifacts must not be assumed to contain the latest fixes. Expired counters are recreated with the default 15-day TTL, while live counters retain their original TTL. Packaging requires real RocksDB race tests and command smoke checks to pass before creating an archive.

The following items remain unaccepted until measured evidence is available. Validate logical expiration separately from physical space reclamation. The current implementation does not guarantee physical reclamation within 30–60 seconds; shortening whole-database compaction intervals alone does not establish that guarantee.

1. Validate RSS, available memory, peak connections, cgroup limits, OOM events, and background errors under actual 2C4G/4C8G limits.
2. Test low free space, disk exhaustion, write errors, and recovery on an isolated test disk, never a production data disk.
3. Validate WAL recovery after abnormal exit, actual SST/Blob flushes, expiration compaction, and Blob GC rather than only memtable round trips.
4. Test sustained workloads with actual value sizes and read/write distribution at 100G scale. Unit tests and short smoke runs do not establish 10k QPS or 100ms latency.
5. Verify CPU, glibc, libstdc++, and LZ4 compatibility, fresh installation, stop/restart, and configuration preservation on upgrade.

Deploy within a trusted private network with authentication. Do not expose RESP directly to the Internet. Monitor `disk_usage_bytes`, `disk_free_bytes`, `disk_reserve_bytes`, `disk_sample_failed`, `rocksdb_background_errors`, and compaction backlog.
