package aptpackage

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func buildLifecycleRepository(t *testing.T, signingKey string) (string, Config, map[PackageIdentity]string) {
	t.Helper()
	root := t.TempDir()
	input := filepath.Join(root, "packages")
	identities := map[PackageIdentity]string{
		{Package: "foo", Version: "1.2.2", Architecture: "amd64"}: "foo_1.2.2_amd64.deb",
		{Package: "foo", Version: "1.2.3", Architecture: "amd64"}: "foo_1.2.3_amd64.deb",
		{Package: "foo", Version: "1.2.3", Architecture: "arm64"}: "foo_1.2.3_arm64.deb",
		{Package: "bar", Version: "4.0.0", Architecture: "amd64"}: "bar_4.0.0_amd64.deb",
	}
	for identity, filename := range identities {
		control := basicControl(identity.Package, identity.Version, identity.Architecture)
		if identity.Package == "bar" {
			control += "Custom-Field: first line\n continuation line\n"
		}
		buildTestDeb(t, input, filename, control, filename)
	}
	repository := filepath.Join(root, "repository")
	cfg := Config{
		InDir: input, OutDir: repository, GPG: signingKey,
		Repo: RepoConfig{Origin: "Lifecycle-Test", Label: "Lifecycle-Test", Suite: "stable", Components: "main", Codename: "stable"},
	}
	if err := Pack(cfg); err != nil {
		t.Fatal(err)
	}
	return repository, cfg, identities
}

func lifecyclePoolPath(repository string, identity PackageIdentity, filename string) string {
	return filepath.Join(repository, "pool", "main", identity.Package[:1], identity.Package, filename)
}

func readLifecycleIndex(t *testing.T, repository string, architecture string) ([]byte, []rawControlStanza) {
	t.Helper()
	path := filepath.Join(repository, "dists", "stable", "main", "binary-"+architecture, "Packages")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	stanzas, err := parseRawControlStanzas(data)
	if err != nil {
		t.Fatal(err)
	}
	return data, stanzas
}

func containsLifecycleIdentity(stanzas []rawControlStanza, identity PackageIdentity) bool {
	for _, stanza := range stanzas {
		if stanza.fields["package"] == identity.Package && stanza.fields["version"] == identity.Version && stanza.fields["architecture"] == identity.Architecture {
			return true
		}
	}
	return false
}

func TestUnlistRemovesOnlyExactIdentityAndRetainsArtifact(t *testing.T) {
	repository, cfg, identities := buildLifecycleRepository(t, "")
	arm64Before, _ := os.ReadFile(filepath.Join(repository, "dists", "stable", "main", "binary-arm64", "Packages"))
	arm64GzipBefore, _ := os.ReadFile(filepath.Join(repository, "dists", "stable", "main", "binary-arm64", "Packages.gz"))
	target := PackageIdentity{Package: "foo", Version: "1.2.3", Architecture: "amd64"}
	artifact := lifecyclePoolPath(repository, target, identities[target])
	artifactBefore, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if err := Unlist(cfg, repository, target); err != nil {
		t.Fatal(err)
	}
	artifactAfter, err := os.ReadFile(artifact)
	if err != nil || !bytes.Equal(artifactBefore, artifactAfter) {
		t.Fatalf("unlist removed or changed the artifact: %v", err)
	}
	_, amd64 := readLifecycleIndex(t, repository, "amd64")
	_, arm64 := readLifecycleIndex(t, repository, "arm64")
	for identity, want := range map[PackageIdentity]bool{
		{Package: "foo", Version: "1.2.2", Architecture: "amd64"}: true,
		target: false,
		{Package: "bar", Version: "4.0.0", Architecture: "amd64"}: true,
	} {
		if got := containsLifecycleIdentity(amd64, identity); got != want {
			t.Errorf("amd64 contains %s = %v, want %v", identity, got, want)
		}
	}
	for _, stanza := range amd64 {
		if stanza.fields["package"] == "bar" && !bytes.Contains(stanza.raw, []byte("Custom-Field: first line\n continuation line\n")) {
			t.Fatal("remaining stanza's custom continuation metadata was not preserved")
		}
	}
	armTarget := PackageIdentity{Package: "foo", Version: "1.2.3", Architecture: "arm64"}
	if !containsLifecycleIdentity(arm64, armTarget) {
		t.Fatal("unlisting amd64 also removed the arm64 identity")
	}
	arm64After, _ := os.ReadFile(filepath.Join(repository, "dists", "stable", "main", "binary-arm64", "Packages"))
	arm64GzipAfter, _ := os.ReadFile(filepath.Join(repository, "dists", "stable", "main", "binary-arm64", "Packages.gz"))
	if !bytes.Equal(arm64Before, arm64After) || !bytes.Equal(arm64GzipBefore, arm64GzipAfter) {
		t.Fatal("unaffected architecture index bytes changed")
	}
	cfg.OutDir = repository
	if err := verifyRepository(cfg); err != nil {
		t.Fatalf("unlisted repository does not verify: %v", err)
	}
}

