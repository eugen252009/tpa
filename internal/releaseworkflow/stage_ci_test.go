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

// The actual workflow shell adapter is exercised, not a duplicate Go validator.
func TestAPTStageCIScriptBindsQualifiedBytesAndCompleteManifest(t *testing.T) {
	root := t.TempDir()
	payload := []byte("qualified package bytes")
	sum := sha256.Sum256(payload)
	a := map[string]any{"filename": "proof_1_all.deb", "package": "proof", "version": "1", "architecture": "all", "size": len(payload), "sha256": hex.EncodeToString(sum[:])}
	manifest, _ := json.Marshal(map[string]any{"artifacts": []any{a}})
	hash := sha256.Sum256(manifest)
	manifestPath := filepath.Join(root, "release-manifest.json")
	os.WriteFile(manifestPath, manifest, 0600)
	packagePath := filepath.Join(root, "proof_1_all.deb")
	os.WriteFile(packagePath, payload, 0600)
	trace := filepath.Join(root, "calls")
	reply := filepath.Join(root, "reply.json")
	helper := filepath.Join(root, "fake-tpa")
	os.WriteFile(helper, []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$TEST_TRACE\"\nif [ \"$2\" = inspect ]; then /bin/cat \"$TEST_REPLY\"; fi\n"), 0700)
	invoke := func(mode, approved string) error {
		cmd := exec.Command("bash", filepath.Join(repositoryRoot(t), "scripts/github-apt-stage.sh"), mode)
		cmd.Env = append(os.Environ(), "RUNNER_TEMP="+root, "RELEASE_BUNDLE="+root, "TPA_BIN="+helper, "APPROVED_MANIFEST_SHA256="+approved, "APT_DESTINATION=tparun+ssh://tparun@fixture/"+strings.Repeat("a", 32), "APT_CHANNEL=stable", "STAGE_ID="+strings.Repeat("b", 32), "STAGE_SSH_KEY=synthetic-test-key", "APT_KNOWN_HOSTS=synthetic-host-pin", "TARGET_FILENAME=proof_1_all.deb", "TEST_TRACE="+trace, "TEST_REPLY="+reply)
		_, err := cmd.CombinedOutput()
		return err
	}
	digest := hex.EncodeToString(hash[:])
	if err := invoke("upload", digest); err != nil {
		t.Fatal("exact qualified artifact rejected", err)
	}
	calls, _ := os.ReadFile(trace)
	if !strings.Contains(string(calls), "stage upload") || !strings.Contains(string(calls), "-in="+packagePath) {
		t.Fatal("did not upload the exact manifest-selected artifact", string(calls))
	}
	os.Remove(trace)
	os.WriteFile(packagePath, []byte("replaced package bytes"), 0600)
	if err := invoke("upload", digest); err == nil {
		t.Fatal("replacement artifact accepted")
	}
	if _, err := os.Stat(trace); !os.IsNotExist(err) {
		t.Fatal("replaced artifact reached transfer")
	}
	os.Remove(packagePath)
	if err := invoke("upload", digest); err == nil {
		t.Fatal("missing required artifact accepted")
	}
	if err := invoke("upload", strings.Repeat("e", 64)); err == nil {
		t.Fatal("wrong approved manifest accepted")
	}
	a["state"] = "ready"
	response := map[string]any{"stage": map[string]any{"manifest_sha256": digest, "state": "OPEN", "artifacts": []any{a}}}
	raw, _ := json.Marshal(response)
	os.WriteFile(reply, raw, 0600)
	if err := invoke("verify", digest); err != nil {
		t.Fatal("complete staged manifest rejected", err)
	}
	a["state"] = "expected"
	raw, _ = json.Marshal(response)
	os.WriteFile(reply, raw, 0600)
	if err := invoke("verify", digest); err == nil {
		t.Fatal("missing staged upload passed approval preflight")
	}
	a["state"] = "ready"
	a["sha256"] = strings.Repeat("f", 64)
	raw, _ = json.Marshal(response)
	os.WriteFile(reply, raw, 0600)
	if err := invoke("verify", digest); err == nil {
		t.Fatal("substituted staged upload passed approval preflight")
	}
}
func TestAPTWorkflowUploadBarrierAndNoBuildAfterApproval(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/reusable-deb-release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var w struct {
		Jobs map[string]struct {
			Needs    any
			If       string
			Strategy struct{ Matrix map[string]string }
			Env      map[string]string
			Steps    []struct {
				Name, Run, Uses string
				Env             map[string]string
			}
		}
	}
	if err = yaml.Unmarshal(raw, &w); err != nil {
		t.Fatal(err)
	}
	upload := w.Jobs["stage-upload"]
	if upload.Strategy.Matrix["filename"] != "${{ fromJSON(needs.bundle.outputs.artifact_filenames) }}" {
		t.Fatal("uploads not enumerated from the complete qualified bundle")
	}
	contains := func(list any, want string) bool {
		if s, ok := list.(string); ok {
			return s == want
		}
		for _, s := range list.([]any) {
			if s == want {
				return true
			}
		}
		return false
	}
	if !contains(w.Jobs["stage-verify"].Needs, "stage-upload") || !contains(w.Jobs["publish"].Needs, "stage-verify") {
		t.Fatal("publication can bypass the complete upload barrier")
	}
	if !strings.Contains(w.Jobs["publish"].If, "needs.stage-verify.result == 'success'") {
		t.Fatal("failed/missing upload may reach approval")
	}
	for name, job := range w.Jobs {
		for _, step := range job.Steps {
			if name == "publish" && (strings.Contains(step.Run, "go ") || strings.Contains(step.Run, "dpkg-deb --build") || strings.Contains(step.Uses, "setup-go")) {
				t.Fatal("post-approval job contains build tooling", step.Name)
			}
			for key, value := range step.Env {
				if name != "publish" && (strings.Contains(key, "PRIVATE_KEY") || strings.Contains(value, "SIGNING_PRIVATE_KEY")) {
					t.Fatal("signing key exposed before approval", name)
				}
			}
		}
	}
}
