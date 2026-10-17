package releaseworkflow

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var githubRepositoryRE = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

type PublishOptions struct {
	Workspace, BundleDir, Repository string
	Config                           Config
	Plan                             Plan
	TPAVersion                       string
}

type githubRelease struct {
	ID      int64  `json:"id"`
	TagName string `json:"tag_name"`
	Draft   bool   `json:"draft"`
	Name    string `json:"name"`
	Body    string `json:"body"`
	Assets  []struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
	} `json:"assets"`
}

func PublishGitHubRelease(options PublishOptions) error {
	if err := ValidatePlanInputs(options.Config, options.Workspace, options.Plan); err != nil {
		return err
	}
	if err := ValidateWorkspaceChanges(options.Config, options.Workspace); err != nil {
		return err
	}
	if !githubRepositoryRE.MatchString(options.Repository) || strings.Contains(options.Repository, "..") {
		return fmt.Errorf("GitHub repository must have owner/name form")
	}
	if !options.Plan.Publish || options.Plan.Tag == "" {
		return fmt.Errorf("GitHub publication is permitted only for a validated tag push")
	}
	if err := VerifyBundle(options.Config, options.Plan, options.BundleDir, options.TPAVersion, os.Getenv("TPA_RELEASE_TPA_COMMIT")); err != nil {
		return fmt.Errorf("refusing to publish invalid bundle: %w", err)
	}
	manifestBytes, err := readRegularFile(filepath.Join(options.BundleDir, "release-manifest.json"))
	if err != nil {
		return err
	}
	var manifest ReleaseManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return err
	}
	if manifest.SourceRepo != options.Repository {
		return fmt.Errorf("bundle source repository %q does not match publisher repository %q", manifest.SourceRepo, options.Repository)
	}
	notes, err := readRegularFile(filepath.Join(options.BundleDir, "release-notes.md"))
	if err != nil {
		return fmt.Errorf("read bundled release notes: %w", err)
	}
	sentinel := draftSentinel(options.Config, options.Plan, manifest.ReleaseNotesSHA256)
	if err := verifyRemoteTag(options.Repository, options.Plan.Tag, options.Plan.SourceCommit); err != nil {
		return err
	}
	assets, err := releaseAssets(options.BundleDir)
	if err != nil {
		return err
	}
	assetProjection, err := os.MkdirTemp("", "tpa-github-release-assets-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(assetProjection)
	assets, err = projectGitHubReleaseAssets(options.BundleDir, assets, assetProjection)
	if err != nil {
		return err
	}
	release, exists, err := getRelease(options.Repository, options.Plan.Tag)
	if err != nil {
		return err
	}
	if !exists {
		createErr := createDraftRelease(options, sentinel)
		// GitHub's release collection can lag behind successful draft creation.
		// Concurrent identical workflow runs may also create the draft first.
		release, exists, err = waitForGitHubRelease(options.Repository, options.Plan.Tag)
		if err != nil {
			return fmt.Errorf("read draft release after creation: %w", err)
		}
		if !exists {
			if createErr != nil {
				return fmt.Errorf("draft release could not be found after creation race: %w", createErr)
			}
			return fmt.Errorf("draft release was not readable after creation")
		}
	}
	if release.TagName != options.Plan.Tag {
		return fmt.Errorf("existing GitHub release tag mismatch: got %q", release.TagName)
	}
	expectedTitle := options.Config.Project.Name + " " + options.Plan.Version
	if release.Name != expectedTitle || !releaseBodyMatches(release.Body, notes, sentinel) {
		return fmt.Errorf("existing GitHub release does not match this release identity or notes; refusing mutation")
	}
	remoteNames := make(map[string]int64, len(release.Assets))
	for _, asset := range release.Assets {
		if _, duplicate := remoteNames[asset.Name]; duplicate {
			return fmt.Errorf("GitHub release contains duplicate asset name %q", asset.Name)
		}
		remoteNames[asset.Name] = asset.Size
	}
	localNames := make(map[string]string, len(assets)*2)
	for _, asset := range assets {
		for _, name := range githubReleaseAssetNames(filepath.Base(asset)) {
			if previous, duplicate := localNames[name]; duplicate && previous != asset {
				return fmt.Errorf("release assets %q and %q collide as GitHub asset name %q", previous, asset, name)
			}
			localNames[name] = asset
		}
	}
	for name, size := range remoteNames {
		if _, expected := localNames[name]; !expected {
			return fmt.Errorf("GitHub release contains unexpected asset %q; refusing to overwrite or delete it", name)
		}
		if size <= 0 {
			return fmt.Errorf("GitHub release asset %s has invalid size", name)
		}
	}

	if len(remoteNames) > 0 {
		downloadDir, err := os.MkdirTemp("", "tpa-release-existing-assets-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(downloadDir)
		if err := ghRun("release", "download", options.Plan.Tag, "--dir", downloadDir, "--repo", options.Repository); err != nil {
			return fmt.Errorf("read back existing GitHub release assets: %w", err)
		}
		for name, remoteSize := range remoteNames {
			localPath := localNames[name]
			remotePath := filepath.Join(downloadDir, name)
			if err := compareAssetIdentity(name, remotePath, localPath, remoteSize); err != nil {
				return err
			}
		}
	}

	if !release.Draft {
		if len(remoteNames) != len(assets) {
			return fmt.Errorf("published GitHub release is incomplete; refusing mutation")
		}
		if err := verifyReleaseAssetSet(options.Repository, release.ID, localNames, len(assets)); err != nil {
			return err
		}
		fmt.Printf("GitHub Release %s already contains the identical qualified artifacts; no assets changed.\n", options.Plan.Tag)
		return nil
	}

	missing := make([]string, 0)
	for _, path := range assets {
		present := false
		for _, name := range githubReleaseAssetNames(filepath.Base(path)) {
			if _, ok := remoteNames[name]; ok {
				present = true
				break
			}
		}
		if !present {
			missing = append(missing, path)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		args := []string{"release", "upload", options.Plan.Tag}
		args = append(args, missing...)
		args = append(args, "--repo", options.Repository)
		if err := ghRun(args...); err != nil {
			return fmt.Errorf("upload draft release assets: %w", err)
		}
	}
	if err := verifyReleaseAssetSet(options.Repository, release.ID, localNames, len(assets)); err != nil {
		return err
	}
	if err := verifyRemoteTag(options.Repository, options.Plan.Tag, options.Plan.SourceCommit); err != nil {
		return err
	}
	if err := setReleasePublished(options.Repository, release.ID); err != nil {
		return err
	}
	final, exists, err := getRelease(options.Repository, options.Plan.Tag)
	if err != nil || !exists {
		return fmt.Errorf("published release could not be read back: %w", err)
	}
	if final.Draft {
		return fmt.Errorf("GitHub Release remains a draft after publication request")
	}
	if err := verifyReleaseAssetSet(options.Repository, final.ID, localNames, len(assets)); err != nil {
		return err
	}
	if err := verifyRemoteTag(options.Repository, options.Plan.Tag, options.Plan.SourceCommit); err != nil {
		return err
	}
	fmt.Printf("GitHub Release %s published with %d verified assets.\n", options.Plan.Tag, len(assets))
	return nil
}

func verifyRemoteTag(repository, tag, expectedCommit string) error {
	output, err := ghOutput("api", "repos/"+repository+"/commits/"+tag)
	if err != nil {
		return fmt.Errorf("verify remote release tag: %w", err)
	}
	var response struct {
		SHA string `json:"sha"`
	}
	if err := json.Unmarshal([]byte(output), &response); err != nil {
		return fmt.Errorf("decode remote tag commit: %w", err)
	}
	if response.SHA != expectedCommit {
		return fmt.Errorf("remote tag %s now resolves to %s, expected qualified commit %s", tag, response.SHA, expectedCommit)
	}
	return nil
}

func getRelease(repository, tag string) (githubRelease, bool, error) {
	output, err := ghOutput("api", "repos/"+repository+"/releases/tags/"+tag)
	if err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "404") && !strings.Contains(strings.ToLower(err.Error()), "not found") {
			return githubRelease{}, false, err
		}
		// GitHub's release-by-tag endpoint omits drafts. Drafts remain visible
		// through the release collection to callers with contents:write access.
		output, err = ghOutput("api", "--paginate", "--jq", ".[]", "repos/"+repository+"/releases?per_page=100")
		if err != nil {
			return githubRelease{}, false, err
		}
		decoder := json.NewDecoder(strings.NewReader(output))
		for {
			var release githubRelease
			if err := decoder.Decode(&release); err != nil {
				if err == io.EOF {
					break
				}
				return githubRelease{}, false, fmt.Errorf("decode GitHub release list: %w", err)
			}
			if release.TagName == tag {
				return validateGitHubRelease(release)
			}
		}
		return githubRelease{}, false, nil
	}
	var release githubRelease
	if err := json.Unmarshal([]byte(output), &release); err != nil {
		return githubRelease{}, false, err
	}
	return validateGitHubRelease(release)
}

