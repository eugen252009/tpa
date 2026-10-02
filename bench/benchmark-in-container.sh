#!/bin/bash
set -euo pipefail
COUNT=${1:-10000}
UPDATES=${2:-100}
BENCH=/bench
RUNS="$BENCH/results"
CORPUS="$BENCH/corpus"
mkdir -p "$RUNS"
STAMP=$(date -u +%Y%m%dT%H%M%SZ)
OUT="$RUNS/$STAMP"
mkdir -p "$OUT"
exec > >(tee "$OUT/driver.log") 2>&1

if [[ "$COUNT" -ne 10000 || "$UPDATES" -ne 100 ]]; then
  echo "This comparison is qualified for --count 10000 --updates 100." >&2
  exit 2
fi
export SOURCE_DATE_EPOCH=1700000000
export GNUPGHOME=/tmp/tpa-bench-gnupg
mkdir -p "$GNUPGHOME"; chmod 700 "$GNUPGHOME"

{
  echo "timestamp_utc=$STAMP"
  echo "kernel=$(uname -srmo)"
  echo "distro=$( . /etc/os-release; echo "$PRETTY_NAME ($VERSION_ID)")"
  echo "aptly=$(aptly version)"
  echo "reprepro=$(reprepro --version 2>&1 | head -n1)"
  echo "dpkg-deb=$(dpkg-deb --version | head -n1)"
  echo "gpg=$(gpg --version | head -n1)"
  echo "filesystem=$(stat -f -c %T "$BENCH")"
  echo "mount=$(df -T "$BENCH" | tail -n1)"
  echo "cpu=$(nproc) logical CPUs visible in container"
  echo "memory=$(awk '/MemTotal/ {print $2 " kB"}' /proc/meminfo)"
} > "$OUT/software-host.txt"

# Warm up process/filesystem paths without retaining those artifacts.
rm -rf /tmp/tpa-meta-warmup
/usr/local/bin/metapackages --count 100 --out /tmp/tpa-meta-warmup >/dev/null 2>&1
rm -rf /tmp/tpa-meta-warmup
mkdir -p "$OUT/package-build"

# Three independently constructed corpora. Trial 1 is retained as the common
# immutable input; trials 2-3 are removed after recording their measurements.
for trial in 1 2 3; do
  if [[ $trial -eq 1 ]]; then dest="$CORPUS"; else dest="$OUT/package-build/trial-$trial"; fi
  rm -rf "$dest"; mkdir -p "$dest"
  echo "package construction measured trial=$trial"
  /usr/bin/time -v -o "$OUT/package-build/trial-$trial.time" \
    /usr/local/bin/metapackages --count "$COUNT" --out "$dest" \
      --metrics "$OUT/package-build/trial-$trial.json" >/dev/null
done
rm -rf "$OUT/package-build/trial-2" "$OUT/package-build/trial-3"

# Validate every archive with dpkg-deb; save detailed sample inspections.
actual=$(find "$CORPUS" -maxdepth 1 -type f -name '*.deb' | wc -l)
test "$actual" -eq "$COUNT"
find "$CORPUS" -maxdepth 1 -type f -name '*.deb' -print0 | while IFS= read -r -d '' f; do
  dpkg-deb --info "$f" >/dev/null
done
for i in 00000 00001 05000 09999; do
  dpkg-deb --info "$CORPUS/tpa-meta-$i"_1.0.0_all.deb > "$OUT/sample-$i.info"
  dpkg-deb --contents "$CORPUS/tpa-meta-$i"_1.0.0_all.deb > "$OUT/sample-$i.contents"
