#!/bin/sh
# Proves that dependency metadata from .deb artifacts survives repository
# generation and that APT resolves direct and transitive dependencies.
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT INT TERM
mkdir "$TMP/packages"

make_deb() {
    name=$1
    depends=$2
    root="$TMP/pkg-$name"
    mkdir -p "$root/DEBIAN" "$root/usr/share/$name"
    {
        printf 'Package: %s\n' "$name"
        printf 'Version: 1.0.0\n'
        printf 'Architecture: all\n'
        printf 'Maintainer: TPA dependency qualification <tpa-qualification@example.invalid>\n'
        printf 'Description: TPA dependency qualification package %s\n' "$name"
        if [ -n "$depends" ]; then
            printf 'Depends: %s\n' "$depends"
        fi
    } >"$root/DEBIAN/control"
    printf '%s\n' "$name" >"$root/usr/share/$name/installed"
    dpkg-deb --build --root-owner-group "$root" "$TMP/packages/${name}_1.0.0_all.deb" >/dev/null
}

make_deb dep-base ''
make_deb dep-meta 'dep-base (= 1.0.0)'
make_deb dep-chain 'dep-meta (= 1.0.0)'

(cd "$ROOT" && go build -o "$TMP/tpa" .)
"$TMP/tpa" pack -in="$TMP/packages" -atomic-publish="$TMP/repo" >/dev/null

PACKAGES="$TMP/repo/dists/stable/main/binary-all/Packages"
META_STANZA=$(awk 'BEGIN { RS="" } /Package: dep-meta\n/ { print }' "$PACKAGES")
CHAIN_STANZA=$(awk 'BEGIN { RS="" } /Package: dep-chain\n/ { print }' "$PACKAGES")
printf '%s\n' "$META_STANZA" | grep -qx 'Depends: dep-base (= 1.0.0)'
printf '%s\n' "$CHAIN_STANZA" | grep -qx 'Depends: dep-meta (= 1.0.0)'
printf '%s\n' "$META_STANZA" | grep -q '^Filename: pool/main/d/dep-meta/dep-meta_1.0.0_all.deb$'
printf '%s\n' "$META_STANZA" | grep -q '^Size: [0-9][0-9]*$'
printf '%s\n' "$META_STANZA" | grep -q '^SHA256: [0-9a-f][0-9a-f]*$'

cat >"$TMP/Dockerfile" <<'EOF'
FROM debian:bookworm-slim
COPY repo /repo
RUN rm -f /etc/apt/sources.list /etc/apt/sources.list.d/* && \
    printf 'deb [trusted=yes] file:/repo stable main\n' > /etc/apt/sources.list.d/fixture.list && \
    apt-get update && \
    apt-get install -y dep-chain && \
    dpkg-query -W -f='${Status} ${Version}\n' dep-base | grep -qx 'install ok installed 1.0.0' && \
    dpkg-query -W -f='${Status} ${Version}\n' dep-meta | grep -qx 'install ok installed 1.0.0' && \
    dpkg-query -W -f='${Status} ${Version}\n' dep-chain | grep -qx 'install ok installed 1.0.0'
EOF

docker build --network=none -f "$TMP/Dockerfile" "$TMP"
printf '%s\n' 'TPA direct and transitive APT dependency resolution qualification passed.'