func waitForGitHubRelease(repository, tag string) (githubRelease, bool, error) {
	const attempts = 12
	for attempt := 0; attempt < attempts; attempt++ {
		release, exists, err := getRelease(repository, tag)
		if err != nil || exists {
			return release, exists, err
		}
		if attempt+1 < attempts {
			time.Sleep(500 * time.Millisecond)
		}
	}
	return githubRelease{}, false, nil
}

func validateGitHubRelease(release githubRelease) (githubRelease, bool, error) {
	if release.ID == 0 || release.TagName == "" {
		return githubRelease{}, false, fmt.Errorf("GitHub returned an invalid release record")
	}
	return release, true, nil
}

func createDraftRelease(options PublishOptions, sentinel string) error {
	title := options.Config.Project.Name + " " + options.Plan.Version
	args := []string{"release", "create", options.Plan.Tag, "--draft", "--verify-tag", "--target", options.Plan.SourceCommit, "--title", title, "--repo", options.Repository}
	notes, err := readRegularFile(filepath.Join(options.BundleDir, "release-notes.md"))
	if err != nil {
		return fmt.Errorf("release notes are required to identify workflow-owned drafts: %w", err)
	}
	tmp, err := os.CreateTemp("", "tpa-release-notes-*.md")
	if err != nil {
		return err
	}
	tempNotes := tmp.Name()
	defer os.Remove(tempNotes)
	if _, err := tmp.Write(append(notes, []byte("\n\n"+sentinel+"\n")...)); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	args = append(args, "--notes-file", tempNotes)
	return ghRun(args...)
}

