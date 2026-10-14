#!/bin/sh
# Synthetic fixtures only: no host installation, external source, or publication.
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
for tool in go dpkg-deb gpg gzip docker python3; do command -v "$tool" >/dev/null 2>&1 || { echo "missing $tool" >&2; exit 2; }; done
TMP=$(mktemp -d)
chmod 0755 "$TMP"
trap 'rm -rf "$TMP"' EXIT HUP INT TERM
export GNUPGHOME="$TMP/gnupg"
mkdir -m 700 "$GNUPGHOME"
(cd "$ROOT" && go build -o "$TMP/tpa" .)
(cd "$ROOT" && go build -o "$TMP/tpa-utils" ./cmd/tpa-utils)
python3 - "$TMP" <<'PY'
import json,pathlib,sys
root=pathlib.Path(sys.argv[1]);(root/'definitions').mkdir()
records=[]
for n in (1,2):
    version=f'{n}.0'
    records.append({'control':{'name':'tpa-direct-fixture','version':version,'architecture':'all','maintainer':'Fixture <fixture@example.invalid>','description':'Synthetic metadata-only package','depends':'tpa-direct-dependency (>= 1.0)'},'scripts':{'postinst':f'#!/bin/sh\nset -e\nprintf "{version}\\n" > /var/lib/tpa-direct-fixture-version\n'}})
    (root/'definitions'/f'{n}.json').write_text(json.dumps(records[-1])+'\n')
for name,depends in [('tpa-direct-dependency','tpa-direct-leaf (>= 1.0)'),('tpa-direct-leaf','')]:
    record={'control':{'name':name,'version':'1.0','architecture':'all','maintainer':'Fixture','description':'Metadata-only dependency','depends':depends}}
    records.append(record)
    (root/'definitions'/f'{name}.json').write_text(json.dumps(record)+'\n')
(root/'definitions.jsonl').write_text(''.join(json.dumps(r)+'\n' for r in records))
PY
"$TMP/tpa-utils" check package tpa-direct-fixture
"$TMP/tpa-utils" check control "$TMP/definitions/1.json"
test "$("$TMP/tpa-utils" version compare 1.0~beta 1.0)" = -1
"$TMP/tpa" compose -in="$TMP/definitions" -out="$TMP/from-directory" -epoch=1700000000 -workers=1
"$TMP/tpa" compose -in="$TMP/definitions.jsonl" -out="$TMP/from-jsonl" -epoch=1700000000 -workers=8
# Same effective records, independent discovery/worker order, byte-identical trees.
diff -r "$TMP/from-directory" "$TMP/from-jsonl"
gpg --batch --pinentry-mode loopback --passphrase '' --quick-generate-key 'Direct Qualification <direct@example.invalid>' rsa2048 sign 0 >"$TMP/keygen.log" 2>&1
fingerprint=$(gpg --batch --with-colons --list-secret-keys | awk -F: '$1=="fpr" {print $10;exit}')
gpg --batch --export "$fingerprint" >"$TMP/fixture.gpg"
"$TMP/tpa" compose -in="$TMP/definitions.jsonl" -out="$TMP/repo" -epoch=1700000000 -gpg="$fingerprint" -workers=8
"$TMP/tpa" verify --json --require-signed -keyring="$TMP/fixture.gpg" -fingerprint="$fingerprint" "$TMP/repo" >"$TMP/verification.json"
"$TMP/tpa-utils" repo inspect --require-signed --keyring="$TMP/fixture.gpg" --fingerprint="$fingerprint" "$TMP/repo" >"$TMP/inspection.json"
find "$TMP/repo" -type f -exec sha256sum '{}' \; | sort >"$TMP/tree-before.txt"
"$TMP/tpa-utils" repo html --out="$TMP/web" --base-url=https://repository.example.invalid/ --keyring="$TMP/fixture.gpg" "$TMP/repo"
find "$TMP/repo" -type f -exec sha256sum '{}' \; | sort >"$TMP/tree-after.txt"
cmp "$TMP/tree-before.txt" "$TMP/tree-after.txt"
for deb in "$TMP/repo"/pool/main/t/*/*.deb; do
 dpkg-deb --info "$deb" >/dev/null
 test -z "$(dpkg-deb --contents "$deb")"
done
python3 - "$TMP/verification.json" "$TMP/web/repository.json" <<'PY'
import json,sys
r=json.load(open(sys.argv[1]));assert r['valid'] and r['signatureVerified'] and r['packageCount']==4,r
view=json.load(open(sys.argv[2]));assert view['format']=='tpa-utils-repository-browser' and view['version']==1
assert len(view['repository']['packages'])==4
PY
docker run --rm -v "$TMP:/fixtures:ro" debian:bookworm-slim sh -eu -c '
 cp /fixtures/fixture.gpg /usr/share/keyrings/direct-fixture.gpg
 chmod 0644 /usr/share/keyrings/direct-fixture.gpg
 rm -f /etc/apt/sources.list /etc/apt/sources.list.d/*
 printf "deb [signed-by=/usr/share/keyrings/direct-fixture.gpg] file:/fixtures/repo stable main\n" >/etc/apt/sources.list.d/direct.list
 apt-get update
 apt-get install -y tpa-direct-fixture=1.0
 test "$(dpkg-query -W -f='\''${Version}'\'' tpa-direct-fixture)" = 1.0
 test "$(cat /var/lib/tpa-direct-fixture-version)" = 1.0
 test "$(dpkg-query -W -f='\''${Version}'\'' tpa-direct-dependency)" = 1.0
 test "$(dpkg-query -W -f='\''${Version}'\'' tpa-direct-leaf)" = 1.0
 apt-get upgrade -y
 test "$(dpkg-query -W -f='\''${Version}'\'' tpa-direct-fixture)" = 2.0
 test "$(cat /var/lib/tpa-direct-fixture-version)" = 2.0
 apt-cache madison tpa-direct-fixture | grep -F "1.0"
 apt-cache madison tpa-direct-fixture | grep -F "2.0"
'
cp -a "$TMP/repo" "$TMP/tampered"
printf corrupt >"$TMP/tampered/pool/main/t/tpa-direct-fixture/tpa-direct-fixture_2.0_all.deb"
if "$TMP/tpa" verify --json -keyring="$TMP/fixture.gpg" "$TMP/tampered" >"$TMP/tampered.json"; then echo 'accepted tampered artifact' >&2; exit 1; fi
if "$TMP/tpa" compose -in="$TMP/definitions.jsonl" -out="$TMP/failed" -gpg=absent-direct-signing-fixture >"$TMP/failed.log" 2>&1; then echo 'accepted absent signer' >&2; exit 1; fi
test ! -e "$TMP/failed"
echo 'Direct metadata-only DEB, directory/JSONL determinism, signed APT dependency install/upgrade, browser isolation, and tamper qualification PASS'
