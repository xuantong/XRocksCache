#!/usr/bin/env bash
set -euo pipefail

# 该脚本用于生成可交付给同学直接安装的 Linux x86_64 发布包。
# 服务端包必须使用 RocksDB 生产构建；压测工具独立构建后随包一起发布。

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
VERSION="${XRC_VERSION:-0.2.0-go}"
OS_ARCH="linux-amd64"
DIST_DIR="${PROJECT_DIR}/dist"
PACKAGE_NAME="xrockscache-${VERSION}-${OS_ARCH}"
STAGE_DIR="${DIST_DIR}/${PACKAGE_NAME}"

cd "${PROJECT_DIR}"

bash dev/build-rocksdb-wsl.sh

mkdir -p "${DIST_DIR}"
if [[ "${STAGE_DIR}" != "${DIST_DIR}/"* || "${PACKAGE_NAME}" != xrockscache-* ]]; then
  echo "拒绝清理异常发布目录：${STAGE_DIR}" >&2
  exit 1
fi
rm -rf "${STAGE_DIR}" "${DIST_DIR}/${PACKAGE_NAME}.tar.gz" "${DIST_DIR}/${PACKAGE_NAME}.tar.gz.sha256"

mkdir -p "${STAGE_DIR}/bin" \
  "${STAGE_DIR}/conf" \
  "${STAGE_DIR}/docs" \
  "${STAGE_DIR}/install" \
  "${STAGE_DIR}/benchmark"

cp build/xrockscache "${STAGE_DIR}/bin/xrockscache"
ldd "${STAGE_DIR}/bin/xrockscache" > "${STAGE_DIR}/install/runtime-deps.txt"

(
  cd benchmark
  go build -trimpath -o "${STAGE_DIR}/bin/xrcbench" ./xrcbench
)

cp xrockscache.conf "${STAGE_DIR}/conf/xrockscache.conf"
cp xrockscache-4c8g.conf "${STAGE_DIR}/conf/xrockscache-4c8g.conf"
cp LICENSE NOTICE README.md README_EN.md SECURITY.md SECURITY_EN.md THREAT_MODEL.md THREAT_MODEL_EN.md "${STAGE_DIR}/"
cp PLAN.md PLAN_EN.md "${STAGE_DIR}/docs/"
cp docs/*.md "${STAGE_DIR}/docs/"
cp benchmark/README.md benchmark/README_EN.md "${STAGE_DIR}/benchmark/"
cp benchmark/case.env.example benchmark/run_case.sh benchmark/collect_metrics.sh benchmark/summarize.py "${STAGE_DIR}/benchmark/"

cat >"${STAGE_DIR}/install/xrockscache.service" <<'SERVICE'
[Unit]
Description=XRocksCache
After=network.target

[Service]
Type=simple
User=xrockscache
Group=xrockscache
ExecStart=/usr/local/xrockscache/bin/xrockscache -c /etc/xrockscache/xrockscache.conf
Restart=on-failure
RestartSec=3
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
SERVICE

cat >"${STAGE_DIR}/install/install-linux.sh" <<'INSTALL'
#!/usr/bin/env bash
set -euo pipefail

# 该脚本把解压后的 XRocksCache 安装到 Linux 主机。
# 默认安装路径为 /usr/local/xrockscache，配置路径为 /etc/xrockscache。

PREFIX="${XRC_INSTALL_PREFIX:-/usr/local/xrockscache}"
CONFIG_DIR="${XRC_CONFIG_DIR:-/etc/xrockscache}"
DATA_DIR="${XRC_DATA_DIR:-/var/lib/xrockscache}"
SERVICE_FILE="/etc/systemd/system/xrockscache.service"
PACKAGE_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"

if [[ "$(id -u)" != "0" ]]; then
  echo "请使用 root 或 sudo 执行安装脚本" >&2
  exit 1
fi

if ! id xrockscache >/dev/null 2>&1; then
  useradd --system --home-dir "${DATA_DIR}" --shell /usr/sbin/nologin xrockscache
fi

install -d -m 0755 "${PREFIX}/bin" "${CONFIG_DIR}" "${DATA_DIR}"
install -m 0755 "${PACKAGE_DIR}/bin/xrockscache" "${PREFIX}/bin/xrockscache"
install -m 0755 "${PACKAGE_DIR}/bin/xrcbench" "${PREFIX}/bin/xrcbench"

if [[ ! -f "${CONFIG_DIR}/xrockscache.conf" ]]; then
  install -m 0644 "${PACKAGE_DIR}/conf/xrockscache.conf" "${CONFIG_DIR}/xrockscache.conf"
fi
install -m 0644 "${PACKAGE_DIR}/conf/xrockscache-4c8g.conf" "${CONFIG_DIR}/xrockscache-4c8g.conf"
chown -R xrockscache:xrockscache "${DATA_DIR}"

if command -v systemctl >/dev/null 2>&1; then
  install -m 0644 "${PACKAGE_DIR}/install/xrockscache.service" "${SERVICE_FILE}"
  systemctl daemon-reload
  echo "安装完成。启动命令：systemctl enable --now xrockscache"
else
  echo "安装完成。当前系统未检测到 systemd，请手动启动：${PREFIX}/bin/xrockscache -c ${CONFIG_DIR}/xrockscache.conf"
fi
INSTALL
chmod +x "${STAGE_DIR}/install/install-linux.sh"

cat >"${STAGE_DIR}/INSTALL.md" <<'DOC'
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
DOC

cat >"${STAGE_DIR}/INSTALL_EN.md" <<'DOC'
# XRocksCache Linux Installation Guide

Chinese documentation is available in [INSTALL.md](INSTALL.md).

## Install

```bash
tar -xzf xrockscache-0.2.0-go-linux-amd64.tar.gz
cd xrockscache-0.2.0-go-linux-amd64
sudo bash install/install-linux.sh
```

Default locations:

- Program directory: `/usr/local/xrockscache`
- Config directory: `/etc/xrockscache`
- Data directory: `/var/lib/xrockscache`
- systemd service: `xrockscache`

## Runtime dependencies

Dynamic dependencies of the server binary are recorded in `install/runtime-deps.txt`.
The current release package is built with a local static RocksDB library, but the target Linux system still needs C/C++ runtime libraries and the LZ4 runtime library.
On Debian/Ubuntu, install them with:

```bash
sudo apt-get update
sudo apt-get install -y libstdc++6 libgcc-s1 liblz4-1 libc6
```

## Start

```bash
sudo systemctl enable --now xrockscache
redis-cli -p 6666 PING
```

## Manual run

```bash
bin/xrockscache -c conf/xrockscache.conf -dir ./data
```

## Notes

- This package is the Linux x86_64 RocksDB production package.
- The target machine must provide RocksDB runtime dependencies. If startup reports missing shared libraries, install the distribution RocksDB/LZ4 runtime libraries first.
- In production, set `requirepass` and deploy the service inside a trusted private network or behind a secure gateway.
DOC

(
  cd "${DIST_DIR}"
  tar -czf "${PACKAGE_NAME}.tar.gz" "${PACKAGE_NAME}"
  sha256sum "${PACKAGE_NAME}.tar.gz" > "${PACKAGE_NAME}.tar.gz.sha256"
)

echo "${DIST_DIR}/${PACKAGE_NAME}.tar.gz"
echo "${DIST_DIR}/${PACKAGE_NAME}.tar.gz.sha256"
