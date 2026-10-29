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
    dpkg-query -W -f='\''${Package} ${Version} ${Architecture}\n'\'' tpa
    test -s /usr/share/doc/tpa/copyright
    grep -F "MIT License" /usr/share/doc/tpa/copyright
    grep -F "the tpa.run Team" /usr/share/doc/tpa/copyright
    # Full third-party terms must survive Debian slim path-exclude doc filters.
    for notice in "Ulrich Kunitz" "Go Authors" "Kirill Simonov" "Canonical Ltd" "END OF TERMS AND CONDITIONS"; do
      grep -F "$notice" /usr/share/doc/tpa/copyright
    done
    tpa --help > /tmp/tpa-help
    for command in compose stage sign verify publish; do
      grep -E "^[[:space:]]+$command[[:space:]]" /tmp/tpa-help
    done
    cat > /tmp/definition.json <<EOF
{"control":{"name":"tpa-install-proof","version":"1.0","architecture":"all","maintainer":"Fixture <fixture@example.invalid>","description":"Installed compiler proof"}}
EOF
    tpa compose -in=/tmp/definition.json -out=/tmp/compiled -epoch=1700000000 -workers=2
    tpa verify /tmp/compiled
    tpa-utils --help >/dev/null
    tpa-release --help >/dev/null

  '
