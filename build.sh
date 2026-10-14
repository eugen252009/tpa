#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$ROOT"

ARCHS="amd64 riscv64 arm64"
DESC=$(cat description.txt)
VERSION=${TPA_VERSION:-0.0.0~dev}
if ! dpkg --validate-version "$VERSION" >/dev/null 2>&1; then
    echo "Invalid Debian version in TPA_VERSION: $VERSION" >&2
    exit 1
fi
LDFLAGS="-X github.com/eugen252009/tpa/internal/version.Version=$VERSION"
DEPENDS="libc6,dpkg,gpg,gzip"
HOMEPAGE="https://github.com/eugen252009/tpa"
MAINTAINER="TPA Project <tpa@lupricht.net>"
SECTION="utils"

# The host binary is only the package-builder bootstrap; packaged binaries are
# cross-compiled below and never copied from the host build.
go build -ldflags "$LDFLAGS" -o tpa .
mkdir -p dist

for arch in $ARCHS; do
    echo "Building for $arch..."
    work_dir="tpa-$arch"
    rm -rf "$work_dir"
    mkdir -p "$work_dir/usr/local/bin"

    ./tpa init \
        -name=tpa -ver="$VERSION" -depends="$DEPENDS" -recommends=bash-completion \
        -desc="$DESC" -homepage="$HOMEPAGE" -maintainer="$MAINTAINER" \
        -section="$SECTION" -out="$work_dir" -arch="$arch"
    # TPA init creates empty compatibility scripts; the package needs none.
    rm -f "$work_dir/DEBIAN/preinst" "$work_dir/DEBIAN/postinst" \
        "$work_dir/DEBIAN/prerm" "$work_dir/DEBIAN/postrm"

    CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -ldflags "$LDFLAGS" -o "$work_dir/usr/local/bin/tpa" .
    chmod 0755 "$work_dir/usr/local/bin/tpa"
    if [ -f manpage/usr/share/man/man1/tpa.1 ]; then
        mkdir -p "$work_dir/usr/share/man/man1"
        gzip -cn manpage/usr/share/man/man1/tpa.1 >"$work_dir/usr/share/man/man1/tpa.1.gz"
        chmod 0644 "$work_dir/usr/share/man/man1/tpa.1.gz"
    fi
    # Vendor paths are discovered by each shell; no profile edits or postinst
    # shell execution are needed. Generate with the host binary for all targets.
    mkdir -p "$work_dir/usr/share/bash-completion/completions" \
        "$work_dir/usr/share/zsh/vendor-completions" \
        "$work_dir/usr/share/fish/vendor_completions.d"
    ./tpa completion bash >"$work_dir/usr/share/bash-completion/completions/tpa"
    ./tpa completion zsh >"$work_dir/usr/share/zsh/vendor-completions/_tpa"
    ./tpa completion fish >"$work_dir/usr/share/fish/vendor_completions.d/tpa.fish"
    chmod 0644 "$work_dir/usr/share/bash-completion/completions/tpa" \
        "$work_dir/usr/share/zsh/vendor-completions/_tpa" \
        "$work_dir/usr/share/fish/vendor_completions.d/tpa.fish"
    ./tpa build -in="$work_dir" -out="dist/tpa-$arch.deb"
    rm -rf "$work_dir"
done

rm -f tpa
echo "Building done"