func TestConcurrentUnlistsSerializeWithoutLostUpdates(t *testing.T) {
	repository, cfg, _ := buildLifecycleRepository(t, "")
	identities := []PackageIdentity{
		{Package: "foo", Version: "1.2.3", Architecture: "amd64"},
		{Package: "bar", Version: "4.0.0", Architecture: "amd64"},
	}
	results := make(chan error, len(identities))
	for _, identity := range identities {
		identity := identity
		go func() { results <- Unlist(cfg, repository, identity) }()
	}
	var firstErr error
	for range identities {
		if err := <-results; err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		t.Fatal(firstErr)
	}
	_, stanzas := readLifecycleIndex(t, repository, "amd64")
	for _, identity := range identities {
		if containsLifecycleIdentity(stanzas, identity) {
			t.Errorf("concurrent unlist lost update for %s", identity)
		}
	}
	cfg.OutDir = repository
	if err := verifyRepository(cfg); err != nil {
		t.Fatalf("repository after concurrent unlists does not verify: %v", err)
	}
}

func TestUnlistLastPackageLeavesValidEmptyIndexAndRetainsArtifact(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "packages")
	identity := PackageIdentity{Package: "only", Version: "1.0", Architecture: "all"}
	deb := buildTestDeb(t, input, "only_1.0_all.deb", basicControl(identity.Package, identity.Version, identity.Architecture), "last package")
	repository := filepath.Join(root, "repository")
	cfg := Config{InDir: input, OutDir: repository}
	if err := Pack(cfg); err != nil {
		t.Fatal(err)
	}
	if err := Unlist(cfg, repository, identity); err != nil {
		t.Fatal(err)
	}
	packagesPath := filepath.Join(repository, "dists", "stable", "main", "binary-all", "Packages")
	data, err := os.ReadFile(packagesPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 0 {
		t.Fatalf("last identity remains in Packages: %q", data)
	}
	cfg.OutDir = repository
	if err := verifyRepository(cfg); err != nil {
		t.Fatalf("empty but valid index failed repository verification: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repository, "pool", "main", "o", "only", filepath.Base(deb))); err != nil {
		t.Fatalf("artifact for last package was removed by unlist: %v", err)
	}
}

func TestUnlistUnknownIdentityFailsWithoutChangingRepository(t *testing.T) {
	repository, cfg, _ := buildLifecycleRepository(t, "")
	before := snapshotLifecycleTree(t, repository)
	err := Unlist(cfg, repository, PackageIdentity{Package: "missing", Version: "9", Architecture: "amd64"})
	if err == nil || !strings.Contains(err.Error(), "is not listed") {
		t.Fatalf("expected clear missing-identity error, got %v", err)
	}
	assertLifecycleTreeEqual(t, repository, before)
}

func TestUnlistSignedRepositoryResignsAndVerifies(t *testing.T) {
	if _, err := exec.LookPath("gpg"); err != nil {
		t.Skip("gpg is required")
	}
	gpgHome := filepath.Join(t.TempDir(), "gnupg")
	if err := os.Mkdir(gpgHome, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GNUPGHOME", gpgHome)
	key := generateTestSigningKey(t, "Lifecycle signer <lifecycle@example.invalid>")
	repository, cfg, _ := buildLifecycleRepository(t, key)
	oldInRelease, err := os.ReadFile(filepath.Join(repository, "dists", "stable", "InRelease"))
	if err != nil {
		t.Fatal(err)
	}
	target := PackageIdentity{Package: "foo", Version: "1.2.3", Architecture: "amd64"}
	if err := Unlist(cfg, repository, target); err != nil {
		t.Fatal(err)
	}
	newInRelease, err := os.ReadFile(filepath.Join(repository, "dists", "stable", "InRelease"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(oldInRelease, newInRelease) {
		t.Fatal("signed metadata was not regenerated")
	}
	cfg.OutDir = repository
	if err := verifyRepository(cfg); err != nil {
		t.Fatalf("signed lifecycle repository does not verify: %v", err)
	}
	_, stanzas := readLifecycleIndex(t, repository, "amd64")
	if containsLifecycleIdentity(stanzas, target) {
		t.Fatal("removed identity remains in signed Packages")
	}
}

func TestDeleteRefusesArtifactStillReferencedByAnotherIdentity(t *testing.T) {
	repository, cfg, _ := buildLifecycleRepository(t, "")
	target := PackageIdentity{Package: "foo", Version: "1.2.3", Architecture: "amd64"}
	packagesPath := filepath.Join(repository, "dists", "stable", "main", "binary-amd64", "Packages")
	data, stanzas := readLifecycleIndex(t, repository, "amd64")
	var targetStanza map[string]string
	for _, stanza := range stanzas {
		if stanza.fields["package"] == target.Package && stanza.fields["version"] == target.Version && stanza.fields["architecture"] == target.Architecture {
			targetStanza = stanza.fields
		}
	}
	if targetStanza == nil {
		t.Fatal("fixture target not found")
	}
	alias := fmt.Sprintf("Package: shadow\nVersion: 9.0\nArchitecture: amd64\nMaintainer: Test\nDescription: shared artifact\nFilename: %s\nSize: %s\nSHA256: %s\n\n", targetStanza["filename"], targetStanza["size"], targetStanza["sha256"])
	if err := os.WriteFile(packagesPath, append(data, []byte(alias)...), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := gzipFile(packagesPath); err != nil {
		t.Fatal(err)
	}
	if err := rewriteLifecycleRelease(cfg, repository); err != nil {
		t.Fatal(err)
	}
	cfg.OutDir = repository
	if err := verifyRepository(cfg); err != nil {
		t.Fatalf("shared-artifact fixture does not verify: %v", err)
	}
	before := snapshotLifecycleTree(t, repository)
	_, err := Delete(cfg, repository, target)
	if err == nil || !strings.Contains(err.Error(), "still referenced by Packages identity shadow 9.0 amd64") {
		t.Fatalf("expected shared reference refusal, got %v", err)
	}
	assertLifecycleTreeEqual(t, repository, before)
}

func TestDeleteListedPackageUnlistsThenRemovesArtifact(t *testing.T) {
	repository, cfg, identities := buildLifecycleRepository(t, "")
	target := PackageIdentity{Package: "foo", Version: "1.2.3", Architecture: "amd64"}
	artifact := lifecyclePoolPath(repository, target, identities[target])
	result, err := Delete(cfg, repository, target)
	if err != nil {
		t.Fatal(err)
	}
	if !result.WasListed {
		t.Fatal("delete did not report that the package was unlisted")
	}
	if _, err := os.Stat(artifact); !os.IsNotExist(err) {
		t.Fatalf("deleted artifact still exists (err=%v)", err)
	}
	_, stanzas := readLifecycleIndex(t, repository, "amd64")
	if containsLifecycleIdentity(stanzas, target) {
		t.Fatal("deleted package remains listed")
	}
	cfg.OutDir = repository
	if err := verifyRepository(cfg); err != nil {
		t.Fatalf("repository after delete does not verify: %v", err)
	}
}

func TestDeleteAlreadyUnlistedPackageRemovesArtifactOnly(t *testing.T) {
	repository, cfg, identities := buildLifecycleRepository(t, "")
	target := PackageIdentity{Package: "foo", Version: "1.2.3", Architecture: "amd64"}
	if err := Unlist(cfg, repository, target); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(repository, "dists", "stable", "main", "binary-amd64", "Packages"))
	result, err := Delete(cfg, repository, target)
	if err != nil {
		t.Fatal(err)
	}
	if result.WasListed {
		t.Fatal("already-unlisted delete reported a metadata transition")
	}
	after, _ := os.ReadFile(filepath.Join(repository, "dists", "stable", "main", "binary-amd64", "Packages"))
	if !bytes.Equal(before, after) {
		t.Fatal("delete of an already-unlisted artifact changed metadata")
	}
	if _, err := os.Stat(lifecyclePoolPath(repository, target, identities[target])); !os.IsNotExist(err) {
		t.Fatalf("artifact remains after delete (err=%v)", err)
	}
}

func TestDeleteSigningFailureLeavesOldRepositoryAndArtifact(t *testing.T) {
	if _, err := exec.LookPath("gpg"); err != nil {
		t.Skip("gpg is required")
	}
	gpgHome := filepath.Join(t.TempDir(), "gnupg")
	if err := os.Mkdir(gpgHome, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GNUPGHOME", gpgHome)
	key := generateTestSigningKey(t, "Delete signer <delete@example.invalid>")
	repository, cfg, identities := buildLifecycleRepository(t, key)
	target := PackageIdentity{Package: "foo", Version: "1.2.3", Architecture: "amd64"}
	before := snapshotLifecycleTree(t, repository)
	artifact := lifecyclePoolPath(repository, target, identities[target])
	oldSigner := signLifecycleCandidate
	signLifecycleCandidate = func(Config, string, string) error { return errors.New("injected signing failure") }
	t.Cleanup(func() { signLifecycleCandidate = oldSigner })
	_, err := Delete(cfg, repository, target)
	if err == nil || !strings.Contains(err.Error(), "injected signing failure") {
		t.Fatalf("expected signing failure, got %v", err)
	}
	assertLifecycleTreeEqual(t, repository, before)
	if _, err := os.Stat(artifact); err != nil {
		t.Fatalf("artifact was removed before successful signing: %v", err)
	}
	cfg.OutDir = repository
	if err := verifyRepository(cfg); err != nil {
		t.Fatalf("old signed repository became invalid: %v", err)
	}
}

func TestDeleteMetadataPublicationFailureLeavesOldRepositoryAndArtifact(t *testing.T) {
	repository, cfg, identities := buildLifecycleRepository(t, "")
	target := PackageIdentity{Package: "foo", Version: "1.2.3", Architecture: "amd64"}
	before := snapshotLifecycleTree(t, repository)
	artifact := lifecyclePoolPath(repository, target, identities[target])
	oldHook := beforeLifecyclePublish
	beforeLifecyclePublish = func(string) error { return errors.New("injected publication failure") }
	t.Cleanup(func() { beforeLifecyclePublish = oldHook })
	_, err := Delete(cfg, repository, target)
	if err == nil || !strings.Contains(err.Error(), "injected publication failure") {
		t.Fatalf("expected injected publication failure, got %v", err)
	}
	assertLifecycleTreeEqual(t, repository, before)
	if _, err := os.Stat(artifact); err != nil {
		t.Fatalf("artifact was removed before metadata publication: %v", err)
	}
	cfg.OutDir = repository
	if err := verifyRepository(cfg); err != nil {
		t.Fatalf("old repository became invalid: %v", err)
	}
}

func TestDeleteArtifactCleanupFailureLeavesSafeUnlistedState(t *testing.T) {
	repository, cfg, identities := buildLifecycleRepository(t, "")
	target := PackageIdentity{Package: "foo", Version: "1.2.3", Architecture: "amd64"}
	artifact := lifecyclePoolPath(repository, target, identities[target])
	oldRemove := removeLifecycleArtifact
	removeLifecycleArtifact = func(string) error { return errors.New("injected artifact removal failure") }
	t.Cleanup(func() { removeLifecycleArtifact = oldRemove })
	result, err := Delete(cfg, repository, target)
	if err == nil || !strings.Contains(err.Error(), "was unlisted, but artifact deletion failed") {
		t.Fatalf("expected distinct cleanup failure, got %v", err)
	}
	if !result.WasListed {
		t.Fatal("partial result did not report successful unlisting")
	}
	if _, err := os.Stat(artifact); err != nil {
		t.Fatalf("artifact should remain after cleanup failure: %v", err)
	}
	_, stanzas := readLifecycleIndex(t, repository, "amd64")
	if containsLifecycleIdentity(stanzas, target) {
		t.Fatal("package remains listed after cleanup failure")
	}
	cfg.OutDir = repository
	if err := verifyRepository(cfg); err != nil {
		t.Fatalf("unlisted repository is invalid after cleanup failure: %v", err)
	}
}

func TestDeleteMissingIdentityAndArtifactFails(t *testing.T) {
	repository, cfg, _ := buildLifecycleRepository(t, "")
	before := snapshotLifecycleTree(t, repository)
	_, err := Delete(cfg, repository, PackageIdentity{Package: "absent", Version: "1", Architecture: "amd64"})
	if err == nil || !strings.Contains(err.Error(), "package artifact not found") {
		t.Fatalf("expected missing artifact failure, got %v", err)
	}
	assertLifecycleTreeEqual(t, repository, before)
}

func snapshotLifecycleTree(t *testing.T, root string) map[string][]byte {
	t.Helper()
	snapshot := make(map[string][]byte)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		snapshot[rel] = data
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func assertLifecycleTreeEqual(t *testing.T, root string, expected map[string][]byte) {
	t.Helper()
	actual := snapshotLifecycleTree(t, root)
	if len(actual) != len(expected) {
		t.Fatalf("repository file count changed: got %d, want %d", len(actual), len(expected))
	}
	for path, want := range expected {
		if got, ok := actual[path]; !ok || !bytes.Equal(got, want) {
			t.Errorf("repository file %s changed", path)
		}
	}
}
