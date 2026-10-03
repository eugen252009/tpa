#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
VERSION=9.8.7~test1
HOST_ARCH=$(go env GOARCH)
case "$HOST_ARCH" in
    amd64|arm64|riscv64) ;;
    *) echo "version qualification requires an amd64, arm64, or riscv64 Go host" >&2; exit 2 ;;
esac
for tool in go dpkg dpkg-deb tar; do
    command -v "$tool" >/dev/null 2>&1 || { echo "missing required tool: $tool" >&2; exit 2; }
done

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT HUP INT TERM
mkdir -p "$TMP/source"
tar -C "$ROOT" \
    --exclude='./.git' --exclude='./.cocoindex_code' --exclude='./bench' \
    --exclude='./dist' --exclude='./tpa' \
    --exclude='./tpa-amd64' --exclude='./tpa-arm64' --exclude='./tpa-riscv64' \
    -cf - . | tar -C "$TMP/source" -xf -

(
    cd "$TMP/source"
    TPA_VERSION="$VERSION" ./build.sh
)

for arch in amd64 arm64 riscv64; do
    deb="$TMP/source/dist/tpa-$arch.deb"
    test -f "$deb"
    package_version=$(dpkg-deb -f "$deb" Version)
    provenance_version=$(dpkg-deb -f "$deb" TPA-Version)
    maintainer=$(dpkg-deb -f "$deb" Maintainer)
    homepage=$(dpkg-deb -f "$deb" Homepage)
    test "$package_version" = "$VERSION"
    test "$provenance_version" = "$VERSION"
    test "$maintainer" = 'TPA Project <tpa@lupricht.net>'
    test "$homepage" = 'https://github.com/eugen252009/tpa'
    printf '%s: Debian Version=%s TPA-Version=%s\n' "$arch" "$package_version" "$provenance_version"
    if [ "$arch" = "$HOST_ARCH" ]; then
        extracted="$TMP/extracted-$arch"
        mkdir -p "$extracted"
        dpkg-deb -x "$deb" "$extracted"
        binary="$extracted/usr/local/bin/tpa"
        test "$("$binary" version)" = "$VERSION"
        test "$("$binary" --version)" = "tpa $VERSION"
        help=$("$binary" --help)
        printf '%s\n' "$help" | grep -Fqx "TPA $VERSION - Tool for Package Automation"
        printf '%s\n' "$help" | grep -Fq 'https://github.com/eugen252009/tpa'
        printf '%s\n' "$help" | grep -Fq 'tpa@lupricht.net'
        echo "runtime/help surfaces match package version for $arch"
    fi
done
