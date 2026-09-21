#!/usr/bin/env bash
set -euo pipefail

# 阿里云磁盘能力探测：只使用指定测试目录，退出时删除临时文件。
# 用法：XRC_TEST_DIR=/data/xrc-test bash dev/disk-write-capacity-test.sh
TEST_DIR="${XRC_TEST_DIR:-$(mktemp -d /tmp/xrc_disk_probe.XXXXXX)}"
KEEP="${XRC_KEEP_TEST_DATA:-0}"
SIZE="${XRC_PROBE_SIZE:-4G}"
BLOCK="${XRC_PROBE_BLOCK:-1M}"
RESULT="${XRC_PROBE_RESULT:-${PWD}/benchmark/results/disk-probe-$(date +%Y%m%d-%H%M%S)}"
mkdir -p "${TEST_DIR}" "${RESULT}"
cleanup() {
  if [[ "${KEEP}" != 1 && "${TEST_DIR}" == /tmp/xrc_disk_probe.* ]]; then rm -rf -- "$(realpath -e "${TEST_DIR}")"; fi
}
trap cleanup EXIT

command -v fio >/dev/null || { echo '需要安装 fio（Ubuntu: apt-get install fio）' >&2; exit 1; }
command -v df >/dev/null || exit 1
df -h "${TEST_DIR}" | tee "${RESULT}/filesystem.txt"
fio --name=xrc-sequential-write --directory="${TEST_DIR}" --filename=.probe --size="${SIZE}" --bs="${BLOCK}" --rw=write --direct=1 --iodepth=16 --ioengine=libaio --runtime="${XRC_PROBE_RUNTIME:-60}" --time_based=1 --group_reporting=1 --output-format=json --output="${RESULT}/fio.json"
fio --name=xrc-sequential-read --directory="${TEST_DIR}" --filename=.probe --size="${SIZE}" --bs="${BLOCK}" --rw=read --direct=1 --iodepth=16 --ioengine=libaio --runtime="${XRC_PROBE_RUNTIME:-60}" --time_based=1 --group_reporting=1 --output-format=json --output="${RESULT}/fio-read.json"
python3 - "${RESULT}/fio.json" "${RESULT}/fio-read.json" <<'PY'
import json,sys
for path in sys.argv[1:]:
 d=json.load(open(path)); j=d['jobs'][0]; x=j['write'] if j['job options']['rw']=='write' else j['read']
 print(path, 'bw_mib_s=%.2f iops=%.0f lat_p99_us=%.0f' % (x['bw']/1024, x['iops'], x['clat_ns']['percentile']['99.000000']/1000))
PY
rm -f -- "${TEST_DIR}/.probe"
echo "结果已保存：${RESULT}"
