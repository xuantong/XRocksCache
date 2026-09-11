# XRocksCache

XRocksCache is a lightweight single-node K/V cache service rewritten in Go for low-cost 2C4G / 4C8G cloud servers. It keeps the most common Redis RESP string commands and enforces the product boundaries: maximum key size 512KiB, maximum value size 1MiB, and maximum write TTL 15 days.

The current server and benchmark tool use only the Go standard library, so the source tree keeps only the root Apache License 2.0 file.
Runtime logs use Go standard-library `log/slog` and can be emitted in `text` or `json` format. With `log-dir <directory>`, logs rotate daily as `xrockscache-YYYY-MM-DD.log` and old files are cleaned by `log-retention-days`.

Build:

```bash
go build -trimpath -o build/xrockscache ./cmd/xrockscache
```

The main module path is `xrockscache`, and internal packages use `xrockscache/internal/...`. This keeps the code mirror-friendly across both Gitee and GitHub without binding the module identity to one remote repository.

Run:

```bash
./build/xrockscache -c xrockscache.conf
```

Test:

```bash
go test ./...
```

The minimal C++ baseline before the Go rewrite is preserved in the local Git tag:

```bash
git checkout release_tag_cpp_baseline_20260907
```

See `README.md` for the bilingual Chinese/English project overview.
