# XRocksCache RocksDB Store

Chinese documentation is available in [rocksdb-store.md](rocksdb-store.md).

## Storage scope

The production storage path of XRocksCache uses RocksDB. XRocksCache does not implement its own segment, SST, or blob file engine. The production `Store` implementation in `internal/store` depends on cgo + RocksDB directly, so the default build already uses the real RocksDB Store — no extra build tag is needed:

```bash
CGO_ENABLED=1 go build -trimpath -o build/xrockscache ./cmd/xrockscache
```

This requires a discoverable RocksDB header and library (either a system `librocksdb-dev` package, or one built from source via `dev/build-rocksdb-wsl.sh` with `CGO_CFLAGS`/`CGO_LDFLAGS` set) — otherwise the build fails.

## Data layout

The RocksDB key is the user key. The RocksDB value is an XRocksCache header followed by the user value:

```text
0..3    magic: XRC1
4       version: 1
5       flags, reserved
6..7    reserved
8..15   expiresAtMs, little-endian; 0 means no expiration
16..19  user value length, little-endian
20..    user value
```

TTL metadata must live with the value in RocksDB so the read path and compaction filter can decide expiration without depending on a full in-memory index.

## TTL semantics

XRocksCache guarantees:

- Expired keys are invisible immediately on reads.
- `GET`, `EXISTS`, `TTL`, and `PTTL` decode `expiresAtMs`.
- If an expired key is read, the command returns it as missing and writes a best-effort RocksDB delete.
- The RocksDB compaction filter physically drops expired records during background compaction.

Therefore, expiration correctness does not depend on immediate compaction. Compaction only performs delayed physical reclamation.

## AOF removal

XRocksCache no longer uses `xrockscache.aof` as primary storage. The old AOF plus in-memory Store has been removed. Persistence is handled by RocksDB WAL, memtables, SST files, and blob files.

This is the core change: XRocksCache no longer keeps the full cache dataset in an in-memory map and no longer recovers 100G-class data through application-level AOF replay.

## File growth control

The first version does not introduce kvrocks' complex checker. It controls file growth through:

- LZ4 compression
- leveled compaction
- TTL compaction filter
- blob files and blob GC
- `soft_pending_compaction_bytes_limit`
- `hard_pending_compaction_bytes_limit`
- background rate limiter
- `periodic_compaction_seconds`
- XRocksCache disk watermarks

These parameters are computed at startup from CPU, memory, and data-disk free space. They are not exposed in the main config file.
