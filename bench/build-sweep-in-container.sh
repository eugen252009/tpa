#!/bin/bash
set -euo pipefail
OUT=${1:?result directory path required (under /bench)}
BUILDER=${BUILDER:-metapackages}
SOURCE_DATE_EPOCH=${SOURCE_DATE_EPOCH:-1700000000}
export SOURCE_DATE_EPOCH
mkdir -p "$OUT"
for workers in 1 2 4 8 16 32; do
  dir="$OUT/workers-$workers"
  mkdir -p "$dir"
  echo "build package corpus workers=$workers"
  /usr/bin/time -v -o "$dir/time.txt" "$BUILDER" \
    --count 10000 --workers "$workers" --out "$dir/packages" \
    --metrics "$dir/metrics.json" --stage-metrics "$dir/stages.json" \
    >"$dir/output.log" 2>&1
  test "$(find "$dir/packages" -maxdepth 1 -name '*.deb' | wc -l)" -eq 10000
  find "$dir/packages" -maxdepth 1 -name '*.deb' -printf '%f\n' | sort > "$dir/names.txt"
  (cd "$dir/packages" && sha256sum *.deb) > "$dir/sha256.txt"
  dpkg-deb --info "$dir/packages/tpa-meta-00000_1.0.0_all.deb" > "$dir/sample-info.txt"
done
find /bench/corpus -maxdepth 1 -name '*.deb' -printf '%f\n' | sort > "$OUT/reference-names.txt"
(cd /bench/corpus && sha256sum *.deb) > "$OUT/reference-sha256.txt"
cmp "$OUT/workers-1/names.txt" "$OUT/reference-names.txt"
cmp "$OUT/workers-1/sha256.txt" "$OUT/reference-sha256.txt"
for workers in 2 4 8 16 32; do
  cmp "$OUT/workers-1/names.txt" "$OUT/workers-$workers/names.txt"
  cmp "$OUT/workers-1/sha256.txt" "$OUT/workers-$workers/sha256.txt"
done
printf 'package build sweep passed; all 60,000 artifact SHA-256 inventories match the baseline\n' > "$OUT/result.txt"
