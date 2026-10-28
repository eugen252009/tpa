#!/usr/bin/env bash
# Owner-side CI adapter. No package build operation exists in this script.
set -euo pipefail
umask 077
mode=${1:?create|upload|verify|sign}
: "${RUNNER_TEMP:?}" "${APT_DESTINATION:?}" "${APPROVED_MANIFEST_SHA256:?}"
bundle=${RELEASE_BUNDLE:-$RUNNER_TEMP/release-bundle}
manifest=$bundle/release-manifest.json
tpa=${TPA_BIN:-$RUNNER_TEMP/tpa-builder}
[[ $APPROVED_MANIFEST_SHA256 =~ ^[0-9a-f]{64}$ ]]
actual=$(sha256sum "$manifest")
[[ ${actual%% *} == "$APPROVED_MANIFEST_SHA256" ]] || { echo 'candidate manifest mismatch' >&2; exit 1; }
[[ ${APT_CHANNEL:-} == staging || ${APT_CHANNEL:-} == stable ]] || { echo 'explicit staging/stable channel required' >&2; exit 1; }
[[ $APT_DESTINATION == tparun+ssh://* ]] || { echo 'explicit SSH-CAS repository destination required' >&2; exit 1; }
# Destination participates in stage identity: the same tested bundle can be
# explicitly staged to distinct channel repositories, never implicitly promoted.
if [[ -z ${STAGE_ID:-} ]]; then
  identity=$(printf '%s\n%s\n' "$APT_DESTINATION" "$APPROVED_MANIFEST_SHA256" | sha256sum)
  STAGE_ID=${identity:0:32}
fi
[[ $STAGE_ID =~ ^[0-9a-f]{32}$ ]]
sshdir=$RUNNER_TEMP/apt-stage-ssh
mkdir -p -m 700 "$sshdir"
owner_home=""
cleanup() {
  rm -f -- "$sshdir/identity"
  if [[ -n $owner_home ]]; then
    GNUPGHOME="$owner_home" gpgconf --kill all >/dev/null 2>&1 || true
    rm -rf -- "$owner_home"
  fi
}
trap cleanup EXIT
: "${STAGE_SSH_KEY:?}" "${APT_KNOWN_HOSTS:?}"
printf '%s\n' "$STAGE_SSH_KEY" > "$sshdir/identity"
printf '%s\n' "$APT_KNOWN_HOSTS" > "$sshdir/known_hosts"
chmod 600 "$sshdir/identity" "$sshdir/known_hosts"
printf 'Host *\n IdentityFile %s\n IdentitiesOnly yes\n UserKnownHostsFile %s\n StrictHostKeyChecking yes\n' "$sshdir/identity" "$sshdir/known_hosts" > "$sshdir/config"
common=("--output=$APT_DESTINATION" "-stage=$STAGE_ID" "-ssh-config=$sshdir/config")
case $mode in
  create)
    "$tpa" stage create "${common[@]}" "-manifest=$manifest" > "$RUNNER_TEMP/stage-create.json"
    printf 'stage_id=%s\n' "$STAGE_ID" >> "${GITHUB_OUTPUT:?}"
    ;;
  upload)
    : "${TARGET_FILENAME:?}"
    # Validate the exact already-qualified file before any transfer. No glob or
    # alternate package source can substitute for the approved manifest entry.
    filename=$(python3 - "$manifest" "$bundle" "$TARGET_FILENAME" <<'PY'
import hashlib, json, pathlib, sys
manifest, root, filename = pathlib.Path(sys.argv[1]), pathlib.Path(sys.argv[2]), sys.argv[3]
items = [a for a in json.loads(manifest.read_bytes())["artifacts"] if a["filename"] == filename]
if len(items) != 1: raise SystemExit("expected exactly one qualified artifact for upload job")
a = items[0]
if pathlib.Path(a["filename"]).name != a["filename"]: raise SystemExit("unsafe artifact filename")
p = root / a["filename"]
with p.open("rb") as f: digest = hashlib.file_digest(f, "sha256").hexdigest()
if p.stat().st_size != a["size"] or digest != a["sha256"]: raise SystemExit("qualified artifact checksum mismatch")
print(a["filename"])
PY
)
    "$tpa" stage upload "${common[@]}" "-in=$bundle/$filename"
    ;;
  verify|sign)
    "$tpa" stage inspect "${common[@]}" > "$RUNNER_TEMP/stage-review.json"
    python3 - "$manifest" "$RUNNER_TEMP/stage-review.json" "$APPROVED_MANIFEST_SHA256" <<'PY'
