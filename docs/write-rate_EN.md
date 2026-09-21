# Baseline write rate

The production baseline is Alibaba Cloud `cloud_essd / PL1`; the server defaults to `write-rate-mib 35` MiB/s, accepting 1–1024. PL0 is outside the supported scope. All connections share one database budget without accumulating idle burst credits. 35MiB/s is an initial PL1/4C8G validation value, not a final performance promise.

SET accounts for key, value and header bytes; MSET reserves the complete batch. Increments reserve the key, maximum integer text and header. EXPIRE conservatively reserves the maximum value size, potentially limiting small-value expiration updates. Deletes and immediate expiration bypass the new-version limit to preserve a recovery path. Unsuccessful conditional writes and failed operations may still consume reservations.

Waiting occurs before key locking and is bounded to one second. Excess requests receive `ERR write rate limit exceeded`. Clients should use bounded backoff and must not blindly retry non-idempotent commands after ambiguous network timeouts. RocksDB backpressure and disk protection remain enabled. The limiter does not guarantee total request latency below 100ms.

On successful or failed exit, the capacity script stops the server, preserves logs, and deletes its temporary database. Use `bash dev/cleanup-capacity-wsl.sh` for the three explicitly named historical runs; active databases are rejected. Reports remain in `benchmark/results/`. Forced termination or a system crash may bypass exit cleanup and requires inspection.
