# 阿里云 ESSD PL1 磁盘能力测试

上线基线固定为 `cloud_essd / PL1`，不接受 PL0。100GiB 可用于 PL1；PL2/PL3 需要更大容量，不能用 100GiB 结论代替。

## 云端编译与测试

适用于 Ubuntu 24.04、4C8G 和 ESSD PL1 数据盘。100GiB 是逻辑数据目标，不等于云盘容量；WAL、元数据和压实临时空间还需要额外容量，建议使用至少 300GiB 的 PL1 数据盘挂载到 `/data`。不要在生产数据目录执行破坏性测试。

```bash
sudo apt-get update
sudo apt-get install -y git ca-certificates build-essential cmake ninja-build pkg-config liblz4-dev \
  golang-go openjdk-17-jdk redis-tools python3 fio sysstat tmux
```

先准备 RocksDB 源码。服务端构建脚本会自动编译该源码为静态库，然后链接 Go 程序，并非只编译 Go。以下按服务器项目目录 `/root/xrocks-cache` 编写；如果路径不同，请调整 `cd`。使用已核对的本地基线提交，不直接跟随上游最新分支：

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

整个子 shell 失败即停止，不会在编译失败后继续冒烟。成功产物为 `build/rocksdb/librocksdb.a` 和 `build/xrockscache`。已有 `/root/rocksdb` 时不会覆盖或切换其版本，版本校验失败请人工确认；使用其他源码目录时将 `ROCKSDB_DIR` 改为真实路径，并在构建及测试中保持一致。GitHub 不可访问时，可在可联网机器获取同一提交再上传完整源码；不需要上传本地编译产物。

常见报错：`rocksdb c.h not found` 表示依赖源码缺失或路径错误；`build/xrockscache: No such file or directory` 通常是前面的构建失败，不能通过跳过构建解决。

长时间测试放在 `tmux` 中。编译、全量测试或冒烟测试任一步失败，都不能继续容量压测。

如果服务器用户是 `root`，脚本会给出警告但允许编译；服务部署和运行仍应使用独立的非 root 用户。

上传当前工作区最新源码（含未提交新增文件），目录应为 `~/xrc-work/XRocksCache/` 与 `~/xrc-work/rocksdb/`，后者使用此前测试的相同 RocksDB 版本。不要上传本地 build、dist 或测试数据库。Go 至少为 1.22，Java 为 17。

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

在阿里云控制台核实 `/data` 对应的真实磁盘是 PL1；脚本只校验声明参数，不能自动验证云盘等级。测试前确认 6691 端口未占用：`ss -ltnH 'sport = :6691'` 应无输出。100GiB 测试建议至少保留约 250GiB 可用空间。

1. 先在独立测试目录运行 `XRC_TEST_DIR=/data/xrc-test bash dev/disk-write-capacity-test.sh`，记录顺序写带宽和延迟。脚本默认只写 4GiB、运行 60 秒，并删除 `.probe`。
2. 在真实 PL1 服务器运行 `XRC_DISK_TYPE=cloud_essd XRC_DISK_PL=pl1 XRC_DISK_CAPACITY_GIB=100 bash dev/rate-sweep-java-wsl.sh`，按 10/20/30/35/40/50MiB/s 逐档测试 1GiB 随机 value。脚本会拒绝 PL0 或不符合容量要求的配置。
3. 以不出现 `Write stall`、L0 不持续增长且读取 P99 可接受的最高档作为候选速率；最终值取候选值的 70–80%，并在目标实例持续负载下复核。

推荐先进行 10GiB 探路，再进行 100GiB 正式测试。阶梯脚本会删除传入目录下的 `rocksdb` 子目录，因此只能使用新建的隔离目录：

如果只跑当前生产基线，不需要复制长命令，直接执行封装入口：

```bash
bash dev/test-pl1-100g.sh
```

默认使用 `/data/xrc-bench`、ESSD PL1、声明容量 300GiB、35MiB/s、100GiB 随机数据和 6691 端口。可通过 `XRC_TEST_ROOT`、`XRC_DISK_CAPACITY_GIB`、`XRC_TEST_PORT` 修改。脚本会先检查至少 250GiB 可用空间，启动当前服务端，检查 INFO 中的 35MiB/s 限速，要求 `LOAD_PASS` 和 `VERIFY_PASS`，然后停止服务、保存报告并删除本轮临时数据库。服务未退出或路径安全校验失败时会保留数据并报告失败。

## 实时查看性能

压测命令在第一个 `tmux` 窗格运行。第二个 SSH 会话或窗格执行：

```bash
bash dev/watch-pl1-100g.sh benchmark/results/pl1-100g-时间戳
```

如果省略报告目录，脚本会自动选择最新的 `pl1-100g-*` 目录：

```bash
bash dev/watch-pl1-100g.sh
```

它每 5 秒显示 `INFO` 中的 memtable、L0、pending compaction、后台错误、磁盘使用和剩余空间，并显示服务 CPU、RSS、进程读写字节及 Java 压测最新进度。`Ctrl+C` 只退出监控，不会停止压测。可用 `XRC_WATCH_INTERVAL=1` 改为每秒刷新。

```bash
test_dir=$(mktemp -d /data/xrc-bench/sweep.XXXXXX)
XRC_DISK_TYPE=cloud_essd XRC_DISK_PL=pl1 XRC_DISK_CAPACITY_GIB=300 \
XRC_SWEEP_DATA_DIR="$test_dir" XRC_SWEEP_DATASET=10GiB \
XRC_SWEEP_RATES=35,40,45,50 XRC_SWEEP_TIMEOUT=3600 \
bash dev/rate-sweep-java-wsl.sh
rm -rf -- "$test_dir"
```

探路通过后将 `XRC_SWEEP_DATASET` 改为 `100GiB`，`XRC_SWEEP_TIMEOUT` 改为 `14400`，并将 `300` 改为真实云盘容量。每档必须同时出现 `LOAD_PASS` 和 `VERIFY_PASS`；仅看到“结果已保存”不代表测试通过。

固定目录或强制终止可能留下文件，下一轮前检查并清理：

```bash
find /tmp /data/xrc-bench -maxdepth 1 -type d \( -name 'xrc_*' -o -name 'fio.*' -o -name 'sweep.*' \)
df -h /data
```

当前阶梯脚本只在加载后进行抽样校验，不执行重启校验、GET RT 测量或完整 RocksDB 原生日志采集，且会忽略 Java 失败退出码，必须检查每档日志。该流程仅用于容量与限速初筛，不能证明重启持久性、10,000 QPS、100ms P99 或长期生产稳定性。上述固定目录示例只在命令执行到最后时清理；中断后先确认测试服务已经停止，再检查并删除本次明确创建的目录，禁止批量删除搜索结果。

测试目录只使用 `/tmp/xrc_*` 自动目录或调用者明确指定的目录。正常退出会删除自动目录；`XRC_KEEP_*` 仅用于调试保留。强制终止后需手动检查并清理。
