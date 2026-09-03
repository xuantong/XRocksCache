#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "${script_dir}"
mkdir -p bin
go build -trimpath -o bin/xrcbench ./xrcbench
"${script_dir}/bin/xrcbench" version
