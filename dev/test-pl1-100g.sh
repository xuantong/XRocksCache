#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
if [[ "${1:-}" == --help ]]; then
  echo '用法：bash dev/test-pl1-100g.sh'
  echo '脚本会自动探测机器配置与数据盘位置，并在报告 environment.txt 中输出实测值，避免与声明值混淆。'
  echo '默认：自动在真实磁盘（/dev/*）挂载点中选择空间最大且可写的一个作为测试盘（可用空间须 >= 250GiB），PL1 声明 300GiB，100GiB 数据，35MiB/s，端口 6691。'
  echo '可选：XRC_TEST_ROOT、XRC_DISK_CAPACITY_GIB、XRC_TEST_PORT。'
  exit 0
fi
for tool in java javac redis-cli ss timeout realpath df awk mktemp; do command -v "${tool}" >/dev/null; done
test -x build/xrockscache
test -f benchmark/java/CapacityCheck.java
test -f xrockscache-4c8g.conf
PORT="${XRC_TEST_PORT:-6691}"
CAPACITY="${XRC_DISK_CAPACITY_GIB:-300}"
[[ "${PORT}" =~ ^[0-9]{1,5}$ && "${CAPACITY}" =~ ^[0-9]{1,5}$ ]] || exit 1
(( 10#${PORT} >= 1 && 10#${PORT} <= 65535 && 10#${CAPACITY} >= 20 && 10#${CAPACITY} <= 65536 )) || exit 1
listeners="$(ss -ltnH "sport = :${PORT}")"
[[ -z "${listeners}" ]] || { echo '测试端口已占用，停止'; exit 1; }
ROOT="${XRC_TEST_ROOT:-}"
if [[ -z "${ROOT}" ]]; then
  # 自动探测数据盘：只考虑 source 为 /dev/*（或 LVM 等）的真实磁盘挂载点，按可用空间从大到小取第一个可写的。
  while read -r avail mount; do
    [[ -n "${mount}" ]] || continue
    if mkdir -p "${mount%/}/xrc-bench" 2>/dev/null; then
      ROOT="${mount%/}/xrc-bench"
      break
    fi
  done < <( { df -B1 --output=target,fstype,source,avail 2>/dev/null || true; } \
    | awk 'NR>1 && $3 ~ /^\/dev\// {print $4, $1}' \
    | sort -rn | awk '!seen[$2]++')
  [[ -n "${ROOT}" ]] || { echo '未能自动探测到可写的数据盘挂载点，请用 XRC_TEST_ROOT=/your/path 指定' >&2; df -h / /data /mnt /u01 /home 1>&2 2>/dev/null || true; exit 1; }
else
  mkdir -p "${ROOT}" 2>/dev/null || { echo "无法创建测试目录：${ROOT}" >&2; exit 1; }
fi
mkdir -p benchmark/results
ROOT="$(realpath -e -- "${ROOT}")"
free_bytes="$(df -B1 --output=avail "${ROOT}" | awk 'NR==2 {print $1}')"
(( free_bytes >= 250*1024*1024*1024 )) || { echo "测试盘可用空间不足 250GiB（${ROOT}），停止"; exit 1; }

# 采集机器与磁盘实测信息，供 environment.txt 输出。
CPU_MODEL="$(awk -F': ' '/^model name/ {print $2; exit}' /proc/cpuinfo 2>/dev/null || echo unknown)"
MEM_TOTAL_GIB="$(awk '/MemTotal/ {printf "%.1fGiB", $2/1024/1024}' /proc/meminfo 2>/dev/null || echo unknown)"
DATA_MOUNT="$(df --output=target "${ROOT}" | awk 'NR==2 {print $1}')"
DATA_DEVICE="$(df --output=source "${ROOT}" | awk 'NR==2 {print $1}')"
DATA_FSTYPE="$(df --output=fstype "${ROOT}" | awk 'NR==2 {print $1}')"
DATA_SIZE_HUMAN="$(df -h --output=size "${DATA_MOUNT}" | awk 'NR==2 {print $1}')"
DATA_AVAIL_HUMAN="$(df -h --output=avail "${DATA_MOUNT}" | awk 'NR==2 {print $1}')"
DATA_DEVICE_MODEL="unknown"
if [[ "${DATA_DEVICE}" == /dev/* ]] && command -v lsblk >/dev/null 2>&1; then
  # 追溯分区所属整盘的型号；云盘通常显示为厂商通用型号，无法直接区分 PL 等级。
  DATA_PARENT="$(lsblk -no PKNAME "${DATA_DEVICE}" 2>/dev/null | head -n1 || true)"
  DATA_DEVICE_MODEL="$(lsblk -dno MODEL "${DATA_PARENT:-${DATA_DEVICE}}" 2>/dev/null | xargs || echo unknown)"
  [[ -n "${DATA_DEVICE_MODEL}" ]] || DATA_DEVICE_MODEL=unknown
fi
REPORT="$(mktemp -d "$PWD/benchmark/results/pl1-100g-$(date +%Y%m%d-%H%M%S).XXXXXX")"
DATA="$(mktemp -d "${ROOT}/xrc-pl1-100g.XXXXXX")"
SERVER_PID=""
cleanup() {
  status=$?
  trap - EXIT
  set +e
  if [[ -n "${SERVER_PID}" ]]; then
    kill -TERM "${SERVER_PID}" 2>/dev/null
    for _ in $(seq 1 40); do kill -0 "${SERVER_PID}" 2>/dev/null || break; sleep 1; done
    if kill -0 "${SERVER_PID}" 2>/dev/null; then
      echo "服务未退出，保留数据：${DATA}" >&2
      echo CLEANUP_BLOCKED > "${REPORT}/status.txt"
      exit 1
    fi
    wait "${SERVER_PID}"
  fi
  for file in "${DATA}"/rocksdb/LOG* "${DATA}"/rocksdb/OPTIONS*; do
    [[ -f "${file}" ]] && cp -- "${file}" "${REPORT}/"
  done
  if [[ ! -L "${DATA}" && "$(realpath -e -- "${DATA}")" == "${DATA}" && "$(dirname "${DATA}")" == "${ROOT}" && "$(basename "${DATA}")" == xrc-pl1-100g.* ]]; then
    rm -rf -- "${DATA}" || status=1
  else
    status=1
  fi
  if (( status == 0 )); then echo PASS > "${REPORT}/status.txt"; else echo FAIL > "${REPORT}/status.txt"; fi
  echo "报告：${REPORT}，退出码：${status}"
  exit "${status}"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
echo "报告：${REPORT}"
echo "临时数据：${DATA}（退出时清理）"
{
  echo "== 时间 =="
  date -Ins
  echo
  echo "== 内核 =="
  uname -a
  echo
  echo "== CPU（实测）=="
  echo "cores=$(nproc) model=${CPU_MODEL}"
  echo
  echo "== 内存（实测）=="
  echo "total=${MEM_TOTAL_GIB}"
  free -h
  echo
  echo "== 数据盘（实测）=="
  echo "mount=${DATA_MOUNT}"
  echo "device=${DATA_DEVICE}"
  echo "device_model=${DATA_DEVICE_MODEL}"
  echo "fstype=${DATA_FSTYPE}"
  echo "size=${DATA_SIZE_HUMAN} avail=${DATA_AVAIL_HUMAN}"
  df -h "${ROOT}"
  echo
  echo "== 磁盘声明（脚本参数，非实测）=="
  echo "declared_disk_type=cloud_essd declared_pl=PL1 declared_capacity_gib=${CAPACITY}"
  echo '注意：PL 等级无法在主机侧直接读取，请在阿里云控制台确认数据盘（'"${DATA_DEVICE}"'）实际类型与性能等级。'
} > "${REPORT}/environment.txt"
echo '--- 机器与磁盘自检 ---'
grep -v '^$' "${REPORT}/environment.txt" | sed 's/^/  /'
echo '-----------------------'
echo '开始测试'
sed "s/^write-rate-mib .*/write-rate-mib 35/; s/^disk-type .*/disk-type cloud_essd/; s/^disk-pl .*/disk-pl pl1/; s/^disk-capacity-gib .*/disk-capacity-gib ${CAPACITY}/" xrockscache-4c8g.conf > "${REPORT}/config.conf"
javac -d "${REPORT}" benchmark/java/CapacityCheck.java
build/xrockscache -c "${REPORT}/config.conf" -dir "${DATA}" -bind 127.0.0.1 -port "${PORT}" > "${REPORT}/server.log" 2>&1 &
SERVER_PID=$!
ready=0
for _ in $(seq 1 100); do
  kill -0 "${SERVER_PID}" || { cat "${REPORT}/server.log"; exit 1; }
  if [[ "$(timeout 2 redis-cli -p "${PORT}" --raw PING 2>/dev/null || true)" == PONG ]]; then ready=1; break; fi
  sleep 0.1
done
(( ready == 1 )) || { cat "${REPORT}/server.log"; exit 1; }
timeout 5 redis-cli -p "${PORT}" --raw INFO > "${REPORT}/info-before.txt"
grep -q '^write_rate_bytes_sec:36700160' "${REPORT}/info-before.txt" || { echo '程序未启用 35MiB/s 限速，请重新编译'; exit 1; }
timeout 14400 java -Xmx256m -cp "${REPORT}" CapacityCheck "${PORT}" 107374182400 "${REPORT}/samples.txt" 2>&1 | tee "${REPORT}/rate-35.log"
grep '^LOAD_PASS ' "${REPORT}/rate-35.log"
grep '^VERIFY_PASS ' "${REPORT}/rate-35.log"
timeout 5 redis-cli -p "${PORT}" --raw INFO > "${REPORT}/info-35.txt"
echo '100GiB 加载及内容、TTL 抽样检查通过。此测试不包含重启或读取 RT 验收。'