func draftSentinel(cfg Config, plan Plan, notesHash string) string {
	return fmt.Sprintf("<!-- tpa-release-workflow:v1 project=%s package=%s version=%s tag=%s source=%s notes=%s -->", cfg.Project.Name, cfg.Package.Name, plan.Version, plan.Tag, plan.SourceCommit, notesHash)
}

func releaseBodyMatches(body string, notes []byte, sentinel string) bool {
	body = strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\r", "\n")
	normalizedNotes := strings.ReplaceAll(strings.ReplaceAll(string(notes), "\r\n", "\n"), "\r", "\n")
	body = strings.TrimRight(body, "\n")
	suffix := "\n\n" + sentinel
	if !strings.HasSuffix(body, suffix) {
		return false
	}
	bodyNotes := strings.TrimRight(strings.TrimSuffix(body, suffix), "\n")
	normalizedNotes = strings.TrimRight(normalizedNotes, "\n")
	return bodyNotes == normalizedNotes
}

func releaseAssets(bundle string) ([]string, error) {
	manifestBytes, err := readRegularFile(filepath.Join(bundle, "release-manifest.json"))
	if err != nil {
		return nil, err
	}
	var manifest ReleaseManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return nil, err
	}
	names := []string{"SHA256SUMS-GITHUB.txt", "release-manifest.json", "provenance.json", manifest.SourceArchive.Filename}
	for _, artifact := range manifest.Artifacts {
		names = append(names, artifact.Filename)
	}
	sort.Strings(names)
	paths := make([]string, 0, len(names))
	for _, name := range names {
		path := filepath.Join(bundle, name)
		if _, err := os.Lstat(path); err != nil {
			return nil, fmt.Errorf("release asset %s is unavailable: %w", name, err)
		}
		paths = append(paths, path)
	}
	return paths, nil
}

func githubReleaseAssetName(name string) string {
	return strings.ReplaceAll(name, "~", ".")
}

