#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
# 仅清理已经结束、且有明确目录名的历史测试，不匹配其他数据库。
for name in xrc_capacity_java.5Eg8em xrc_capacity_java.coSavS xrc_capacity_java.zbcyzN; do
  path="/tmp/${name}"
  [[ -d "${path}" ]] || continue
  resolved="$(realpath -e -- "${path}")"
  [[ ! -L "${path}" && "${resolved}" == "${path}" && "$(dirname "${resolved}")" == /tmp ]]
  if pgrep -af xrockscache | grep -F -- "-dir ${path}"; then
    echo "测试数据库仍在使用，拒绝删除：${path}" >&2
    exit 1
  fi
  mkdir -p "benchmark/results/${name}"
  cp "${path}"/rocksdb/LOG* "benchmark/results/${name}/" 2>/dev/null || true
  cp "${path}"/rocksdb/OPTIONS* "benchmark/results/${name}/" 2>/dev/null || true
  du -sh "${resolved}"
  rm -rf -- "${resolved}"
done
df -h /tmp
