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

const protectedReleaseEnvironment = `{"name":"tpa-release","can_admins_bypass":false,"protection_rules":[{"type":"required_reviewers","prevent_self_review":true,"reviewers":[{"type":"Team","reviewer":{"id":42}}]}]}`

// RELEASE-SOURCE: only commits contained in the fresh origin staging history
// are releasable; normal advancement is allowed, missing/rewritten history is not.
func TestReleaseSourceBranch(t *testing.T) {
	workspace := makeGitWorkspace(t)
	base := gitTest(t, workspace, "rev-parse", "HEAD")
	gitTest(t, workspace, "branch", "staging")
	gitTest(t, workspace, "commit", "--allow-empty", "-qm", "candidate")
	commit := gitTest(t, workspace, "rev-parse", "HEAD")
	gitTest(t, workspace, "tag", "v1.2.3", commit)
	gitTest(t, workspace, "branch", "-f", "staging", commit)
	cfg := testConfig()
	cfg.Release.SourceBranch = "staging"
	planCandidate := func() (Plan, error) {
		plan, _, err := CreatePlan(cfg, workspace, "push", "refs/tags/v1.2.3", commit, 1)
		return plan, err
	}
	if _, err := planCandidate(); err == nil {
		t.Fatal("a local branch must not substitute for missing origin/staging")
	}
	gitTest(t, workspace, "update-ref", "refs/remotes/origin/staging", base)
	if _, err := planCandidate(); err == nil {
		t.Fatal("accepted tag on a commit not yet merged into staging")
	}
	gitTest(t, workspace, "update-ref", "refs/remotes/origin/staging", commit)
	plan, err := planCandidate()
	if err != nil || !plan.Publish {
		t.Fatalf("rejected staged candidate: %+v, %v", plan, err)
	}
	if err := ValidatePlanInputs(cfg, workspace, plan); err != nil {
		t.Fatal(err)
	}
	gitTest(t, workspace, "commit", "--allow-empty", "-qm", "next staging change")
	next := gitTest(t, workspace, "rev-parse", "HEAD")
	gitTest(t, workspace, "update-ref", "refs/remotes/origin/staging", next)
	gitTest(t, workspace, "checkout", "--detach", "-q", commit)
	if _, err := planCandidate(); err != nil {
		t.Fatalf("staging advancement invalidated the already-qualified commit: %v", err)
	}
	if err := ValidatePlanInputs(cfg, workspace, plan); err != nil {
		t.Fatalf("staging advancement invalidated the source plan: %v", err)
	}
	gitTest(t, workspace, "update-ref", "refs/remotes/origin/staging", base)
	if err := ValidatePlanInputs(cfg, workspace, plan); err == nil {
		t.Fatal("accepted staging rewrite excluding the candidate at revalidation")
	}
	gitTest(t, workspace, "update-ref", "-d", "refs/remotes/origin/staging")
	if err := ValidatePlanInputs(cfg, workspace, plan); err == nil {
		t.Fatal("accepted deleted staging branch at revalidation")
	}
	// A manual branch run is still qualification-only, without a staging ref.
	qualification, _, err := CreatePlan(cfg, workspace, "workflow_dispatch", "refs/heads/main", commit, 2)
	if err != nil || qualification.Publish || qualification.Release {
		t.Fatalf("branch qualification changed semantics: %+v, %v", qualification, err)
	}
}

func TestReleasePolicyConfigurationNames(t *testing.T) {
	workspace := makeGitWorkspace(t)
	for _, branch := range []string{"", "staging", "release/staging"} {
		cfg := testConfig()
		cfg.Release.SourceBranch = branch
		if err := cfg.Validate(workspace); err != nil {
			t.Errorf("rejected branch %q: %v", branch, err)
		}
	}
	for _, branch := range []string{"-staging", "refs/heads/staging", "../staging", "staging^{commit}", "staging\nx", "staging..old"} {
		cfg := testConfig()
		cfg.Release.SourceBranch = branch
		if err := cfg.Validate(workspace); err == nil {
			t.Errorf("accepted branch %q", branch)
		}
	}
	for _, name := range []string{"", "tpa-release", strings.Repeat("a", 64)} {
		cfg := testConfig()
		cfg.Release.PublishEnvironment = name
		if err := cfg.Validate(workspace); err != nil {
			t.Errorf("rejected environment %q: %v", name, err)
		}
	}
	for _, name := range []string{"../release", "a/b", "release\nx", "-release", strings.Repeat("a", 65)} {
		cfg := testConfig()
		cfg.Release.PublishEnvironment = name
		if err := cfg.Validate(workspace); err == nil {
			t.Errorf("accepted environment %q", name)
		}
	}
}

