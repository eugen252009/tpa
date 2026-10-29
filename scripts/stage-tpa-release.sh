#!/bin/sh
# Shared payload staging for build.sh and the optional reusable release workflow.
# Metadata and DEB construction remain owned by the normal TPA init/build engine.
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"
[ "$#" -ge 3 ] && [ "$#" -le 4 ] || { echo 'usage: stage-tpa-release.sh STAGE ARCH VERSION [HOST_TPA]' >&2; exit 2; }
STAGE=$1
ARCH=$2
VERSION=$3
HOST=${4:-}
case "$ARCH" in amd64|arm64|riscv64) ;; *) echo "unsupported release architecture: $ARCH" >&2; exit 2 ;; esac
dpkg --validate-version "$VERSION" >/dev/null 2>&1
LDFLAGS="-X github.com/eugen252009/tpa/internal/version.Version=$VERSION"
TMP=
cleanup() { if [ -n "$TMP" ]; then rm -rf "$TMP"; fi; }
trap cleanup EXIT HUP INT TERM
if [ -z "$HOST" ]; then
    TMP=$(mktemp -d)
    HOST="$TMP/tpa"
    CGO_ENABLED=0 go build -trimpath -buildvcs=false -p="${TPA_BUILD_JOBS:-4}" -ldflags "$LDFLAGS" -o "$HOST" .
fi
mkdir -p "$STAGE/usr/local/bin" "$STAGE/usr/share/man/man1" "$STAGE/usr/share/doc/tpa"
# Debian slim images exclude most docs but explicitly preserve copyright.
# Keep all required notices there, not solely in an optional separate doc file.
cat LICENSE THIRD_PARTY_NOTICES > "$STAGE/usr/share/doc/tpa/copyright"
cp THIRD_PARTY_NOTICES "$STAGE/usr/share/doc/tpa/THIRD_PARTY_NOTICES"
chmod 0644 "$STAGE/usr/share/doc/tpa/copyright" "$STAGE/usr/share/doc/tpa/THIRD_PARTY_NOTICES"
for executable in tpa tpa-utils tpa-release; do
    case "$executable" in tpa) package=. ;; *) package=./cmd/$executable ;; esac
    CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" go build -trimpath -buildvcs=false \
        -p="${TPA_BUILD_JOBS:-4}" -ldflags "$LDFLAGS" -o "$STAGE/usr/local/bin/$executable" "$package"
    chmod 0755 "$STAGE/usr/local/bin/$executable"
done
gzip -n -c manpage/usr/share/man/man1/tpa.1 > "$STAGE/usr/share/man/man1/tpa.1.gz"
chmod 0644 "$STAGE/usr/share/man/man1/tpa.1.gz"
mkdir -p "$STAGE/usr/share/bash-completion/completions" \
    "$STAGE/usr/share/zsh/vendor-completions" "$STAGE/usr/share/fish/vendor_completions.d"
"$HOST" completion bash > "$STAGE/usr/share/bash-completion/completions/tpa"
"$HOST" completion zsh > "$STAGE/usr/share/zsh/vendor-completions/_tpa"
"$HOST" completion fish > "$STAGE/usr/share/fish/vendor_completions.d/tpa.fish"
chmod 0644 "$STAGE/usr/share/bash-completion/completions/tpa" \
    "$STAGE/usr/share/zsh/vendor-completions/_tpa" "$STAGE/usr/share/fish/vendor_completions.d/tpa.fish"
