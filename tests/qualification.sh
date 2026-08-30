#!/bin/sh
# Reproducible TPA signed-repository qualification. Requires docker, gpg,
# dpkg-deb, and a Go toolchain. It intentionally uses a throw-away signing key.
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
TMP=$(mktemp -d)
GNUPGHOME="$TMP/gnupg"
export GNUPGHOME
trap 'rm -rf "$TMP"' EXIT INT TERM
mkdir -m 700 "$GNUPGHOME"

cat >"$TMP/key.conf" <<'EOF'
%no-protection
Key-Type: RSA
Key-Length: 2048
Name-Real: TPA qualification
Name-Email: tpa-qualification@example.invalid
Expire-Date: 0
%commit
EOF
gpg --batch --generate-key "$TMP/key.conf" >/dev/null 2>&1
KEY=$(gpg --batch --with-colons --list-secret-keys | awk -F: '$1 == "fpr" { print $10; exit }')
[ -n "$KEY" ] || { echo 'could not create qualification key' >&2; exit 1; }

mkdir "$TMP/packages"
make_deb() {
    version=$1
    root="$TMP/pkg-$version"
    mkdir -p "$root/DEBIAN" "$root/usr/share/tpa-qualification"
    cat >"$root/DEBIAN/control" <<EOF
Package: tpa-qualification
Version: $version
Architecture: all
Maintainer: TPA qualification <tpa-qualification@example.invalid>
Description: TPA repository qualification package
EOF
    printf '%s\n' "$version" >"$root/usr/share/tpa-qualification/version"
    dpkg-deb --build "$root" "$TMP/packages/tpa-qualification_${version}_all.deb" >/dev/null
}
make_deb 1.0.0
make_deb 2.0.0

(cd "$ROOT" && go build -o "$TMP/tpa" .)
"$TMP/tpa" pack -in="$TMP/packages" -atomic-publish="$TMP/repo" -gpg="$KEY" \
    -origin=TPA-Qualification -label=TPA-Qualification -suite=stable \
    -codename=stable -components=main >/dev/null

[ -s "$TMP/repo/dists/stable/Release" ]
[ -s "$TMP/repo/dists/stable/InRelease" ]
[ -s "$TMP/repo/dists/stable/main/binary-all/Packages" ]
[ -s "$TMP/repo/dists/stable/main/binary-all/Packages.gz" ]
[ -s "$TMP/repo/pool/main/t/tpa-qualification/tpa-qualification_1.0.0_all.deb" ]
[ -s "$TMP/repo/pool/main/t/tpa-qualification/tpa-qualification_2.0.0_all.deb" ]
gpg --batch --verify "$TMP/repo/dists/stable/InRelease" >/dev/null 2>&1
# Verify the clear-signature payload independently as well.
gpg --batch --decrypt "$TMP/repo/dists/stable/InRelease" >"$TMP/release" 2>/dev/null
diff -q "$TMP/release" "$TMP/repo/dists/stable/Release" >/dev/null

gpg --batch --export "$KEY" >"$TMP/fixture.gpg"
cat >"$TMP/Dockerfile" <<'EOF'
FROM debian:bookworm-slim
COPY repo /repo
COPY fixture.gpg /etc/apt/keyrings/fixture.gpg
RUN rm -f /etc/apt/sources.list /etc/apt/sources.list.d/* && \
    printf 'deb [signed-by=/etc/apt/keyrings/fixture.gpg] file:/repo stable main\n' > /etc/apt/sources.list.d/fixture.list && \
    apt-get update && \
    apt-get install -y tpa-qualification=1.0.0 && \
    test "$(cat /usr/share/tpa-qualification/version)" = 1.0.0 && \
    apt-get upgrade -y && \
    test "$(cat /usr/share/tpa-qualification/version)" = 2.0.0 && \
    apt-get install -y --allow-downgrades tpa-qualification=1.0.0 && \
    test "$(cat /usr/share/tpa-qualification/version)" = 1.0.0 && \
    dpkg-query -W -f='${Version}\n' tpa-qualification | grep -qx 1.0.0
EOF

docker build --network=none -f "$TMP/Dockerfile" "$TMP"
printf '%s\n' 'TPA signed repository, APT install, upgrade, and downgrade qualification passed.'
