#!/bin/bash
# Real retained BLL/Mema packages -> local owner signing -> ordinary OpenSSH ->
# HTTP/APT on disposable Debian12. No TPA.run/account/CAS/server signing service.
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd)
INPUT=${1:?usage: signed-ssh-repository-qualification.sh retained-review-directory}
EVIDENCE=${EVIDENCE_DIR:-$(mktemp -d /tmp/tpa-signed-ssh-evidence.XXXXXX)}
mkdir -p "$EVIDENCE"
TMP=$(mktemp -d)
CONTAINER=tpa-owner-signed-ssh-$$
cleanup(){ docker rm -f "$CONTAINER" >/dev/null 2>&1 || :; GNUPGHOME="$TMP/gpg" gpgconf --kill gpg-agent >/dev/null 2>&1 || :; rm -rf "$TMP"; }
trap cleanup EXIT
export GNUPGHOME="$TMP/gpg"
mkdir -m700 "$GNUPGHOME"
(cd "$ROOT" && go build -o "$TMP/tpa" .)
if test -n "${FROZEN_SOURCE_DIR:-}"; then
 gpg --batch --import "${FROZEN_PUBLIC_KEY:?required with frozen source}" >"$TMP/public-import.log" 2>&1
 FP=$(gpg --batch --with-colons --list-keys 2>/dev/null | awk -F: '$1=="fpr"{print $10;exit}')
 mkdir "$EVIDENCE/source"; cp -a "$FROZEN_SOURCE_DIR/." "$EVIDENCE/source/"
else
 gpg --batch --pinentry-mode loopback --passphrase '' --quick-generate-key 'Local SSH Qualification <ssh@example.invalid>' rsa2048 sign 0 >"$TMP/keygen.log" 2>&1
 FP=$(gpg --batch --with-colons --list-secret-keys 2>/dev/null | awk -F: '$1=="fpr"{print $10;exit}')
 mkdir "$TMP/input"
 # Existing archives are reused unchanged; source package trees are not rebuilt.
 cp "$INPUT"/bll/dist/bll*_0.2.0_all.deb "$INPUT/mema-041/dist/mema_0.4.1_amd64.deb" "$INPUT/mema-release/dist/mema_0.4.2_amd64.deb" "$TMP/input/"
 (cd "$TMP/input" && sha256sum *.deb) > "$EVIDENCE/input-packages.sha256"
 "$TMP/tpa" pack -in="$TMP/input" -out="$EVIDENCE/source" -gpg="$FP"