// RELEASE-APPROVAL: a name alone, wait timer or absent reviewer is not an
// approval policy. The GitHub scheduler, not this API preflight, enforces review.
func TestVerifyReleaseEnvironment(t *testing.T) {
	bin := t.TempDir()
	log := filepath.Join(bin, "requests")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$ENVIRONMENT_TEST_LOG\"\nprintf '%s\\n' \"$ENVIRONMENT_TEST_POLICY\"\nexit \"${ENVIRONMENT_TEST_STATUS:-0}\"\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ENVIRONMENT_TEST_LOG", log)
	cases := []struct {
		name, policy, status string
		wantSuccess          bool
	}{
		{"protected team", protectedReleaseEnvironment, "0", true},
		{"protected user", strings.Replace(protectedReleaseEnvironment, "Team", "User", 1), "0", true},
		{"case insensitive name", strings.Replace(protectedReleaseEnvironment, "tpa-release", "TPA-Release", 1), "0", true},
		{"unprotected", `{"name":"tpa-release","can_admins_bypass":false,"protection_rules":[]}`, "0", false},
		{"self review", strings.Replace(protectedReleaseEnvironment, `"prevent_self_review":true`, `"prevent_self_review":false`, 1), "0", false},
		{"admin bypass", strings.Replace(protectedReleaseEnvironment, `"can_admins_bypass":false`, `"can_admins_bypass":true`, 1), "0", false},
		{"missing bypass policy", strings.Replace(protectedReleaseEnvironment, `"can_admins_bypass":false,`, "", 1), "0", false},
		{"no reviewers", strings.Replace(protectedReleaseEnvironment, `[{"type":"Team","reviewer":{"id":42}}]`, "[]", 1), "0", false},
		{"invalid reviewer", strings.Replace(protectedReleaseEnvironment, "42", "0", 1), "0", false},
		{"unknown reviewer type", strings.Replace(protectedReleaseEnvironment, "Team", "Unknown", 1), "0", false},
		{"timer only", strings.Replace(protectedReleaseEnvironment, "required_reviewers", "wait_timer", 1), "0", false},
		{"wrong environment", strings.Replace(protectedReleaseEnvironment, "tpa-release", "other-release", 1), "0", false},
		{"invalid json", "{", "0", false},
		{"missing or inaccessible", "", "1", false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ENVIRONMENT_TEST_POLICY", tt.policy)
			t.Setenv("ENVIRONMENT_TEST_STATUS", tt.status)
			err := VerifyReleaseEnvironment("example/project", "tpa-release")
			if (err == nil) != tt.wantSuccess {
				t.Fatalf("VerifyReleaseEnvironment: %v; want success %v", err, tt.wantSuccess)
			}
		})
	}
	requests, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(requests)), "\n")
	if len(lines) != len(cases) {
		t.Fatalf("unexpected number of API calls: %s", requests)
	}
	for _, line := range lines {
		if line != "api --method GET repos/example/project/environments/tpa-release" {
			t.Fatalf("environment check was not one explicit read-only request: %s", line)
		}
	}
	for _, invalid := range [][2]string{{"../project", "tpa-release"}, {"example/project", ""}, {"example/project", "../release"}} {
		if err := VerifyReleaseEnvironment(invalid[0], invalid[1]); err == nil {
			t.Fatalf("accepted invalid API input: %q", invalid)
		}
	}
	after, err := os.ReadFile(log)
	if err != nil || string(after) != string(requests) {
		t.Fatal("invalid input reached the GitHub API")
	}
}

