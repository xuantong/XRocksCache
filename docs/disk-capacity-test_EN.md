# Alibaba Cloud ESSD PL1 disk capacity test

The production baseline is fixed to `cloud_essd / PL1`; PL0 is not accepted. 100GiB is valid for PL1. PL2/PL3 require larger disks, so a 100GiB result cannot represent them.

Run `XRC_TEST_DIR=/data/xrc-test bash dev/disk-write-capacity-test.sh` on an isolated directory to record sequential write bandwidth and latency. The default probe writes 4GiB for 60 seconds and removes `.probe`.

On the real PL1 server run `XRC_DISK_TYPE=cloud_essd XRC_DISK_PL=pl1 XRC_DISK_CAPACITY_GIB=100 bash dev/rate-sweep-java-wsl.sh` to test 1GiB of random values at 10/20/30/35/40/50MiB/s. The script rejects PL0 and invalid capacities.

Choose the highest rate without `Write stall`, sustained L0 growth, or unacceptable GET P99; use 70–80% of that rate as the candidate and recheck under sustained target load. Automatic `/tmp/xrc_*` directories are removed on normal exit; forced termination requires manual cleanup.
