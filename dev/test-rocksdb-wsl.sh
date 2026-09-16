#!/usr/bin/env bash
set -euo pipefail

# 该脚本只负责在 WSL/Linux 下跑 Go 测试。
# 依赖 dev/build-rocksdb-wsl.sh 已生成的静态库 build/rocksdb/librocksdb.a。

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
WORKSPACE_DIR="$(cd -- "${PROJECT_DIR}/.." && pwd)"
ROCKSDB_DIR="${ROCKSDB_DIR:-${WORKSPACE_DIR}/rocksdb}"
ROCKSDB_BUILD_DIR="${ROCKSDB_BUILD_DIR:-${PROJECT_DIR}/build/rocksdb}"
LIBROCKSDB_A="${ROCKSDB_BUILD_DIR}/librocksdb.a"

if [[ ! -f "${ROCKSDB_DIR}/include/rocksdb/c.h" ]]; then
  echo "rocksdb c.h not found: ${ROCKSDB_DIR}/include/rocksdb/c.h" >&2
  exit 1
fi

if [[ ! -f "${LIBROCKSDB_A}" ]]; then
  echo "librocksdb.a not found: ${LIBROCKSDB_A}" >&2
  echo "请先执行: bash dev/build-rocksdb-wsl.sh" >&2
  exit 1
fi

export CGO_ENABLED=1
export CGO_CFLAGS="-I${ROCKSDB_DIR}/include"
export CGO_LDFLAGS="-L${ROCKSDB_BUILD_DIR} ${LIBROCKSDB_A} -lstdc++ -lm -llz4 -lpthread -ldl"

cd "${PROJECT_DIR}"
echo "using ${LIBROCKSDB_A}"
go test -tags rocksdb ./...