fi
gpg --batch --export "$FP" > "$EVIDENCE/owner-public.gpg"
export QUALIFICATION_PUBLIC_KEY="$EVIDENCE/owner-public.gpg"
"$TMP/tpa" verify --require-signed -keyring="$EVIDENCE/owner-public.gpg" -fingerprint="$FP" "$EVIDENCE/source"
mkdir "$TMP/helper"
printf 'module qualification\n\ngo 1.26.3\nrequire github.com/eugen252009/tpa v0.0.0\nreplace github.com/eugen252009/tpa => %s\n' "$ROOT" > "$TMP/helper/go.mod"
cat > "$TMP/helper/main.go" <<'GO'
package main
import("context";"fmt";"os";"github.com/eugen252009/tpa/internals/aptpackage")
func main(){ctx:=context.Background();key,e:=os.ReadFile(os.Getenv("QUALIFICATION_PUBLIC_KEY"));if e==nil{var loc aptpackage.RepositoryLocator;loc,e=aptpackage.ParseRepositoryLocator(os.Args[1]);if e==nil{var reader aptpackage.RepositoryReader;reader,e=aptpackage.OpenRepositoryReader(loc);if e==nil{_,e=aptpackage.VerifyRepository(ctx,reader,aptpackage.RepositoryOptions{Codename:"stable",Keyring:key,Fingerprint:os.Args[4],RequireSigned:true});reader.Close()}}};if e==nil{var d aptpackage.RepositoryDestination;d,e=aptpackage.ParseRepositoryDestination(os.Args[2]);if e==nil{d.SSHConfigPath=os.Args[3];e=aptpackage.PublishRepositoryToSSH(ctx,aptpackage.Config{OutDir:os.Args[1],GPG:os.Args[4]},d)}};if e!=nil{fmt.Fprintln(os.Stderr,e);os.Exit(1)}}
GO
(cd "$TMP/helper" && go build -mod=mod -o "$TMP/publish" .)
ssh-keygen -q -t ed25519 -N '' -f "$TMP/client"
docker run --rm -d --name "$CONTAINER" -p 127.0.0.1::2222 debian:bookworm-slim sleep 600 >/dev/null
docker exec "$CONTAINER" sh -eu -c 'apt-get update -qq; DEBIAN_FRONTEND=noninteractive apt-get install -qq -y openssh-server python3 >/dev/null; useradd -m -s /bin/sh publisher; mkdir -p /home/publisher/.ssh /run/sshd; chmod 700 /home/publisher/.ssh; ssh-keygen -A' >/dev/null
docker cp "$TMP/client.pub" "$CONTAINER:/home/publisher/.ssh/authorized_keys" >/dev/null
docker exec "$CONTAINER" sh -c 'chown -R publisher:publisher /home/publisher; chmod 600 /home/publisher/.ssh/authorized_keys'
docker exec -d "$CONTAINER" /usr/sbin/sshd -D -e -p 2222
PORT=$(docker port "$CONTAINER" 2222/tcp); PORT=${PORT##*:}
for _ in $(seq 1 30); do ssh-keyscan -p "$PORT" 127.0.0.1 >"$TMP/known_hosts" 2>/dev/null && test -s "$TMP/known_hosts" && break; sleep .1; done
printf 'Host *\n IdentityFile %s\n IdentitiesOnly yes\n UserKnownHostsFile %s\n StrictHostKeyChecking yes\n' "$TMP/client" "$TMP/known_hosts" >"$TMP/config"
# The existing transport owns/mutates its readback scratch, never source/.
mkdir "$TMP/scratch"; cp -a "$EVIDENCE/source/." "$TMP/scratch/"
URL="ssh://publisher@127.0.0.1:$PORT/home/publisher/finished"
if test "${MEASURE_SSH:-false}" = true; then
 mkdir "$TMP/instrument"
 printf '#!/bin/bash\nargs=(); for arg in "$@"; do if test "$arg" != "-q"; then args+=("$arg"); fi; done\nexec /usr/bin/ssh -vv -E "$SSH_TRACE_DIR/ssh-$$.log" "${args[@]}"\n' > "$TMP/instrument/ssh-measured"
 printf '#!/bin/sh\nexec /usr/bin/sftp -S "$SSH_MEASURED" "$@"\n' > "$TMP/instrument/sftp"
 chmod 755 "$TMP/instrument/ssh-measured" "$TMP/instrument/sftp"
 PATH="$TMP/instrument:$PATH" SSH_MEASURED="$TMP/instrument/ssh-measured" SSH_TRACE_DIR="$EVIDENCE" python3 "$ROOT/bench/measure-ordinary.py" "$EVIDENCE" "$TMP/publish" "$TMP/scratch" "$URL" "$TMP/config" "$FP"
else
 "$TMP/publish" "$TMP/scratch" "$URL" "$TMP/config" "$FP"
fi
if "$TMP/publish" "$TMP/scratch" "$URL" "$TMP/config" "$FP" >"$EVIDENCE/repeat.log" 2>&1; then echo 'existing destination unexpectedly replaced' >&2; exit 1; fi
grep -F 'destination already exists' "$EVIDENCE/repeat.log"
mkdir "$EVIDENCE/received"; docker cp "$CONTAINER:/home/publisher/finished/." "$EVIDENCE/received/" >/dev/null
python3 - "$EVIDENCE" <<'PY'
import pathlib,hashlib,json,sys
r=pathlib.Path(sys.argv[1])
def inventory(root):return {str(p.relative_to(root)):hashlib.sha256(p.read_bytes()).hexdigest() for p in root.rglob('*') if p.is_file()}
a,b=inventory(r/'source'),inventory(r/'received');assert a==b
assert sum(p.endswith('.deb') for p in a)==6
assert 'index.html' in a and 'repository.json' in a and 'dists/stable/InRelease' in a
(r/'preserved-files.json').write_text(json.dumps(a,indent=2)+'\n')
PY
"$TMP/tpa" verify --require-signed -keyring="$EVIDENCE/owner-public.gpg" -fingerprint="$FP" "$EVIDENCE/received"
docker cp "$EVIDENCE/owner-public.gpg" "$CONTAINER:/usr/share/keyrings/owner.gpg" >/dev/null
docker exec -d "$CONTAINER" python3 -m http.server 8080 --bind 127.0.0.1 --directory /home/publisher
for _ in $(seq 1 30); do docker exec "$CONTAINER" python3 -c 'import urllib.request;urllib.request.urlopen("http://127.0.0.1:8080/finished/index.html").read(1)' >/dev/null 2>&1 && break; sleep .1; done
docker exec "$CONTAINER" sh -eu -c '
 printf "deb [signed-by=/usr/share/keyrings/owner.gpg] http://127.0.0.1:8080/finished stable main\n" > /etc/apt/sources.list.d/owner.list
 apt-get update
 DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends mema=0.4.1 bll bll-bash bll-zsh
 test "$(dpkg-query -W -f='\''${Version}'\'' mema)" = 0.4.1
 DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends mema=0.4.2
 test "$(dpkg-query -W -f='\''${Version}'\'' mema)" = 0.4.2
 mema --version
 mema list
' >"$EVIDENCE/apt-install-upgrade.log" 2>&1
echo "PASS: real BLL/Mema exact signed repository -> ordinary SSH -> HTTP -> Debian12 APT install/upgrade; evidence $EVIDENCE"