func githubReleaseAssetNames(name string) []string {
	// GitHub normalizes tildes to dots in uploaded release asset names.
	// Accept both forms when reading existing assets, but verify downloaded
	// bytes against the exact qualified local file either way.
	normalized := githubReleaseAssetName(name)
	if normalized == name {
		return []string{name}
	}
	return []string{name, normalized}
}

func projectGitHubReleaseAssets(bundle string, assets []string, tempDir string) ([]string, error) {
	projected := append([]string(nil), assets...)
	for i, path := range assets {
		name := filepath.Base(path)
		if name == "SHA256SUMS-GITHUB.txt" {
			projectedPath := filepath.Join(tempDir, "SHA256SUMS.txt")
			if err := copyRegularFile(path, projectedPath); err != nil {
				return nil, fmt.Errorf("prepare GitHub release checksum asset: %w", err)
			}
			projected[i] = projectedPath
			continue
		}
		normalized := githubReleaseAssetName(name)
		if normalized == name {
			continue
		}
		projectedPath := filepath.Join(tempDir, normalized)
		if err := copyRegularFile(path, projectedPath); err != nil {
			return nil, fmt.Errorf("prepare GitHub-normalized release asset %s: %w", name, err)
		}
		projected[i] = projectedPath
	}
	return projected, nil
}

func verifyReleaseAssetSet(repository string, releaseID int64, expected map[string]string, expectedCount int) error {
	output, err := ghOutput("api", "repos/"+repository+"/releases/"+fmt.Sprint(releaseID))
	if err != nil {
		return err
	}
	var release githubRelease
	if err := json.Unmarshal([]byte(output), &release); err != nil {
		return err
	}
	if len(release.Assets) != expectedCount {
		return fmt.Errorf("GitHub release asset set is incomplete: found %d, expected %d", len(release.Assets), expectedCount)
	}
	tmp, err := os.MkdirTemp("", "tpa-release-readback-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := ghRun("release", "download", release.TagName, "--dir", tmp, "--repo", repository); err != nil {
		return fmt.Errorf("download release assets for independent read-back: %w", err)
	}
	seen := make(map[string]bool, len(release.Assets))
	for _, asset := range release.Assets {
		path, ok := expected[asset.Name]
		if !ok || seen[asset.Name] {
			return fmt.Errorf("GitHub release has unexpected or duplicate asset %s", asset.Name)
		}
		seen[asset.Name] = true
		if err := compareAssetIdentity(asset.Name, filepath.Join(tmp, asset.Name), path, asset.Size); err != nil {
			return err
		}
	}
	return nil
}

func compareAssetIdentity(name, remotePath, expectedPath string, remoteSize int64) error {
	switch name {
	case "release-manifest.json":
		_, size, err := fileDigest(remotePath)
		if err != nil {
			return err
		}
		if size != remoteSize {
			return fmt.Errorf("GitHub release manifest asset size metadata is inconsistent")
		}
		return compareManifestIdentity(remotePath, expectedPath)
	case "provenance.json":
		_, size, err := fileDigest(remotePath)
		if err != nil {
			return err
		}
		if size != remoteSize {
			return fmt.Errorf("GitHub provenance asset size metadata is inconsistent")
		}
		return compareProvenanceIdentity(remotePath, expectedPath)
	default:
		wantHash, wantSize, err := fileDigest(expectedPath)
		if err != nil {
			return err
		}
		gotHash, gotSize, err := fileDigest(remotePath)
		if err != nil {
			return fmt.Errorf("read back GitHub asset %s: %w", name, err)
		}
		if gotHash != wantHash || gotSize != wantSize || remoteSize != wantSize {
			return fmt.Errorf("existing GitHub release asset %s conflicts with qualified bytes", name)
		}
		return nil
	}
}