import json, sys
expected = json.load(open(sys.argv[1]))["artifacts"]
stage = json.load(open(sys.argv[2]))["stage"]
keys = ("filename", "package", "version", "architecture", "size", "sha256")
def projection(items): return sorted(tuple(a[k] for k in keys) for a in items)
if stage["manifest_sha256"] != sys.argv[3] or projection(stage["artifacts"]) != projection(expected): raise SystemExit("staged artifact manifest mismatch")
if any(a["state"] != "ready" for a in stage["artifacts"]): raise SystemExit("required upload missing")
if stage["state"] not in ("OPEN", "FINALIZING", "PUBLISHED"): raise SystemExit("stage is unavailable")
PY
    if [[ $mode == sign ]]; then
      : "${APT_PUBLIC_KEY:?}" "${APT_SIGNING_FINGERPRINT:?}"
      [[ $APT_SIGNING_FINGERPRINT =~ ^[A-Fa-f0-9]{40}$ ]] || { echo 'full owner signing fingerprint required' >&2; exit 1; }
      if [[ -n ${APT_SIGNING_HOME:-} ]]; then
        [[ $APT_SIGNING_HOME == /* && -d $APT_SIGNING_HOME ]] || { echo "existing absolute owner signing home required" >&2; exit 1; }
        export GNUPGHOME=$APT_SIGNING_HOME
        # Reuse the owner's existing agent/hardware identity. Never import,
        # export, remove or kill it as if it were disposable runner state.
      else
        export GNUPGHOME=$RUNNER_TEMP/apt-owner-gnupg
        mkdir -p -m 700 "$GNUPGHOME"
        owner_home=$GNUPGHOME
      fi
      printf '%s\n' "$APT_PUBLIC_KEY" | gpg --batch --yes --dearmor > "$RUNNER_TEMP/apt-owner-public.gpg"
      state=$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["stage"]["state"])' "$RUNNER_TEMP/stage-review.json")
      if [[ $state != PUBLISHED && -n $owner_home ]]; then
        : "${APT_SIGNING_PRIVATE_KEY:?protected environment owner key required}"
        printf '%s\n' "$APT_SIGNING_PRIVATE_KEY" | gpg --batch --import
        # Unlock only this protected runner's local agent. The key/passphrase
        # never enters a stage request or the TPA.run receiver.
        gpg --batch --yes --pinentry-mode loopback --passphrase-fd 3 \
          --local-user "$APT_SIGNING_FINGERPRINT" --detach-sign \
          --output "$RUNNER_TEMP/owner-unlock.sig" "$manifest" 3<<<"${APT_SIGNING_PASSPHRASE:-}"
      fi
      "$tpa" sign "${common[@]}" "-manifest=$manifest" \
        "-gpg=$APT_SIGNING_FINGERPRINT" "-fingerprint=$APT_SIGNING_FINGERPRINT" \
        "-keyring=$RUNNER_TEMP/apt-owner-public.gpg" "-origin=${APT_ORIGIN:-TPA-Repo}" \
        "-label=${APT_LABEL:-TPA-Repo}" "-repo-description=${APT_DESCRIPTION:-}" \
        > "$RUNNER_TEMP/stage-publication.json"
    fi
    ;;
  *) echo 'unsupported stage job mode' >&2; exit 2 ;;
esac
