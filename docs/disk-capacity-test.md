# 阿里云 ESSD PL1 磁盘能力测试

上线基线固定为 `cloud_essd / PL1`，不接受 PL0。100GiB 可用于 PL1；PL2/PL3 需要更大容量，不能用 100GiB 结论代替。

1. 先在独立测试目录运行 `XRC_TEST_DIR=/data/xrc-test bash dev/disk-write-capacity-test.sh`，记录顺序写带宽和延迟。脚本默认只写 4GiB、运行 60 秒，并删除 `.probe`。
2. 在真实 PL1 服务器运行 `XRC_DISK_TYPE=cloud_essd XRC_DISK_PL=pl1 XRC_DISK_CAPACITY_GIB=100 bash dev/rate-sweep-java-wsl.sh`，按 10/20/30/35/40/50MiB/s 逐档测试 1GiB 随机 value。脚本会拒绝 PL0 或不符合容量要求的配置。
3. 以不出现 `Write stall`、L0 不持续增长且读取 P99 可接受的最高档作为候选速率；最终值取候选值的 70–80%，并在目标实例持续负载下复核。

测试目录只使用 `/tmp/xrc_*` 自动目录或调用者明确指定的目录。正常退出会删除自动目录；`XRC_KEEP_*` 仅用于调试保留。强制终止后需手动检查并清理。
