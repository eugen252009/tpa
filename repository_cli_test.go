package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/eugen252009/tpa/internals/aptpackage"
)

func TestInspectAndVerifyCommandsEmitJSON(t *testing.T) {
	_, repo, _ := makeCLILifecycleRepository(t)
	var stdout, stderr bytes.Buffer
	args := []string{"inspect", repo, "--json", "-package=fixture", "-ver=1.0", "-arch=all"}
	if code := run(args, bytes.NewReader(nil), &stdout, &stderr); code != 0 {
		t.Fatalf("inspect exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var inspected aptpackage.RepositoryReport
	if err := json.Unmarshal(stdout.Bytes(), &inspected); err != nil {
		t.Fatalf("inspect JSON: %v: %s", err, stdout.String())
	}
	if inspected.PackageCount != 1 || len(inspected.Packages) != 1 || inspected.Packages[0].Package != "fixture" {
		t.Fatalf("unexpected inspect JSON: %+v", inspected)
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"verify", "-json", repo}, bytes.NewReader(nil), &stdout, &stderr); code != 0 {
		t.Fatalf("verify exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var verified aptpackage.RepositoryVerifyResult
	if err := json.Unmarshal(stdout.Bytes(), &verified); err != nil {
		t.Fatalf("verify JSON: %v: %s", err, stdout.String())
	}
	if !verified.Valid || verified.PackageCount != 1 {
		t.Fatalf("unexpected verify JSON: %+v", verified)
	}
}

func TestVerifyJSONReportsFailureWithoutHumanDiagnostics(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"verify", "--json", "./definitely-not-a-repository"}, bytes.NewReader(nil), &stdout, &stderr); code == 0 {
		t.Fatal("verify succeeded for missing repository")
	}
	var result aptpackage.RepositoryVerifyResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("invalid error JSON %q: %v", stdout.String(), err)
	}
	if result.Valid || result.Error == "" || stderr.Len() != 0 {
		t.Fatalf("unexpected failure response: %+v stderr=%q", result, stderr.String())
	}
}

func TestInspectRequiresCompleteExactIdentity(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"inspect", "-package=fixture", "./missing"}, bytes.NewReader(nil), &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "requires -package, -ver, and -arch") {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
}
