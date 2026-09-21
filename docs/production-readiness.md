# 部署边界与上线验收

英文文档见 [production-readiness_EN.md](production-readiness_EN.md)。

## 当前行为

- 生产磁盘基线固定为阿里云 `cloud_essd / PL1`；PL0 不接受。100GiB 只用于 PL1 验证，PL2/PL3 需要满足各自最低容量后单独验收。

- 所有新写入最多保留 15 天，未指定 TTL 时使用 15 天；自增保留已有 TTL。旧版本无 TTL 数据不会被启动过程全库改写，升级需要重建缓存或逐项设置 TTL。
- 请求最多 1024 个参数，单个参数最多 1MiB，总负载约 8MiB；key 仍最多 512KiB。全部连接共享 64MiB 请求分配预算，含字符串复制预留。
- `MGET` 最多 64 个 key，响应负载最多 16MiB。`workers` 控制同时执行的命令数（最多 32），连接读写期限为 30 秒。
- 过期读取不删除记录；底层故障返回错误，不伪装为正常未命中。`DBSIZE` 仍是 RocksDB 估计值，可能包含尚未回收的过期记录。
- `SIGTERM`/`SIGINT` 停止接入，等待在途连接，30 秒后关闭剩余连接；底层不可中断 I/O 仍受系统服务停止期限约束。systemd 停止期限为 40 秒。
- 配置文件缺失、未知配置键会阻止启动。只支持整行注释，密码中的 `#` 保持原样。仅支持 RESP2；使用独立 `AUTH` 命令认证，不支持 `HELLO AUTH`。
- RocksDB compaction 负责物理回收，不承诺 60 秒内释放文件空间。拒绝水位不是自动淘汰策略，持续写满后需要等待回收、主动删除或增加容量。

## 构建与安装

文件日志按天切换，每份最多 16MiB，每天最多保留 3 份大小轮转备份，仍按配置保留天数清理。RocksDB 自身诊断日志最多保留 4 份、每份约 16MiB。

使用 `bash dev/build-rocksdb-wsl.sh` 构建；`bash dev/test-rocksdb-wsl.sh -race` 运行含竞争检测的真实 RocksDB 测试。默认 Go 构建没有占位 Store，必须具备 cgo 与 RocksDB。

发布脚本使用通用 CPU 指令集构建 RocksDB，运行测试和真实命令冒烟检查后打包。systemd 使用绝对数据目录 `/var/lib/xrockscache`。安装脚本默认不启动服务、不覆盖现有主配置；更改安装目录需自行配置服务单元。

通用 CPU 编译不代表任意 Linux 系统都兼容：C/C++ 和 LZ4 运行库仍有版本要求。发布包的 `install/runtime-deps.txt` 和 `install/build-platform.txt` 记录依赖与构建环境；必须在目标系统或其基线镜像验证启动。尚未通过目标环境验证的包不能视为通用 Linux 安装包。

## 上线前必须完成的环境验收

当前工作区的回归补丁尚未重新生成发布包；现有 `dist/` 安装包不能视为包含最新修复。过期计数器会按不存在的 key 重建并获得默认 15 天 TTL，未过期计数器保留原 TTL。发布脚本要求真实 RocksDB 竞态测试和命令冒烟均通过后再打包。

以下项目没有实测记录前均视为未验收。逻辑过期与物理空间释放分别验收；当前实现不满足 30–60 秒物理回收保证，不能仅通过缩短全库 compaction 周期宣称满足该目标。

1. 在实际 2C4G/4C8G 限额中验证 RSS、可用内存、连接峰值和容器限制，观察 OOM 及后台错误。
2. 在独立测试盘验证低剩余空间、磁盘写满、写入错误及恢复；不可使用生产数据盘做破坏性测试。
3. 验证异常退出后 WAL 恢复、真实 SST/Blob 落盘、过期 compaction 与 Blob GC，不以 memtable 内读写代替落盘验证。
4. 使用实际 value 分布、读写比例和 100G 数据规模验证持续负载；单元测试和短冒烟不证明 1 万 QPS 或 100ms 延迟达标。
5. 发布前确认目标机器 CPU、glibc、libstdc++、LZ4 与发行包兼容，验证全新安装、停止、重启及升级保留配置。

缓存必须部署于可信内网并设置认证。不要把 RESP 端口直接暴露到公网。监控 `disk_usage_bytes`、`disk_free_bytes`、`disk_reserve_bytes`、`disk_sample_failed`、`rocksdb_background_errors` 与 compaction backlog。
