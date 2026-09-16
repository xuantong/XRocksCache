#!/usr/bin/env bash
set -euo pipefail

# 这个脚本刻意放在用户配置面之外。
# 它用于在 WSL/Linux 下验证生产 RocksDB 构建路径，
# 同时让 Windows 默认开发构建保持轻依赖。

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
WORKSPACE_DIR="$(cd -- "${PROJECT_DIR}/.." && pwd)"
ROCKSDB_DIR="${ROCKSDB_DIR:-${WORKSPACE_DIR}/rocksdb}"
# 放在项目 build 目录，避免 /tmp 被系统清理后丢失静态库。
ROCKSDB_BUILD_DIR="${ROCKSDB_BUILD_DIR:-${PROJECT_DIR}/build/rocksdb}"
LIBROCKSDB_A="${ROCKSDB_BUILD_DIR}/librocksdb.a"

if [[ ! -f "${ROCKSDB_DIR}/include/rocksdb/c.h" ]]; then
  echo "rocksdb c.h not found: ${ROCKSDB_DIR}/include/rocksdb/c.h" >&2
  exit 1
fi

mkdir -p "${ROCKSDB_BUILD_DIR}"

cmake -S "${ROCKSDB_DIR}" \
  -B "${ROCKSDB_BUILD_DIR}" \
  -G Ninja \
  -DCMAKE_BUILD_TYPE=Release \
  -DROCKSDB_BUILD_SHARED=ON \
  -DWITH_LZ4=ON \
  -DWITH_ZSTD=OFF \
  -DWITH_SNAPPY=OFF \
  -DWITH_ZLIB=OFF \
  -DWITH_BZ2=OFF \
  -DWITH_GFLAGS=OFF \
  -DWITH_TOOLS=OFF \
  -DWITH_CORE_TOOLS=OFF \
  -DWITH_BENCHMARK_TOOLS=OFF \
  -DWITH_ALL_TESTS=OFF \
  -DWITH_TESTS=OFF \
  -DFAIL_ON_WARNINGS=OFF \
  -DWITH_JEMALLOC=OFF \
  -DWITH_LIBURING=OFF

cmake --build "${ROCKSDB_BUILD_DIR}" --target rocksdb -j "${ROCKSDB_BUILD_JOBS:-2}"

if [[ ! -f "${LIBROCKSDB_A}" ]]; then
  echo "librocksdb.a not found under ${ROCKSDB_BUILD_DIR}" >&2
  exit 1
fi

export CGO_ENABLED=1
export CGO_CFLAGS="-I${ROCKSDB_DIR}/include"
export CGO_LDFLAGS="-L${ROCKSDB_BUILD_DIR} ${LIBROCKSDB_A} -lstdc++ -lm -llz4 -lpthread -ldl"

cd "${PROJECT_DIR}"
echo "using ${LIBROCKSDB_A}"
go build -tags rocksdb -trimpath -o build/xrockscache ./cmd/xrockscache
