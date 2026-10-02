#!/usr/bin/env bash
set -euo pipefail

OUT=${1:?usage: direct-reader-sweep.sh OUTPUT_DIR}
TPA_BIN=${TPA_BIN:-tpa}
SOURCE=${TPA_CORPUS:-bench/corpus}
BASE_PACKAGES=${TPA_BASE_PACKAGES:-}
BASE_PACKAGES_GZ=${TPA_BASE_PACKAGES_GZ:-}
mkdir -p "$OUT"
KEY_HOME=$(mktemp -d /tmp/tpa-direct-reader-gpg.XXXXXX)
chmod 700 "$KEY_HOME"
cleanup() {
  GNUPGHOME="$KEY_HOME" gpgconf --kill all >/dev/null 2>&1 || true
  rm -rf "$KEY_HOME"
}
trap cleanup EXIT
export GNUPGHOME="$KEY_HOME"
gpg --batch --pinentry-mode loopback --passphrase '' \
  --quick-generate-key 'TPA disposable direct-reader benchmark <benchmark@example.invalid>' rsa2048 sign 1y
FPR=$(gpg --with-colons --list-secret-keys | awk -F: '$1=="fpr" {print $10; exit}')
test -n "$FPR"
gpg --batch --export "$FPR" > "$OUT/apt-benchmark-keyring.gpg"
printf '%s\n' "$FPR" > "$OUT/fingerprint.txt"

for trial in 1 2 3; do
  for workers in 1 2 4 8 16; do
    root="$OUT/trial-$trial/workers-$workers"
    mkdir -p "$root"
    if [[ -x /usr/bin/time ]]; then
      /usr/bin/time -v -o "$root/time.txt" env \
        TPA_STAGE_METRICS="$root/stages.json" \
        "$TPA_BIN" pack -in="$SOURCE" -out="$root/repository" \
          -gpg="$FPR" -workers="$workers" >"$root/output.log" 2>&1
    else
      python3 bench/time-command.py "$root/time.txt" "$root/output.log" -- env \
        TPA_STAGE_METRICS="$root/stages.json" \
        "$TPA_BIN" pack -in="$SOURCE" -out="$root/repository" \
          -gpg="$FPR" -workers="$workers"
    fi
    packages="$root/repository/dists/stable/main/binary-all/Packages"
    test "$(grep -c '^Package:' "$packages")" -eq 10000
    gpg --batch --verify "$root/repository/dists/stable/InRelease" >"$root/gpg.txt" 2>&1
    if [[ -n "$BASE_PACKAGES" ]]; then
      cmp "$BASE_PACKAGES" "$packages"
    fi
    if [[ -n "$BASE_PACKAGES_GZ" ]]; then
      cmp "$BASE_PACKAGES_GZ" "$packages.gz"
    fi
  done
done

reference="$OUT/trial-1/workers-1/repository/dists/stable/main/binary-all"
for trial in 1 2 3; do
  for workers in 1 2 4 8 16; do
    candidate="$OUT/trial-$trial/workers-$workers/repository/dists/stable/main/binary-all"
    cmp "$reference/Packages" "$candidate/Packages"
    cmp "$reference/Packages.gz" "$candidate/Packages.gz"
  done
done
printf 'signed direct-reader worker sweep passed; Packages outputs match\n' > "$OUT/result.txt"
