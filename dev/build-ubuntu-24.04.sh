#!/usr/bin/env bash
set -euo pipefail

# Ubuntu 24.04 x86_64 生产构建入口。
# 该脚本只负责校验构建平台，然后复用统一的 RocksDB + Go 构建逻辑。
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd -- "${SCRIPT_DIR}/.." && pwd)"

if [[ "$(uname -s)" != "Linux" || "$(uname -m)" != "x86_64" ]]; then
  echo "Ubuntu 24.04 构建要求 Linux x86_64" >&2
  exit 1
fi
if [[ ! -r /etc/os-release ]]; then
  echo "无法读取 /etc/os-release，拒绝生成 Ubuntu 24.04 构建" >&2
  exit 1
fi
# shellcheck disable=SC1091
source /etc/os-release
if [[ "${ID:-}" != "ubuntu" || "${VERSION_ID:-}" != "24.04" ]]; then
  echo "当前系统不是 Ubuntu 24.04：${PRETTY_NAME:-unknown}" >&2
  exit 1
fi
if [[ "${EUID}" -eq 0 ]]; then
  echo "警告：当前使用 root 编译；生产运行请使用非 root 用户" >&2
fi

cd "${PROJECT_DIR}"
export XRC_BUILD_PLATFORM="ubuntu-24.04"
echo "building ${XRC_BUILD_PLATFORM} on ${PRETTY_NAME}"
bash "${SCRIPT_DIR}/build-rocksdb-wsl.sh"

test -s "${PROJECT_DIR}/build/rocksdb/librocksdb.a"
test -x "${PROJECT_DIR}/build/xrockscache"
"${PROJECT_DIR}/build/xrockscache" -version
echo "Ubuntu 24.04 build passed: ${PROJECT_DIR}/build/xrockscache"
