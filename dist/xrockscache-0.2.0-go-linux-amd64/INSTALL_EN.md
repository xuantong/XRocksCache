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
