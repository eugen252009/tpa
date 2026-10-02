#!/bin/bash
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd)
COUNT=${1:-10000}
UPDATES=${2:-100}
if [[ "$COUNT" != 10000 || "$UPDATES" != 100 ]]; then
  echo "usage: $0 [10000] [100] (qualified configuration is fixed)" >&2
  exit 2
fi
command -v docker >/dev/null || { echo "docker is required" >&2; exit 1; }
NAME="tpa-meta-bench-$$"
TPA_BIN=$(mktemp)
GEN_BIN=$(mktemp)
cleanup() {
  docker rm -f "$NAME" >/dev/null 2>&1 || true
  rm -f "$TPA_BIN" "$GEN_BIN"
}
trap cleanup EXIT

cd "$ROOT"
go build -o "$TPA_BIN" .
go build -o "$GEN_BIN" ./bench/metapackages
{
  echo "host_kernel=$(uname -srmo)"
  echo "host_go=$(go version)"
  echo "host_distro=$( . /etc/os-release; echo "$PRETTY_NAME ($VERSION_ID)")"
  echo "host_cpu=$(lscpu | grep -E 'Model name:|Socket\(s\):|Core\(s\) per socket:|Thread\(s\) per core:|CPU\(s\):' | tr '\n' ';')"
  echo "host_memory=$(awk '/MemTotal/ {printf \"%.1f GiB\", $2/1024/1024}' /proc/meminfo)"
  echo "benchmark_fs=$(stat -f -c '%T' "$ROOT/bench")"
  echo "benchmark_mount=$(df -T "$ROOT/bench" | tail -n1)"
  echo "storage_devices=$(lsblk -d -o NAME,ROTA,SIZE,MODEL | tr '\n' ';')"
  echo "docker_server=$(docker version --format '{{.Server.Version}}')"
  echo "tpa_commit=$(git rev-parse HEAD)"
} > "$ROOT/bench/host.txt"

docker run --rm -d --name "$NAME" \
  -v "$ROOT/bench:/bench" \
  -v "$TPA_BIN:/usr/local/bin/tpa:ro" \
  -v "$GEN_BIN:/usr/local/bin/metapackages:ro" \
  debian:trixie-slim sleep infinity >/dev/null
docker exec "$NAME" sh -lc \
  'apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq aptly reprepro gnupg time strace python3 >/dev/null'
docker exec "$NAME" bash /bench/benchmark-in-container.sh "$COUNT" "$UPDATES"
