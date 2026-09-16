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
