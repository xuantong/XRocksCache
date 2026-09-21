# Alibaba Cloud ESSD PL1 disk capacity test

The production baseline is fixed to `cloud_essd / PL1`; PL0 is not accepted. 100GiB is valid for PL1. PL2/PL3 require larger disks, so a 100GiB result cannot represent them.

## Cloud build and test

This flow targets Ubuntu 24.04, 4C8G, and an ESSD PL1 data disk. 100GiB is the logical dataset target, not the disk size; WAL, metadata, and compaction temporary space require additional capacity. Use at least a 300GiB PL1 disk mounted at `/data`, never a production data directory.

```bash
sudo apt-get update
sudo apt-get install -y git ca-certificates build-essential cmake ninja-build pkg-config liblz4-dev \
  golang-go openjdk-17-jdk redis-tools python3 fio sysstat tmux
```

Prepare RocksDB source first. The build script compiles it into a static library before linking the Go server. The example uses `/root/xrocks-cache`; adjust the project path as needed. Pin the verified local baseline commit instead of following upstream HEAD:

```bash
(
set -euo pipefail
cd /root/xrocks-cache
export ROCKSDB_DIR=/root/rocksdb
if [[ ! -e "$ROCKSDB_DIR" ]]; then
  git clone --no-checkout https://github.com/facebook/rocksdb.git "$ROCKSDB_DIR"
  git -C "$ROCKSDB_DIR" checkout --detach 7a90dc5ba8398dc52dd6824ca2d6bc46acca7b04
fi
test -f "$ROCKSDB_DIR/include/rocksdb/c.h"
test "$(git -C "$ROCKSDB_DIR" rev-parse HEAD)" = 7a90dc5ba8398dc52dd6824ca2d6bc46acca7b04
find dev benchmark -type f -name '*.sh' -exec sed -i 's/\r$//' {} +
ROCKSDB_BUILD_JOBS=2 bash dev/build-ubuntu-24.04.sh
test -s build/rocksdb/librocksdb.a
test -x build/xrockscache
build/xrockscache -version
bash dev/test-rocksdb-wsl.sh -race
bash dev/smoke-rocksdb-wsl.sh
)
```

The subshell stops at the first failure. Outputs are `build/rocksdb/librocksdb.a` and `build/xrockscache`. Existing RocksDB directories are not overwritten or switched; investigate a revision mismatch manually. Set `ROCKSDB_DIR` consistently for build and tests when using another directory. If GitHub is unavailable, acquire the same revision elsewhere and upload the complete source instead of local binaries.

`rocksdb c.h not found` means the dependency source is missing or the path is wrong. A missing `build/xrockscache` generally follows an earlier build failure; do not skip the build.

Use `tmux` for long-running tests. Do not continue to capacity testing if build, full tests, or smoke checks fail.

If the cloud host user is `root`, the script warns but permits the build; deployment and runtime should still use a dedicated non-root user.

Upload the latest working tree, including uncommitted new files, as `~/xrc-work/XRocksCache/` alongside `~/xrc-work/rocksdb/`, using the previously tested RocksDB version. Exclude local build, dist and database files. Require Go 1.22 or later and Java 17.

```bash
go version
java -version
javac -version
findmnt -T /data
df -h /data
free -h
nproc
sudo mkdir -p /data/xrc-bench
sudo chown "$(id -un):$(id -gn)" /data/xrc-bench
```

Verify in the Alibaba Cloud console that the disk mounted at `/data` is actually PL1; scripts validate declarations only. Before testing, `ss -ltnH 'sport = :6691'` must return no listener. Reserve approximately 250GiB free space for the 100GiB test.

Run `XRC_TEST_DIR=/data/xrc-test bash dev/disk-write-capacity-test.sh` on an isolated directory to record sequential write bandwidth and latency. The default probe writes 4GiB for 60 seconds and removes `.probe`.

On the real PL1 server run `XRC_DISK_TYPE=cloud_essd XRC_DISK_PL=pl1 XRC_DISK_CAPACITY_GIB=100 bash dev/rate-sweep-java-wsl.sh` to test 1GiB of random values at 10/20/30/35/40/50MiB/s. The script rejects PL0 and invalid capacities.

Choose the highest rate without `Write stall`, sustained L0 growth, or unacceptable GET P99; use 70–80% of that rate as the candidate and recheck under sustained target load. Automatic `/tmp/xrc_*` directories are removed on normal exit; forced termination requires manual cleanup.

Run a 10GiB pilot before the 100GiB test. The sweep script deletes the supplied directory's `rocksdb` subdirectory, so use a new isolated directory:

For the current production baseline, use the wrapped entry point instead of copying the long command:

```bash
bash dev/test-pl1-100g.sh
```

Defaults are `/data/xrc-bench`, declared ESSD PL1 capacity 300GiB, 35MiB/s, 100GiB of random data, and port 6691. Override with `XRC_TEST_ROOT`, `XRC_DISK_CAPACITY_GIB`, and `XRC_TEST_PORT`. The script requires at least 250GiB free, verifies the 35MiB/s setting through INFO, requires both `LOAD_PASS` and `VERIFY_PASS`, then stops the server, preserves the report, and removes the temporary database. If shutdown or path safety checks fail, it retains the data and reports failure.

```bash
test_dir=$(mktemp -d /data/xrc-bench/sweep.XXXXXX)
XRC_DISK_TYPE=cloud_essd XRC_DISK_PL=pl1 XRC_DISK_CAPACITY_GIB=300 \
XRC_SWEEP_DATA_DIR="$test_dir" XRC_SWEEP_DATASET=10GiB \
XRC_SWEEP_RATES=35,40,45,50 XRC_SWEEP_TIMEOUT=3600 \
bash dev/rate-sweep-java-wsl.sh
rm -rf -- "$test_dir"
```

After the pilot passes, use `XRC_SWEEP_DATASET=100GiB`, `XRC_SWEEP_TIMEOUT=14400`, and replace `300` with the actual disk capacity. Each rate must contain both `LOAD_PASS` and `VERIFY_PASS`; the “results saved” line alone is not a pass.

Before the next run, inspect leftover files after forced termination:

```bash
find /tmp /data/xrc-bench -maxdepth 1 -type d \( -name 'xrc_*' -o -name 'fio.*' -o -name 'sweep.*' \)
df -h /data
```

The current sweep only verifies samples after loading; it does not test restart persistence, GET RT, or preserve full RocksDB native logs. It also ignores Java failure exit codes, so inspect each rate log. This flow screens capacity and rate limits only, not restart persistence, 10,000 QPS, 100ms P99, or long-term stability. Fixed-directory examples clean up only if execution reaches the final command. After interruption, confirm the test server is stopped and remove only the explicitly created directory, never bulk-delete search results.
