#!/bin/sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$ROOT"
ARCHS="amd64 riscv64 arm64"
DESC=$(cat description.txt)
# SOURCE_DATE_EPOCH is optional: dynamic provenance remains the ordinary default.
VERSION=${TPA_VERSION:-0.0.0~dev}
if ! dpkg --validate-version "$VERSION" >/dev/null 2>&1; then
    echo "Invalid Debian version in TPA_VERSION: $VERSION" >&2
    exit 1
fi
SOURCE_COMMIT=${TPA_SOURCE_COMMIT:-$(git rev-parse HEAD 2>/dev/null || :)}
if [ -n "$SOURCE_COMMIT" ]; then
    case "$SOURCE_COMMIT" in *[!0-9a-f]* ) echo 'invalid source commit' >&2; exit 1 ;; esac
    [ "${#SOURCE_COMMIT}" -eq 40 ] || { echo 'source commit must be full SHA-1' >&2; exit 1; }
fi
LDFLAGS="-X github.com/eugen252009/tpa/internal/version.Version=$VERSION"
DEPENDS="libc6,dpkg,gpg,gzip,openssh-client"
HOMEPAGE="https://github.com/eugen252009/tpa"
MAINTAINER="TPA Project <tpa@lupricht.net>"
SECTION="utils"
# Bootstrap uses THIS source's ordinary TPA init/build; never an ambient install.
CGO_ENABLED=0 go build -trimpath -buildvcs=false -p="${TPA_BUILD_JOBS:-4}" -ldflags "$LDFLAGS" -o tpa .
mkdir -p dist
for arch in $ARCHS; do
    echo "Building for $arch..."
    work_dir="tpa-$arch"
    rm -rf "$work_dir"
    mkdir -p "$work_dir"
    ./tpa init -name=tpa -ver="$VERSION" -depends="$DEPENDS" -recommends=bash-completion \
        -desc="$DESC" -homepage="$HOMEPAGE" -maintainer="$MAINTAINER" \
        -section="$SECTION" -out="$work_dir" -arch="$arch"
    rm -f "$work_dir/DEBIAN/preinst" "$work_dir/DEBIAN/postinst" \
        "$work_dir/DEBIAN/prerm" "$work_dir/DEBIAN/postrm"
    if [ -n "$SOURCE_COMMIT" ]; then
        printf 'Git-Commit: %s\nRepository-URL: %s\n' "$SOURCE_COMMIT" "$HOMEPAGE" >> "$work_dir/DEBIAN/control"
    fi
    ./scripts/stage-tpa-release.sh "$ROOT/$work_dir" "$arch" "$VERSION" "$ROOT/tpa"
    ./tpa build -in="$work_dir" -out="dist/tpa-$arch.deb"
    rm -rf "$work_dir"
done
rm -f tpa
echo "Building done"