func compareProvenanceIdentity(existingPath, expectedPath string) error {
	read := func(path string) (Provenance, error) {
		data, err := readRegularFile(path)
		if err != nil {
			return Provenance{}, err
		}
		var provenance Provenance
		if err := json.Unmarshal(data, &provenance); err != nil {
			return Provenance{}, err
		}
		return provenance, nil
	}
	existing, err := read(existingPath)
	if err != nil {
		return fmt.Errorf("decode existing release provenance: %w", err)
	}
	expected, err := read(expectedPath)
	if err != nil {
		return err
	}
	if existing.SchemaVersion != expected.SchemaVersion || existing.PredicateType != expected.PredicateType || existing.Project != expected.Project || existing.Package != expected.Package || existing.Version != expected.Version || existing.Tag != expected.Tag || existing.SourceRepo != expected.SourceRepo || existing.SourceCommit != expected.SourceCommit || existing.ConfigSHA256 != expected.ConfigSHA256 || !equalStringMap(existing.SourceInputs, expected.SourceInputs) || existing.ReleaseNotesSHA256 != expected.ReleaseNotesSHA256 || existing.SourceArchive.Filename != expected.SourceArchive.Filename || existing.SourceArchive.Size != expected.SourceArchive.Size || existing.SourceArchive.SHA256 != expected.SourceArchive.SHA256 || !sameBuildIdentityStable(existing.Builder, expected.Builder) || len(existing.Artifacts) != len(expected.Artifacts) {
		return fmt.Errorf("existing GitHub provenance conflicts with this tag/source identity")
	}
	byName := make(map[string]ReleaseArtifact, len(existing.Artifacts))
	for _, artifact := range existing.Artifacts {
		byName[artifact.Filename] = artifact
	}
	for _, artifact := range expected.Artifacts {
		old, ok := byName[artifact.Filename]
		if !ok || !sameArtifact(old, artifact) {
			return fmt.Errorf("existing GitHub provenance conflicts for artifact %s", artifact.Filename)
		}
	}
	return nil
}

func setReleasePublished(repository string, id int64) error {
	return ghRun("api", "--method", "PATCH", "repos/"+repository+"/releases/"+fmt.Sprint(id), "-F", "draft=false")
}

func compareManifestIdentity(existingPath, expectedPath string) error {
	read := func(path string) (ReleaseManifest, error) {
		data, err := readRegularFile(path)
		if err != nil {
			return ReleaseManifest{}, err
		}
		var manifest ReleaseManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			return ReleaseManifest{}, err
		}
		return manifest, nil
	}
	existing, err := read(existingPath)
	if err != nil {
		return fmt.Errorf("decode existing release manifest: %w", err)
	}
	expected, err := read(expectedPath)
	if err != nil {
		return err
	}
	if existing.SchemaVersion != expected.SchemaVersion || existing.Project != expected.Project || existing.Package != expected.Package || existing.Version != expected.Version || existing.Tag != expected.Tag || existing.SourceRepo != expected.SourceRepo || existing.SourceCommit != expected.SourceCommit || existing.ConfigSHA256 != expected.ConfigSHA256 || !equalStringMap(existing.SourceInputs, expected.SourceInputs) || existing.ReleaseNotesSHA256 != expected.ReleaseNotesSHA256 || existing.SourceArchive.Filename != expected.SourceArchive.Filename || existing.SourceArchive.Size != expected.SourceArchive.Size || existing.SourceArchive.SHA256 != expected.SourceArchive.SHA256 || !sameBuildIdentityStable(existing.Build, expected.Build) || len(existing.Artifacts) != len(expected.Artifacts) {
		return fmt.Errorf("existing GitHub release manifest conflicts with this tag/source identity")
	}
	byName := make(map[string]ReleaseArtifact, len(existing.Artifacts))
	for _, artifact := range existing.Artifacts {
		byName[artifact.Filename] = artifact
	}
	for _, artifact := range expected.Artifacts {
		old, ok := byName[artifact.Filename]
		if !ok || !sameArtifact(old, artifact) {
			return fmt.Errorf("existing GitHub release manifest has conflicting artifact %s", artifact.Filename)
		}
	}
	return nil
}

func ghOutput(args ...string) (string, error) {
	cmd := exec.Command("gh", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("gh %s: %s: %w", strings.Join(args, " "), strings.TrimSpace(string(output)), err)
	}
	return strings.TrimSpace(string(output)), nil
}

func ghRun(args ...string) error {
	cmd := exec.Command("gh", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gh %s: %w", strings.Join(args, " "), err)
	}
	return nil
}
