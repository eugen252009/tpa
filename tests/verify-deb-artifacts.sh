#!/bin/sh
# Verify the Debian packages emitted by build.sh and qualify them through a
# disposable local repository. This script never publishes a repository.
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
if [ "$#" -ne 3 ]; then
    echo "usage: $0 VERSION BUILD_DIR ARTIFACT_DIR" >&2
    exit 2
fi
VERSION=$1
BUILD_DIR=$2
ARTIFACT_DIR=$3
cd "$ROOT"

for tool in dpkg dpkg-deb sha256sum python3; do
    command -v "$tool" >/dev/null 2>&1 || { echo "missing required tool: $tool" >&2; exit 2; }
done
if ! dpkg --validate-version "$VERSION" >/dev/null 2>&1; then
    echo "invalid Debian package version: $VERSION" >&2
    exit 2
fi

if [ -e "$ARTIFACT_DIR" ]; then
    if [ ! -d "$ARTIFACT_DIR" ] || find "$ARTIFACT_DIR" -mindepth 1 -maxdepth 1 -print -quit | grep -q .; then
        echo "artifact output directory must be absent or empty: $ARTIFACT_DIR" >&2
        exit 2
    fi
else
    mkdir -p "$ARTIFACT_DIR"
fi

TMP=$(mktemp -d)
cleanup() {
    rm -rf "$TMP"
}
trap cleanup EXIT HUP INT TERM

ARCHS="amd64 arm64 riscv64"
ARTIFACTS=
NATIVE_ARCH=$(dpkg --print-architecture)
NATIVE_BINARY=
for arch in $ARCHS; do
    source="$BUILD_DIR/tpa-$arch.deb"
    filename="tpa_${VERSION}_${arch}.deb"
    artifact="$ARTIFACT_DIR/$filename"
    if [ ! -f "$source" ]; then
        echo "missing build output: $source" >&2
        exit 1
    fi
    cp -- "$source" "$artifact"
    chmod 0644 "$artifact"

    check_field() {
        field=$1
        expected=$2
        actual=$(dpkg-deb -f "$artifact" "$field")
        if [ "$actual" != "$expected" ]; then
            echo "$filename: $field is '$actual', expected '$expected'" >&2
            exit 1
        fi
    }
    check_field Package tpa
    check_field Version "$VERSION"
    check_field Architecture "$arch"
    check_field Maintainer 'TPA Project <tpa@lupricht.net>'
    check_field Homepage 'https://github.com/eugen252009/tpa'
    check_field TPA-Version "$VERSION"

    if [ "$arch" = "$NATIVE_ARCH" ]; then
        native_root="$TMP/native-$arch"
        mkdir -p "$native_root"
        dpkg-deb -x "$artifact" "$native_root"
        NATIVE_BINARY="$native_root/usr/local/bin/tpa"
        test -x "$NATIVE_BINARY"
        if [ "$arch" = amd64 ]; then
            test "$("$NATIVE_BINARY" version)" = "$VERSION"
            test "$("$NATIVE_BINARY" --version)" = "tpa $VERSION"
            help=$("$NATIVE_BINARY" --help)
            printf '%s\n' "$help" | grep -Fqx "TPA $VERSION - Tool for Package Automation"
            printf '%s\n' "$help" | grep -Fq 'https://github.com/eugen252009/tpa'
            printf '%s\n' "$help" | grep -Fq 'tpa@lupricht.net'
        fi
    fi
    ARTIFACTS="$ARTIFACTS $filename"
done

if [ -z "$NATIVE_BINARY" ]; then
    echo "no package matching native Debian architecture $NATIVE_ARCH" >&2
    exit 1
fi

(
    cd "$ARTIFACT_DIR"
    # ARTIFACTS contains only the fixed, dpkg-validated version and architectures.
    # shellcheck disable=SC2086
    sha256sum $ARTIFACTS > SHA256SUMS.txt
    sha256sum --check SHA256SUMS.txt
)

