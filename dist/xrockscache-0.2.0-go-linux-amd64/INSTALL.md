# XRocksCache Linux 安装说明

英文文档见 [INSTALL_EN.md](INSTALL_EN.md)。

## 安装

```bash
tar -xzf xrockscache-0.2.0-go-linux-amd64.tar.gz
cd xrockscache-0.2.0-go-linux-amd64
sudo bash install/install-linux.sh
```

默认安装位置：

- 程序目录：`/usr/local/xrockscache`
- 配置目录：`/etc/xrockscache`
- 数据目录：`/var/lib/xrockscache`
- systemd 服务：`xrockscache`

## 运行依赖

服务端二进制的动态依赖记录在 `install/runtime-deps.txt`。
当前发布包使用本地 RocksDB 静态库构建，但仍需要目标 Linux 系统提供 C/C++ 运行库和 LZ4 运行库。
Debian/Ubuntu 可先执行：

```bash
sudo apt-get update
sudo apt-get install -y libstdc++6 libgcc-s1 liblz4-1 libc6
```

## 启动

```bash
sudo systemctl enable --now xrockscache
redis-cli -p 6666 PING
```

## 手动运行

```bash
bin/xrockscache -c conf/xrockscache.conf -dir ./data
```

## 注意事项

- 该包是 Linux x86_64 RocksDB 生产包。
- 目标机器需要提供 RocksDB 运行依赖；如果启动时报动态库缺失，请先安装发行版的 RocksDB/LZ4 运行库。
- 生产环境建议设置 `requirepass`，并把服务部署在可信内网或安全网关之后。
