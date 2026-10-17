#!/usr/bin/env bash
set -euo pipefail

: "${TPA_RELEASE_PACKAGE_FILE:?package path is required}"
: "${TPA_RELEASE_VERSION:?release version is required}"
: "${TPA_RELEASE_ARCH:?target Debian architecture is required}"
test -f "$TPA_RELEASE_PACKAGE_FILE"

test "$(dpkg-deb -f "$TPA_RELEASE_PACKAGE_FILE" Package)" = tpa
test "$(dpkg-deb -f "$TPA_RELEASE_PACKAGE_FILE" Version)" = "$TPA_RELEASE_VERSION"
test "$(dpkg-deb -f "$TPA_RELEASE_PACKAGE_FILE" Architecture)" = "$TPA_RELEASE_ARCH"
test "$(dpkg-deb -f "$TPA_RELEASE_PACKAGE_FILE" TPA-Version)" = "$TPA_RELEASE_TPA_VERSION"

docker run --rm --platform "linux/$TPA_RELEASE_ARCH" \
  --env DEBIAN_FRONTEND=noninteractive \
  --env "TPA_RELEASE_VERSION=$TPA_RELEASE_VERSION" \
  --volume "$TPA_RELEASE_PACKAGE_FILE:/tmp/tpa-package.deb:ro" \
  debian:trixie-slim sh -ec '
    apt-get update
    apt-get install -y --no-install-recommends /tmp/tpa-package.deb
    test "$(tpa version)" = "$TPA_RELEASE_VERSION"
    dpkg-query -W -f="${Package} ${Version} ${Architecture}\n" tpa
    tpa --help >/dev/null
  '
