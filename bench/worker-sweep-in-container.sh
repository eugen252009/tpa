#!/bin/bash
set -euo pipefail
OUT=${1:?result directory path required (under /bench)}
FPR=${2:?disposable signing-key fingerprint required}
mkdir -p "$OUT"
SOURCE=/bench/corpus
TPA_BIN=${TPA_BIN:-tpa}
for workers in 1 2 4 8 16; do
  dir="$OUT/workers-$workers"
  mkdir -p "$dir"
  echo "pack workers=$workers"
  /usr/bin/time -v -o "$dir/time.txt" env \
    TPA_STAGE_METRICS="$dir/stages.json" \
    "$TPA_BIN" pack -in="$SOURCE" -out="$dir/repository" -gpg="$FPR" \
      -workers="$workers" >"$dir/output.log" 2>&1
  packages="$dir/repository/dists/stable/main/binary-all/Packages"
  gzip="$packages.gz"
  test -s "$packages" && test -s "$gzip"
  test "$(grep -c '^Package:' "$packages")" -eq 10000
  gpg --batch --verify "$dir/repository/dists/stable/InRelease" >"$dir/gpg.txt" 2>&1
done
base="$OUT/workers-1/repository/dists/stable/main/binary-all"
for workers in 2 4 8 16; do
  candidate="$OUT/workers-$workers/repository/dists/stable/main/binary-all"
  cmp "$base/Packages" "$candidate/Packages"
  cmp "$base/Packages.gz" "$candidate/Packages.gz"
done
printf 'worker sweep passed; canonical Packages indexes match\n' > "$OUT/result.txt"
