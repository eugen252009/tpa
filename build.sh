#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$ROOT"

ARCHS="amd64 riscv64 arm64"
DESC=$(cat description.txt)
VERSION=${TPA_VERSION:-1}
DEPENDS="libc6,dpkg,gpg,gzip"
HOMEPAGE="https://github.com/coffeemakerstudio/tpa"
MAINTAINER="Coffee Maker Studio <tpa@lupricht.net>"
SECTION="utils"

# The host binary is only the package-builder bootstrap; packaged binaries are
# cross-compiled below and never copied from the host build.
go build -o tpa .
mkdir -p dist

for arch in $ARCHS; do
    echo "Building for $arch..."
    work_dir="tpa-$arch"
    rm -rf "$work_dir"
    mkdir -p "$work_dir/usr/local/bin"

    ./tpa init \
        -name=tpa -ver="$VERSION" -depends="$DEPENDS" \
        -desc="$DESC" -homepage="$HOMEPAGE" -maintainer="$MAINTAINER" \
        -section="$SECTION" -out="$work_dir" -arch="$arch"

    CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -o "$work_dir/usr/local/bin/tpa" .
    chmod 0755 "$work_dir/usr/local/bin/tpa"
    if [ -f manpage/usr/share/man/man1/tpa.1 ]; then
        mkdir -p "$work_dir/usr/share/man/man1"
        gzip -cn manpage/usr/share/man/man1/tpa.1 >"$work_dir/usr/share/man/man1/tpa.1.gz"
        chmod 0644 "$work_dir/usr/share/man/man1/tpa.1.gz"
    fi
    ./tpa build -in="$work_dir" -out="dist/tpa-$arch.deb"
    rm -rf "$work_dir"
done

rm -f tpa
echo "Building done"
