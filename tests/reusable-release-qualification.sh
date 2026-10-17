#!/usr/bin/env bash
set -euo pipefail

root=$(cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"

for tool in go git dpkg dpkg-deb gzip; do
  command -v "$tool" >/dev/null || { echo "missing required qualification tool: $tool" >&2; exit 1; }
done

go test ./...
go test -race ./internal/releaseworkflow
go vet ./...
bash -n tests/release/install-package.sh

echo "Reusable Debian release workflow qualification passed."