done
find "$CORPUS" -maxdepth 1 -name '*.deb' -printf '%f\n' | sort > "$OUT/corpus-filenames.txt"
awk -F_ '{print $1}' "$OUT/corpus-filenames.txt" | sort -u | wc -l > "$OUT/corpus-unique-names.txt"
du -sb "$CORPUS" > "$OUT/corpus-du.txt"
du -sb --apparent-size "$CORPUS" > "$OUT/corpus-apparent.txt"
chmod 0444 "$CORPUS"/*.deb
chmod 0555 "$CORPUS"

# One disposable signing key. Its private material stays in this container.
gpg --batch --pinentry-mode loopback --passphrase '' \
  --quick-generate-key 'TPA disposable benchmark <benchmark@example.invalid>' rsa2048 sign 1y
FPR=$(gpg --with-colons --list-secret-keys | awk -F: '$1=="fpr" {print $10; exit}')
test -n "$FPR"
echo "$FPR" > "$OUT/fingerprint.txt"
gpg --batch --export "$FPR" > "$OUT/apt-benchmark-keyring.gpg"

# The current-state update contains 9,900 hardlinked unchanged .debs plus 100
# separately built v1.0.1 artifacts (10,000 files total).
mkdir -p "$OUT/package-build/updates"
/usr/bin/time -v -o "$OUT/package-build/updates.time" \
  /usr/local/bin/metapackages --count "$UPDATES" --version 1.0.1 \
    --out "$OUT/package-build/updates" --metrics "$OUT/package-build/updates.json" >/dev/null
UPDATE_INPUT="$OUT/update-input"
mkdir -p "$UPDATE_INPUT"
for f in "$CORPUS"/*.deb; do
  base=$(basename "$f"); idx=${base%%_*}; idx=${idx##*-}
  if (( 10#$idx < UPDATES )); then continue; fi
  ln "$f" "$UPDATE_INPUT/$base"
done
cp "$OUT/package-build/updates"/*.deb "$UPDATE_INPUT/"
test "$(find "$UPDATE_INPUT" -maxdepth 1 -type f -name '*.deb' | wc -l)" -eq "$COUNT"
chmod 0444 "$UPDATE_INPUT"/*.deb
chmod 0555 "$UPDATE_INPUT"

# One full-corpus warm-up per repository workflow; these roots are outside the
# measured output tree and are removed before measurement.
rm -rf /tmp/tpa-repo-warmup /tmp/tpa-aptly-warmup /tmp/tpa-reprepro-warmup
/usr/local/bin/tpa pack -in="$CORPUS" -out=/tmp/tpa-repo-warmup \
  -gpg="$FPR" >/dev/null
mkdir -p /tmp/tpa-aptly-warmup/home
HOME=/tmp/tpa-aptly-warmup/home aptly repo create -distribution=stable -component=main meta-bench >/dev/null
HOME=/tmp/tpa-aptly-warmup/home aptly repo add meta-bench "$CORPUS" >/dev/null
HOME=/tmp/tpa-aptly-warmup/home aptly snapshot create meta-bench-v1 from repo meta-bench >/dev/null
HOME=/tmp/tpa-aptly-warmup/home aptly publish snapshot -batch -gpg-key="$FPR" \
  -architectures=amd64 -distribution=stable -component=main meta-bench-v1 >/dev/null
mkdir -p /tmp/tpa-reprepro-warmup/conf
printf 'Origin: TPA Benchmark\nLabel: TPA Benchmark\nCodename: stable\nArchitectures: amd64 source\nComponents: main\nSignWith: %s\nDescription: warm-up\n' "$FPR" > /tmp/tpa-reprepro-warmup/conf/distributions
reprepro -b /tmp/tpa-reprepro-warmup includedeb stable "$CORPUS"/*.deb >/dev/null
rm -rf /tmp/tpa-repo-warmup /tmp/tpa-aptly-warmup /tmp/tpa-reprepro-warmup

validate_repo() {
  local label=$1 repo=$2 expected=$3 package=$4 version=$5
  local packages
  packages=$(find "$repo/dists/stable" -type f -name Packages | head -n1)
  test -n "$packages"
  test "$(grep -c '^Package:' "$packages")" -eq "$expected"
  test -s "$repo/dists/stable/InRelease"
  gpg --batch --verify "$repo/dists/stable/InRelease" >"$OUT/validate-$label-gpg.txt" 2>&1
  local lists="$OUT/apt-lists/$label"
  mkdir -p "$lists/partial"
  printf 'deb [signed-by=%s] file:%s stable main\n' "$OUT/apt-benchmark-keyring.gpg" "$repo" > "$OUT/$label.sources.list"
  apt-get -o Dir::Etc::sourcelist="$OUT/$label.sources.list" \
    -o Dir::Etc::sourceparts=- -o Dir::State::lists="$lists" \
    -o Acquire::Languages=none update >"$OUT/validate-$label-apt.txt" 2>&1
  local download="$OUT/downloads/$label"
  mkdir -p "$download"
  (cd "$download" && apt-get -o Dir::Etc::sourcelist="$OUT/$label.sources.list" \
    -o Dir::Etc::sourceparts=- -o Dir::State::lists="$lists" \
    download "$package=$version") >"$OUT/validate-$label-download.txt" 2>&1
}

for trial in 1 2 3; do
  for tool in tpa aptly reprepro; do
    root="$OUT/initial/$tool/trial-$trial"
    mkdir -p "$root"
    echo "initial repository tool=$tool trial=$trial"
    case "$tool" in
      tpa)
        /usr/bin/time -v -o "$root/time.txt" \
          /usr/local/bin/tpa pack -in="$CORPUS" -out="$root/repository" -gpg="$FPR" \
          >"$root/output.log" 2>&1
        ;;
      aptly)
        /usr/bin/time -v -o "$root/time.txt" bash -c '
          set -e; mkdir -p "$1"; export HOME="$1";
          aptly repo create -distribution=stable -component=main meta-bench;
          aptly repo add meta-bench "$3";
          aptly snapshot create meta-bench-v1 from repo meta-bench;
          aptly publish snapshot -batch -gpg-key="$2" -architectures=amd64 \
            -distribution=stable -component=main meta-bench-v1
        ' _ "$root/home" "$FPR" "$CORPUS" >"$root/output.log" 2>&1
        ;;
      reprepro)
        /usr/bin/time -v -o "$root/time.txt" bash -c '
          set -e; root="$1"; fpr="$2"; corpus="$3";
          mkdir -p "$root/conf";
          printf "Origin: TPA Benchmark\nLabel: TPA Benchmark\nCodename: stable\nArchitectures: amd64 source\nComponents: main\nSignWith: %s\nDescription: TPA generated 10k meta-package benchmark\n" "$fpr" > "$root/conf/distributions";
          reprepro -b "$root" includedeb stable "$corpus"/*.deb
        ' _ "$root/repository" "$FPR" "$CORPUS" >"$root/output.log" 2>&1
        ;;
    esac
  done
done

# Validate initial repositories before updates change aptly/reprepro's active
# generation. Also capture their initial storage usage.
for trial in 1 2 3; do
  validate_repo "tpa-v1-$trial" "$OUT/initial/tpa/trial-$trial/repository" 10000 tpa-meta-05000 1.0.0
  validate_repo "aptly-v1-$trial" "$OUT/initial/aptly/trial-$trial/home/.aptly/public" 10000 tpa-meta-05000 1.0.0
  validate_repo "reprepro-v1-$trial" "$OUT/initial/reprepro/trial-$trial/repository" 10000 tpa-meta-05000 1.0.0
  du -s -B1 "$OUT/initial/aptly/trial-$trial/home/.aptly" > "$OUT/storage-initial-aptly-$trial.txt"
  du -s --apparent-size -B1 "$OUT/initial/aptly/trial-$trial/home/.aptly" >> "$OUT/storage-initial-aptly-$trial.txt"
  du -s -B1 "$OUT/initial/reprepro/trial-$trial/repository" > "$OUT/storage-initial-reprepro-$trial.txt"
  du -s --apparent-size -B1 "$OUT/initial/reprepro/trial-$trial/repository" >> "$OUT/storage-initial-reprepro-$trial.txt"
done

# Controlled runtime scaling, one observation per GOMAXPROCS value. Pack's
# default bounded worker count follows GOMAXPROCS; this tests that default path.
for procs in 1 2 4 8 16; do
  root="$OUT/scaling/gomaxprocs-$procs"
  mkdir -p "$root"
  echo "TPA CPU scaling GOMAXPROCS=$procs"
  /usr/bin/time -v -o "$root/time.txt" env GOMAXPROCS="$procs" \
    /usr/local/bin/tpa pack -in="$CORPUS" -out="$root/repository" -gpg="$FPR" \
    >"$root/output.log" 2>&1
done

# Updates use the three independent initial repositories above. TPA receives a
# complete current-state set; aptly ingests 100 new versions and retains old
# versions in the new snapshot, while reprepro replaces the active package
# version for those names.
for trial in 1 2 3; do
  root="$OUT/update/tpa/trial-$trial"; mkdir -p "$root"
  echo "update tool=tpa trial=$trial"
  /usr/bin/time -v -o "$root/time.txt" \
    /usr/local/bin/tpa pack -in="$UPDATE_INPUT" -out="$root/repository" -gpg="$FPR" \
    >"$root/output.log" 2>&1

  home="$OUT/initial/aptly/trial-$trial/home"
  root="$OUT/update/aptly/trial-$trial"; mkdir -p "$root"
  echo "update tool=aptly trial=$trial"
  /usr/bin/time -v -o "$root/time.txt" bash -c '
    set -e; export HOME="$1";
    aptly repo add meta-bench "$2";
    aptly snapshot create meta-bench-v2 from repo meta-bench;
    aptly publish switch -batch -gpg-key="$3" -architectures=amd64 \
      -component=main stable meta-bench-v2
  ' _ "$home" "$OUT/package-build/updates" "$FPR" >"$root/output.log" 2>&1

  root="$OUT/update/reprepro/trial-$trial"; mkdir -p "$root"
  echo "update tool=reprepro trial=$trial"
  /usr/bin/time -v -o "$root/time.txt" bash -c '
    set -e; reprepro -b "$1" includedeb stable "$2"/*.deb
  ' _ "$OUT/initial/reprepro/trial-$trial/repository" "$OUT/package-build/updates" \
    >"$root/output.log" 2>&1
done

validate_repo() {
  local label=$1 repo=$2 expected=$3
  local packages
  packages=$(find "$repo/dists/stable" -type f -name Packages | head -n1)
  test -n "$packages"
  test "$(grep -c '^Package:' "$packages")" -eq "$expected"
  test -s "$repo/dists/stable/InRelease"
  gpg --batch --verify "$repo/dists/stable/InRelease" >"$OUT/validate-$label-gpg.txt" 2>&1
  local lists="$OUT/apt-lists/$label"
  mkdir -p "$lists/partial"
  printf 'deb [signed-by=%s] file:%s stable main\n' "$OUT/apt-benchmark-keyring.gpg" "$repo" > "$OUT/$label.sources.list"
  apt-get -o Dir::Etc::sourcelist="$OUT/$label.sources.list" \
    -o Dir::Etc::sourceparts=- -o Dir::State::lists="$lists" \
    -o Acquire::Languages=none update >"$OUT/validate-$label-apt.txt" 2>&1
  local download="$OUT/downloads/$label"
  mkdir -p "$download"
  (cd "$download" && apt-get -o Dir::Etc::sourcelist="$OUT/$label.sources.list" \
    -o Dir::Etc::sourceparts=- -o Dir::State::lists="$lists" \
    download "tpa-meta-05000=1.0.0") >"$OUT/validate-$label-download.txt" 2>&1
}
for trial in 1 2 3; do
  validate_repo "tpa-v2-$trial" "$OUT/update/tpa/trial-$trial/repository" 10000
  validate_repo "aptly-v2-$trial" "$OUT/initial/aptly/trial-$trial/home/.aptly/public" 10100
  validate_repo "reprepro-v2-$trial" "$OUT/initial/reprepro/trial-$trial/repository" 10000
done

# Tool storage excludes the shared source corpus, which is reported separately.
for trial in 1 2 3; do
  for tool in tpa aptly reprepro; do
    case "$tool" in
      tpa) initial="$OUT/initial/tpa/trial-$trial/repository"; update="$OUT/update/tpa/trial-$trial/repository" ;;
      aptly) initial="$OUT/initial/aptly/trial-$trial/home/.aptly"; update="$initial" ;;
      reprepro) initial="$OUT/initial/reprepro/trial-$trial/repository"; update="$initial" ;;
    esac
    {
      echo "tool=$tool trial=$trial"
      echo "final_allocated_bytes:"; du -s -B1 "$initial"
      echo "final_apparent_bytes:"; du -s --apparent-size -B1 "$initial"
      find "$initial" -type f -printf '%s\n' | awk '{s+=$1} END {print "sum_file_sizes=" s}'
      if [[ "$tool" == tpa ]]; then
        echo "update_allocated:"; du -sb "$update"
        echo "update_apparent:"; du -sb --apparent-size "$update"
      fi
    } > "$OUT/storage-final-$tool-$trial.txt"
  done
done

python3 /bench/compare-generations.py \
  "$OUT/initial/tpa/trial-1/repository" "$OUT/update/tpa/trial-1/repository" \
  > "$OUT/whole-file-reuse.txt"

# Exact package-entry counts and index paths after initial and update.
{
  for trial in 1 2 3; do
    for tool in tpa aptly reprepro; do
      case "$tool" in
        tpa) repo="$OUT/initial/tpa/trial-$trial/repository";;
        aptly) repo="$OUT/initial/aptly/trial-$trial/home/.aptly/public";;
        reprepro) repo="$OUT/initial/reprepro/trial-$trial/repository";;
      esac
      printf '%s trial=%s active_entries_after_update=' "$tool" "$trial"
      case "$tool" in
        tpa) repo="$OUT/update/tpa/trial-$trial/repository";;
      esac
      grep -h '^Package:' $(find "$repo/dists/stable" -type f -name Packages) | wc -l
    done
  done
} > "$OUT/index-counts.txt"

# Capability check (not timed): record manifest behavior for the 10k corpus.
mkdir -p "$OUT/manifest-limit"
set +e
/usr/local/bin/tpa pack -in="$CORPUS" -out="$OUT/manifest-limit/repository" \
  -gpg="$FPR" -generation-manifest="$OUT/manifest-limit/generation.json" \
  -repository-id=benchmark -generation-id=initial >"$OUT/manifest-limit/output.log" 2>&1
manifest_status=$?
set -e
echo "$manifest_status" > "$OUT/manifest-limit/exit-status.txt"
chmod -R a+rX "$OUT"
echo "completed=$OUT"
