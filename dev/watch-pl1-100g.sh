#!/usr/bin/env bash
set -euo pipefail

# 实时观察正在运行的 PL1 100GiB 测试；只读，不修改测试数据。
PORT="${XRC_TEST_PORT:-6691}"
INTERVAL="${XRC_WATCH_INTERVAL:-5}"
REPORT="${1:-}"
if [[ -z "${REPORT}" ]]; then
  REPORT="$(ls -dt benchmark/results/pl1-100g-* 2>/dev/null | head -1 || true)"
fi
[[ -n "${REPORT}" && -d "${REPORT}" ]] || { echo '请传入报告目录，例如 benchmark/results/pl1-100g-20260921-120000'; exit 1; }
command -v redis-cli >/dev/null || { echo '缺少 redis-cli'; exit 1; }
command -v ss >/dev/null || { echo '缺少 ss'; exit 1; }

while :; do
  clear
  now="$(date '+%F %T %Z')"
  echo "XRocksCache 实时监控  ${now}"
  echo "报告目录: ${REPORT}  端口: ${PORT}  刷新: ${INTERVAL}s"
  echo
  if ! ss -ltnH "sport = :${PORT}" | grep -q .; then
    echo '服务未监听，可能尚未启动或已经退出。'
  else
    echo '--- INFO ---'
    redis-cli -h 127.0.0.1 -p "${PORT}" --raw INFO 2>/dev/null | grep -E '^(uptime|connected_clients|db0:|write_rate|rocksdb_(estimate_live|pending|background|oldest|immutable|num_running|memtable|l0)|disk_(usage|free|reserve|sample))' || true
  fi
  echo
  echo '--- 进程资源 ---'
  pgrep -a xrockscache | while read -r pid rest; do
    ps -p "${pid}" -o pid=,ppid=,rss=,vsz=,pcpu=,etime=,cmd= || true
    if [[ -r "/proc/${pid}/io" ]]; then grep -E '^(read_bytes|write_bytes):' "/proc/${pid}/io" || true; fi
  done
  echo
  echo '--- 文件系统 ---'
  df -h /data/xrc-bench 2>/dev/null || df -h .
  echo
  echo '--- 最新压测进度 ---'
  if [[ -f "${REPORT}/rate-35.log" ]]; then tail -n 3 "${REPORT}/rate-35.log"; else echo '尚未生成 rate-35.log'; fi
  echo
  echo '按 Ctrl+C 退出监控（不会停止压测）。'
  sleep "${INTERVAL}"
done