# Reconstruct a disposable local repository from the verified package set,
# then confirm repository metadata and browser views against authoritative APT
# indexes and package bytes. TMP is always removed on exit.
mkdir "$TMP/packages"
for filename in $ARTIFACTS; do
    cp -- "$ARTIFACT_DIR/$filename" "$TMP/packages/$filename"
done
"$NATIVE_BINARY" pack -in="$TMP/packages" -out="$TMP/repository" \
    -origin=TPA-CI-Qualification -label=TPA-CI-Qualification \
    -codename=stable -components=main >/dev/null

"$NATIVE_BINARY" inspect --json -codename=stable "$TMP/repository" > "$TMP/inspect.json"
"$NATIVE_BINARY" verify --json -codename=stable "$TMP/repository" > "$TMP/verify.json"
python3 - "$TMP/repository" "$TMP/inspect.json" "$TMP/verify.json" "$VERSION" <<'PY'
import hashlib
import json
import sys
from html.parser import HTMLParser
from pathlib import Path

root = Path(sys.argv[1])
inspect = json.loads(Path(sys.argv[2]).read_text())
verify = json.loads(Path(sys.argv[3]).read_text())
version = sys.argv[4]
index = json.loads((root / "repository.json").read_text())
assert inspect["packageCount"] == 3, inspect
assert verify["valid"] is True and verify["packageCount"] == 3, verify
assert index["format"] == "tpa-repository-index" and index["version"] == 1
assert len(index["packages"]) == 3
expected_arches = {"amd64", "arm64", "riscv64"}
assert {entry["metadata"]["Architecture"] for entry in index["packages"]} == expected_arches
for entry in index["packages"]:
    metadata = entry["metadata"]
    artifact = entry["artifact"]
    assert metadata["Package"] == "tpa" and metadata["Version"] == version
    data = (root / artifact["filename"]).read_bytes()
    assert artifact["size"] == len(data)
    assert artifact["sha256"] == hashlib.sha256(data).hexdigest()

class Links(HTMLParser):
    def __init__(self):
        super().__init__()
        self.hrefs = set()
    def handle_starttag(self, tag, attrs):
        if tag == "a":
            href = dict(attrs).get("href")
            if href:
                self.hrefs.add(href)

html = (root / "index.html").read_text()
parser = Links()
parser.feed(html)
expected_links = {entry["artifact"]["filename"] for entry in index["packages"]}
assert {href for href in parser.hrefs if href.endswith(".deb")} == expected_links
print("local repository indexes and browser views match all package artifacts")
PY

# Neither sidecar is repository authority: corruption or absence must not
# change inspect/verify results.
for sidecar in repository.json index.html; do
    cp "$TMP/repository/$sidecar" "$TMP/$sidecar.saved"
    printf 'corrupt sidecar\n' > "$TMP/repository/$sidecar"
    "$NATIVE_BINARY" inspect --json -codename=stable "$TMP/repository" > "$TMP/sidecar-inspect.json"
    "$NATIVE_BINARY" verify --json -codename=stable "$TMP/repository" > "$TMP/sidecar-verify.json"
    python3 - "$TMP/sidecar-inspect.json" "$TMP/sidecar-verify.json" <<'PY'
import json, sys
inspect = json.load(open(sys.argv[1]))
verify = json.load(open(sys.argv[2]))
assert inspect["packageCount"] == 3
assert verify["valid"] is True and verify["packageCount"] == 3
PY
    rm "$TMP/repository/$sidecar"
    "$NATIVE_BINARY" verify --json -codename=stable "$TMP/repository" > "$TMP/missing-sidecar-verify.json"
    python3 - "$TMP/missing-sidecar-verify.json" <<'PY'
import json, sys
verify = json.load(open(sys.argv[1]))
assert verify["valid"] is True and verify["packageCount"] == 3
PY
    mv "$TMP/$sidecar.saved" "$TMP/repository/$sidecar"
done

echo "qualified $VERSION packages, checksums, native runtime, and disposable local repository"
