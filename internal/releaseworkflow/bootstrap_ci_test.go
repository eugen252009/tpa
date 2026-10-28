package releaseworkflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// BOOTSTRAP-TRUST: reuse the existing owner agent without exporting its key,
// killing its agent, deleting its home, or changing the APT release identity.
func TestAPTCISigningWithExistingOwnerHome(t *testing.T) {
	root := t.TempDir()
	owner := filepath.Join(root, "owner")
	if err := os.Mkdir(owner, 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(owner, "existing-owner-state")
	if err := os.WriteFile(marker, []byte("retain"), 0600); err != nil {
		t.Fatal(err)
	}
	artifact := map[string]any{"filename": "proof_1_all.deb", "package": "proof", "version": "1", "architecture": "all", "size": 1, "sha256": strings.Repeat("a", 64)}
	manifest, _ := json.Marshal(map[string]any{"artifacts": []any{artifact}})
	if err := os.WriteFile(filepath.Join(root, "release-manifest.json"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(manifest)
	artifact["state"] = "ready"
	reply, _ := json.Marshal(map[string]any{"stage": map[string]any{"manifest_sha256": hex.EncodeToString(digest[:]), "state": "OPEN", "artifacts": []any{artifact}}})
	trace := filepath.Join(root, "trace")
	for name, script := range map[string]string{
		"tpa":     "#!/bin/sh\nif [ \"$2\" = inspect ]; then printf '%s' \"$TEST_REPLY\"; else printf 'home=%s\\n' \"$GNUPGHOME\" >> \"$TEST_TRACE\"; printf '%s\\n' \"$@\" >> \"$TEST_TRACE\"; exit \"${TEST_SIGN_EXIT:-0}\"; fi\n",
		"gpg":     "#!/bin/sh\nprintf 'gpg %s\\n' \"$*\" >> \"$TEST_TRACE\"\nprintf public-keyring-fixture\n",
		"gpgconf": "#!/bin/sh\nprintf 'gpgconf %s\\n' \"$*\" >> \"$TEST_TRACE\"\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, tt := range []struct {
		name, home, signExit string
		ok                   bool
	}{
		{"existing agent", owner, "0", true},
		{"signing failure preserves agent", owner, "1", false},
		{"relative home rejected", "owner", "0", false},
		{"absent home rejected", filepath.Join(root, "absent"), "0", false},
		{"managed home still requires secret", "", "0", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := os.WriteFile(trace, nil, 0600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("bash", filepath.Join(repositoryRoot(t), "scripts/github-apt-stage.sh"), "sign")
			cmd.Env = append(os.Environ(), "PATH="+root+":"+os.Getenv("PATH"), "RUNNER_TEMP="+root, "RELEASE_BUNDLE="+root,
				"TPA_BIN="+filepath.Join(root, "tpa"), "APT_DESTINATION=tparun+ssh://tparun@fixture/"+strings.Repeat("b", 32), "APT_CHANNEL=stable",
				"APPROVED_MANIFEST_SHA256="+hex.EncodeToString(digest[:]), "STAGE_SSH_KEY=synthetic-fixture", "APT_KNOWN_HOSTS=synthetic-fixture",
				"APT_PUBLIC_KEY=synthetic-public-key", "APT_SIGNING_FINGERPRINT="+strings.Repeat("C", 40), "APT_SIGNING_PRIVATE_KEY=",
				"APT_SIGNING_HOME="+tt.home, "APT_ORIGIN=TPA", "APT_LABEL=TPA Bootstrap", "APT_DESCRIPTION=Canonical TPA bootstrap repository",
				"TEST_REPLY="+string(reply), "TEST_TRACE="+trace, "TEST_SIGN_EXIT="+tt.signExit)
			output, err := cmd.CombinedOutput()
			if (err == nil) != tt.ok {
				t.Fatalf("success=%v, want %v: %s", err == nil, tt.ok, output)
			}
			calls, err := os.ReadFile(trace)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(calls), "--import") {
				t.Fatal("imported key material despite existing agent or missing secret", string(calls))
			}
			if tt.home != "" && strings.Contains(string(calls), "gpgconf") {
				t.Fatal("killed an existing owner agent", string(calls))
			}
			if tt.home == owner {
				for _, required := range []string{"home=" + owner, "-origin=TPA\n", "-label=TPA Bootstrap\n", "-repo-description=Canonical TPA bootstrap repository\n"} {
					if !strings.Contains(string(calls), required) {
						t.Fatalf("missing %q: %s", required, calls)
					}
				}
			} else if strings.Contains(string(calls), "\nsign\n") {
				t.Fatal("invalid signing configuration reached finalization")
			}
			if data, err := os.ReadFile(marker); err != nil || string(data) != "retain" {
				t.Fatal("existing owner home was modified", err)
			}
		})
	}
}

// BOOTSTRAP-CHANNEL: final TPA tags cannot silently become GitHub-only releases;
// opt-in staging must not reuse the canonical public bootstrap destination.
func TestTPABootstrapWorkflowBinding(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/tpa-release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]struct {
			Needs string
			With  map[string]string
			Steps []struct{ Run string }
		}
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}
	bootstrap := "tparun+ssh://tparun@tpa.run:2222/e47afe7164e1147e5c715e5885d7cee0"
	release := workflow.Jobs["release"]
	if release.Needs != "bootstrap-policy" || len(workflow.Jobs["bootstrap-policy"].Steps) != 1 {
		t.Fatal("bootstrap isolation policy can be bypassed")
	}
	for name, expected := range map[string]string{
		"apt-destination":         "${{ !contains(github.ref_name, '-') && '" + bootstrap + "' || vars.TPA_APT_STAGING_DESTINATION }}",
		"apt-signing-fingerprint": "${{ !contains(github.ref_name, '-') && 'EDCEE63AA803737C8ABBC1E4A34190A2829D5E54' || vars.TPA_APT_STAGING_FINGERPRINT }}",
		"apt-origin":              "TPA",
		"apt-label":               "${{ !contains(github.ref_name, '-') && 'TPA Bootstrap' || 'TPA Staging' }}",
		"apt-description":         "${{ !contains(github.ref_name, '-') && 'Canonical TPA bootstrap repository' || 'Opt-in TPA qualification repository' }}",
	} {
		if release.With[name] != expected {
			t.Fatalf("%s changed bootstrap identity/channel: %s", name, release.With[name])
		}
	}
	for _, destination := range []string{"", bootstrap, "tparun+ssh://tparun@fixture/" + strings.Repeat("b", 32)} {
		cmd := exec.Command("bash", "-c", workflow.Jobs["bootstrap-policy"].Steps[0].Run)
		cmd.Env = append(os.Environ(), "STAGING_DESTINATION="+destination)
		output, err := cmd.CombinedOutput()
		if (err == nil) != (destination != bootstrap) {
			t.Fatalf("staging destination %q: %v: %s", destination, err, output)
		}
	}
}
