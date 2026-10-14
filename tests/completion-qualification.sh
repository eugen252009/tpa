#!/bin/sh
# Fresh Debian installation and vendor completion discovery; no host install.
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
for tool in docker go dpkg-deb tar; do
    command -v "$tool" >/dev/null 2>&1 || { echo "missing required tool: $tool" >&2; exit 2; }
done
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT HUP INT TERM
mkdir -p "$TMP/source"
tar -C "$ROOT" --exclude='./.git' --exclude='./.cocoindex_code' \
    --exclude='./dist' --exclude='./bench' --exclude='./tpa' --exclude='./ops' \
    --exclude='./tpa-amd64' --exclude='./tpa-arm64' --exclude='./tpa-riscv64' \
    -cf - . | tar -C "$TMP/source" -xf -
(cd "$TMP/source" && TPA_VERSION=9.8.7~completion1 ./build.sh)
case "$(go env GOARCH)" in
    amd64|arm64|riscv64) arch=$(go env GOARCH) ;;
    *) echo 'unsupported qualification architecture' >&2; exit 2 ;;
esac
cp "$TMP/source/dist/tpa-$arch.deb" "$TMP/candidate.deb"
docker run --rm -v "$TMP:/fixtures:ro" debian:bookworm-slim sh -eu -c '
    export DEBIAN_FRONTEND=noninteractive
    apt-get update
    apt-get install -y zsh fish
    sha256sum /etc/bash.bashrc /etc/zsh/zshrc /etc/fish/config.fish >/tmp/profiles.sha256
    apt-get install -y /fixtures/candidate.deb
    test "$(tpa version)" = 9.8.7~completion1
    dpkg-query -W bash-completion
    sha256sum -c /tmp/profiles.sha256
    for file in /usr/share/bash-completion/completions/tpa \
        /usr/share/zsh/vendor-completions/_tpa \
        /usr/share/fish/vendor_completions.d/tpa.fish; do
        test -s "$file"
        test "$(stat -c %a "$file")" = 644
    done
    # Load only the normal Bash framework, not the TPA completion file.
    bash --noprofile --norc -c '\''
        source /usr/share/bash-completion/bash_completion
        _completion_loader tpa || :
        complete -p tpa | grep -F -- "-F _tpa tpa"
        COMP_WORDS=(tpa pack --ato)
        COMP_CWORD=2
        COMP_LINE="tpa pack --ato"
        COMP_POINT=${#COMP_LINE}
        _tpa
        printf "%s\n" "${COMPREPLY[@]}" | grep -Fx -- --atomic-publish
    '\''
    # compinit scans the installed vendor directory without profile edits.
    zsh -fc '\''autoload -Uz compinit; compinit -D; print -r -- "zsh tpa completion: ${_comps[tpa]}"; [[ ${_comps[tpa]} == _tpa ]]'\''
    # No explicit source: Fish discovers the installed vendor completion.
    fish -c '\''complete -C "tpa pack --ato"'\'' | grep -F -- --atomic-publish
    dpkg -r tpa
    test ! -e /usr/share/bash-completion/completions/tpa
    test ! -e /usr/share/zsh/vendor-completions/_tpa
    test ! -e /usr/share/fish/vendor_completions.d/tpa.fish
    dpkg -i /fixtures/candidate.deb
    fish -c '\''complete -C "tpa completion fi"'\'' | grep -Fx fish
    apt-get check
    dpkg --audit
    sha256sum -c /tmp/profiles.sha256
'
echo 'Automatic shell completion installation/removal/reinstallation PASS'