// RELEASE-IDENTITY: execute the actual workflow verification shell against an
// isolated fake helper. A mismatched candidate must never reach verify-bundle.
func TestPublishCandidateManifestBinding(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/reusable-deb-release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]struct {
			Steps []struct{ Name, Run string }
		}
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}
	script := ""
	for _, step := range workflow.Jobs["publish"].Steps {
		if step.Name == "Verify complete release bundle" {
			script = step.Run
		}
	}
	if script == "" {
		t.Fatal("missing workflow verification shell")
	}
	root := t.TempDir()
	bundle := filepath.Join(root, "release-bundle")
	if err := os.Mkdir(bundle, 0755); err != nil {
		t.Fatal(err)
	}
	manifest := []byte("{}\n")
	if err := os.WriteFile(filepath.Join(bundle, "release-manifest.json"), manifest, 0644); err != nil {
		t.Fatal(err)
	}
	trace := filepath.Join(root, "trace")
	if err := os.WriteFile(filepath.Join(root, "tpa-release"), []byte("#!/bin/sh\nprintf '%s\\n' \"$1\" >> \"$TEST_TRACE\"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(manifest)
	digest := hex.EncodeToString(hash[:])
	for _, tt := range []struct {
		name, approved string
		success        bool
	}{
		{"exact candidate", digest, true},
		{"different candidate", strings.Repeat("a", 64), false},
		{"missing binding", "", false},
		{"malformed binding", "not-a-sha256", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := os.WriteFile(trace, nil, 0644); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("bash", "-c", script)
			cmd.Env = append(os.Environ(),
				"RUNNER_TEMP="+root, "TEST_TRACE="+trace,
				"APPROVED_MANIFEST_SHA256="+tt.approved,
				"RELEASE_CONFIG=.tpa-release.yml", "EVENT_NAME=push",
				"EVENT_REF=refs/tags/v1.2.3", "RELEASE_COMMIT="+strings.Repeat("b", 40),
				"RUN_NUMBER=1", "PLAN_PATH="+filepath.Join(root, "plan.json"),
				"TPA_RELEASE_TPA_VERSION=1.2.3")
			output, err := cmd.CombinedOutput()
			if (err == nil) != tt.success {
				t.Fatalf("workflow shell success=%v, want %v: %s", err == nil, tt.success, output)
			}
			calls, err := os.ReadFile(trace)
			if err != nil {
				t.Fatal(err)
			}
			want := "plan\n"
			if tt.success {
				want += "verify-bundle\n"
			}
			if string(calls) != want {
				t.Fatalf("unexpected helper calls %q, want %q", calls, want)
			}
		})
	}
}

// RELEASE-IDENTITY: GitHub's gate must guard the sole contents-write job;
// attestation and publication consume the very same immutable candidate ID.
func TestStagingCandidateWorkflowWiring(t *testing.T) {
	root := repositoryRoot(t)
	cfg, _, err := LoadConfig(".tpa-release.yml", root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Release.SourceBranch != "staging" || cfg.Release.PublishEnvironment != "tpa-release" {
		t.Fatalf("TPA must require staged source and independent approval: %+v", cfg.Release)
	}
	data, err := os.ReadFile(filepath.Join(root, ".github/workflows/reusable-deb-release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]struct {
			Permissions map[string]string
			Environment struct{ Name string }
			Outputs     map[string]string
			Steps       []struct {
				Name, ID, Uses, Run string
				With                map[string]any
				Env                 map[string]string
			}
		}
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}
	publish := workflow.Jobs["publish"]
	if publish.Environment.Name != "${{ needs.prepare.outputs.publish_environment || 'release' }}" {
		t.Fatal("publication is not guarded by the planned environment")
	}
	for name, job := range workflow.Jobs {
		if job.Permissions["contents"] == "write" && name != "publish" {
			t.Fatalf("ungated job %s received release write permission", name)
		}
	}
	for _, name := range []string{"prepare", "publish"} {
		if workflow.Jobs[name].Permissions["actions"] != "read" {
			t.Fatalf("%s cannot read the protected environment policy", name)
		}
	}
	for _, name := range []string{"attest", "publish"} {
		found := false
		for _, step := range workflow.Jobs[name].Steps {
			if strings.HasPrefix(step.Uses, "actions/download-artifact@") && step.Name == "Download complete release bundle" {
				found = true
				if step.With["artifact-ids"] != "${{ needs.bundle.outputs.artifact_id }}" || step.With["merge-multiple"] != true {
					t.Fatalf("%s does not consume the immutable candidate artifact: %+v", name, step.With)
				}
			}
		}
		if !found {
			t.Fatalf("%s has no candidate artifact download", name)
		}
	}
	manifestBound := false
	for _, step := range publish.Steps {
		if step.Env["APPROVED_MANIFEST_SHA256"] == "${{ needs.bundle.outputs.manifest_sha256 }}" && strings.Contains(step.Run, "manifest_sum%% *") {
			manifestBound = true
		}
		if strings.Contains(step.Run, "go build") || strings.Contains(step.Run, " build ") || strings.Contains(step.Run, "--phase build") || strings.Contains(step.Run, `" package `) {
			t.Fatal("publication must not rebuild Debian packages")
		}
	}
	if !manifestBound {
		t.Fatal("publication does not bind the reviewed candidate manifest")
	}
	// New optional policy fields must not change legacy configuration hashes.
	encoded, err := json.Marshal(testConfig())
	if err != nil || strings.Contains(string(encoded), "source_branch") || strings.Contains(string(encoded), "publish_environment") {
		t.Fatalf("empty policy fields changed legacy configuration serialization: %s, %v", encoded, err)
	}
}
