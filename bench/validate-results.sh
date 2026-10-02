#!/bin/bash
# Revalidate a completed/partially completed result tree in the Debian tool
# container. No timings are collected by this script.
set -euo pipefail
OUT=${1:?usage: validate-results.sh /bench/results/<run>}
export GNUPGHOME=/tmp/tpa-validation-gnupg
mkdir -p "$GNUPGHOME"; chmod 700 "$GNUPGHOME"
gpg --batch --import "$OUT/apt-benchmark-keyring.gpg" >/dev/null 2>&1

validate() {
  local label=$1 repo=$2 expected=$3 package=$4 version=$5
  local index
  index=$(find "$repo/dists/stable" -type f -name Packages | head -n1)
  test -n "$index"
  test "$(grep -c '^Package:' "$index")" -eq "$expected"
  gpg --batch --verify "$repo/dists/stable/InRelease" >"$OUT/recheck-$label-gpg.txt" 2>&1
  local lists="$OUT/recheck-lists/$label"
  mkdir -p "$lists/partial"
  printf 'deb [signed-by=%s] file:%s stable main\n' "$OUT/apt-benchmark-keyring.gpg" "$repo" > "$OUT/recheck-$label.list"
  apt-get -o Dir::Etc::sourcelist="$OUT/recheck-$label.list" \
    -o Dir::Etc::sourceparts=- -o Dir::State::lists="$lists" \
    -o Acquire::Languages=none update >"$OUT/recheck-$label-apt.txt" 2>&1
  mkdir -p "$OUT/recheck-downloads/$label"
  (cd "$OUT/recheck-downloads/$label" && apt-get \
    -o Dir::Etc::sourcelist="$OUT/recheck-$label.list" \
    -o Dir::Etc::sourceparts=- -o Dir::State::lists="$lists" \
    download "$package=$version") >"$OUT/recheck-$label-download.txt" 2>&1
}

for trial in 1 2 3; do
  validate "tpa-v1-$trial" "$OUT/initial/tpa/trial-$trial/repository" 10000 tpa-meta-05000 1.0.0
  validate "tpa-v2-$trial" "$OUT/update/tpa/trial-$trial/repository" 10000 tpa-meta-00050 1.0.1
  validate "aptly-v2-$trial" "$OUT/initial/aptly/trial-$trial/home/.aptly/public" 10100 tpa-meta-00050 1.0.1
  validate "reprepro-v2-$trial" "$OUT/initial/reprepro/trial-$trial/repository" 10000 tpa-meta-00050 1.0.1
done

# Generation inventory is now supported beyond 10,000 pool files. This
# correctness-only run checks the complete emitted manifest, outside timings.
mkdir -p "$OUT/manifest-scale"
tpa pack -in=/bench/corpus -out="$OUT/manifest-scale/repository" \
  -generation-manifest="$OUT/manifest-scale/generation.json" \
  -repository-id=benchmark -generation-id=initial \
  >"$OUT/manifest-scale/output.log" 2>&1
test "$(grep -c '^[[:space:]]*"path":' "$OUT/manifest-scale/generation.json")" -eq 10004
test "$(wc -c < "$OUT/manifest-scale/generation.json")" -le $((16 * 1024 * 1024))
echo "all requested rechecks passed: $OUT"
