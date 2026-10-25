package aptpackage

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type HostedStageArtifact struct {
	Filename     string          `json:"filename"`
	Package      string          `json:"package"`
	Version      string          `json:"version"`
	Architecture string          `json:"architecture"`
	Size         int64           `json:"size"`
	SHA256       string          `json:"sha256"`
	State        string          `json:"state,omitempty"`
	Inventory    json.RawMessage `json:"inventory,omitempty"`
}
type HostedReleaseStage struct {
	ID                 string                `json:"id"`
	RepositoryID       string                `json:"repository_id"`
	PublisherAccountID string                `json:"publisher_account_id"`
	OwnerAccountID     string                `json:"owner_account_id"`
	BaseGeneration     string                `json:"base_generation"`
	ManifestSHA256     string                `json:"manifest_sha256"`
	State              string                `json:"state"`
	GenerationID       string                `json:"generation_id"`
	CreatedAt          string                `json:"created_at"`
	Artifacts          []HostedStageArtifact `json:"artifacts"`
}
type HostedStageOptions struct {
	SSHConfig, StageID, ParentGeneration, ManifestSHA256, File, Filename string
	Artifacts                                                            []HostedStageArtifact
	Download                                                             io.Writer
}
type HostedStageResult struct {
	Stage         *HostedReleaseStage `json:"stage,omitempty"`
	UploadedBytes int64               `json:"uploaded_bytes"`
	ReusedBytes   int64               `json:"reused_unique_bytes"`
}
type hostedStageRequest struct {
	Operation      string                `json:"operation"`
	StageID        string                `json:"stage_id"`
	BaseGeneration string                `json:"base_generation,omitempty"`
	ManifestSHA256 string                `json:"manifest_sha256,omitempty"`
	Artifacts      []HostedStageArtifact `json:"artifacts,omitempty"`
	File           *hostedCASFile        `json:"file,omitempty"`
	Filename       string                `json:"filename,omitempty"`
}

// ReadStageArtifactManifest accepts the existing release-manifest.json artifact
// projection (or {"artifacts":[...]}) and binds the exact original file bytes.
func ReadStageArtifactManifest(path string) (string, []HostedStageArtifact, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", nil, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, (16<<20)+1))
	if err != nil {
		return "", nil, err
	}
	if len(raw) > 16<<20 {
		return "", nil, fmt.Errorf("artifact manifest exceeds 16MiB")
	}
	var m struct {
		Artifacts []HostedStageArtifact `json:"artifacts"`
	}
	if err = json.Unmarshal(raw, &m); err != nil {
		return "", nil, err
	}
	if err = validateHostedStageArtifacts(m.Artifacts); err != nil {
		return "", nil, err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), m.Artifacts, nil
}
func validateHostedStageArtifacts(artifacts []HostedStageArtifact) error {
	if len(artifacts) == 0 || len(artifacts) > 4096 {
		return fmt.Errorf("stage requires 1-4096 artifacts")
	}
	names, identities := map[string]bool{}, map[string]bool{}
	for _, a := range artifacts {
		identity := a.Package + "\x00" + a.Version + "\x00" + a.Architecture
		if len(a.Filename) > 255 || !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+~%-]*\.deb$`).MatchString(a.Filename) || a.Package == "" || a.Version == "" || a.Architecture == "" || a.Size <= 0 || len(a.SHA256) != 64 || names[a.Filename] || identities[identity] || a.State != "" || len(a.Inventory) != 0 {
			return fmt.Errorf("invalid stage artifact %q", a.Filename)
		}
		b, err := hex.DecodeString(a.SHA256)
		if err != nil || hex.EncodeToString(b) != a.SHA256 {
			return fmt.Errorf("invalid stage artifact digest")
		}
		names[a.Filename] = true
		identities[identity] = true
	}
	return nil
}

// StageRepositoryArtifacts uses only the existing restricted SSH account and
// repository-scoped CAS. It never sends signing secrets or makes content public.
func StageRepositoryArtifacts(ctx context.Context, destination, operation string, opts HostedStageOptions) (HostedStageResult, error) {
	var result HostedStageResult
	if !hostedIDPattern.MatchString(opts.StageID) {
		return result, fmt.Errorf("stage requires a 32-hex identity")
	}
	if !strings.HasPrefix(destination, "tparun+ssh://") {
		return result, fmt.Errorf("explicit tparun+ssh:// destination required")
	}
	dest, err := ParseRepositoryDestination("ssh://" + strings.TrimPrefix(destination, "tparun+ssh://"))
	if err != nil {
		return result, err
	}
	repo := strings.TrimPrefix(dest.RemotePath, "/")
	if !hostedIDPattern.MatchString(repo) || (dest.User != "" && dest.User != "tparun") {
		return result, fmt.Errorf("stage requires protocol user tparun and one repository ID")
	}
	req := hostedStageRequest{Operation: operation, StageID: opts.StageID, BaseGeneration: opts.ParentGeneration, ManifestSHA256: opts.ManifestSHA256, Artifacts: opts.Artifacts, Filename: opts.Filename}
	var locations map[string]hostedChunkLocation
	if operation == "upload" {
		info, err := os.Lstat(opts.File)
		if err != nil {
			return result, err
		}
		if !info.Mode().IsRegular() {
			return result, fmt.Errorf("artifact must be a regular file")
		}
		control, err := ParsePackage(opts.File)
		if err != nil {
			return result, err
		}
		_ = control
		f, err := os.Open(opts.File)
		if err != nil {
			return result, err
		}
		h := sha256.New()
		n, err := io.Copy(h, f)
		f.Close()
		if err != nil {
			return result, err
		}
		base := filepath.Base(opts.File)
		m := GenerationManifest{RepositoryID: repo, GenerationID: opts.StageID, Files: []GenerationFile{{Path: base, Size: n, SHA256: hex.EncodeToString(h.Sum(nil))}}}
		inv, loc, err := hostedCASInventoryFor(filepath.Dir(opts.File), m)
		if err != nil {
			return result, err
		}
		req.File = &inv.Files[0]
		locations = loc
	} else if operation == "create" {
		if err = validateHostedStageArtifacts(req.Artifacts); err != nil {
			return result, err
		}
	} else if operation != "inspect" && operation != "freeze" && operation != "cancel" && operation != "fetch" {
		return result, fmt.Errorf("unsupported stage operation")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	args := []string{"-T", "-oBatchMode=yes", "-oStrictHostKeyChecking=yes", "-oConnectTimeout=15", "-oForwardAgent=no", "-oClearAllForwardings=yes", "-oPermitLocalCommand=no"}
	if opts.SSHConfig != "" {
		args = append(args, "-F", opts.SSHConfig)
	}
	if dest.PortExplicit {
		args = append(args, "-p", strconv.Itoa(dest.Port))
	}
	args = append(args, "-l", "tparun", "--", dest.Host, "tparun-v1 stage-cas "+repo)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	input, err := cmd.StdinPipe()
	if err != nil {
		return result, err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		input.Close()
		return result, err
	}
	diagnostic := &hostedDiagnostic{}
	cmd.Stderr = diagnostic
	if err = cmd.Start(); err != nil {
		return result, err
	}
	waited := false
	defer func() {
		input.Close()
		if !waited {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	reader := bufio.NewReader(output)
	var ready struct {
		Version         int
		Protocol, State string
	}
	if err = hostedCASRead(reader, &ready); err != nil {
		return result, fmt.Errorf("stage admission: %w: %s", err, diagnostic.String())
	}
	if ready.Version != 1 || ready.Protocol != "tparun-stage-cas" || ready.State != "ready" {
		return result, fmt.Errorf("stage protocol unavailable; no fallback")
	}
	if err = hostedCASWrite(input, req); err != nil {
		return result, err
	}
	if operation == "upload" {
		var need struct {
			Missing []hostedChunk `json:"missing"`
		}
		if err = hostedCASRead(reader, &need); err != nil {
			return result, err
		}
		seen := map[string]bool{}
		for _, c := range need.Missing {
			loc, ok := locations[c.Digest]
			if !ok || seen[c.Digest] || loc.size != c.Size {
				return result, fmt.Errorf("server requested undeclared/duplicate chunk")
			}
			seen[c.Digest] = true
			f, err := os.Open(opts.File)
			if err != nil {
				return result, err
			}
			n, err := io.Copy(input, io.NewSectionReader(f, loc.offset, loc.size))
			f.Close()
			if err != nil {
				return result, err
			}
			if n != loc.size {
				return result, fmt.Errorf("artifact changed during transfer")
			}
			var ack struct {
				Committed string `json:"committed"`
			}
			if err = hostedCASRead(reader, &ack); err != nil {
				return result, err
			}
			if ack.Committed != c.Digest {
				return result, fmt.Errorf("wrong stage chunk acknowledgement")
			}
		}
	}
	if err = input.Close(); err != nil {
		return result, err
	}
	if operation == "fetch" {
		var response struct {
			File hostedCASFile `json:"file"`
		}
		if err = hostedCASRead(reader, &response); err != nil {
			return result, err
		}
		if response.File.Path != opts.Filename || response.File.Size <= 0 || opts.Download == nil {
			return result, fmt.Errorf("unexpected stage artifact")
		}
		// The owner verifies against the immutable expected manifest, not this header.
		n, err := io.Copy(opts.Download, io.LimitReader(reader, response.File.Size))
		if err != nil {
			return result, err
		}
		if n != response.File.Size {
			return result, fmt.Errorf("incomplete stage retrieval")
		}
	} else {
		if err = hostedCASRead(reader, &result); err != nil {
			return result, err
		}
		if result.Stage == nil || result.Stage.ID != opts.StageID || result.Stage.RepositoryID != repo {
			return result, fmt.Errorf("stage response identity mismatch")
		}
	}
	err = cmd.Wait()
	waited = true
	if err != nil {
		return result, fmt.Errorf("stage completion uncertain; retry the same stage identity: %w: %s", err, diagnostic.String())
	}
	return result, nil
}

type HostedSignOptions struct {
	StageID, SSHConfig, ManifestSHA256, GPG string
	Keyring                                 []byte
	Fingerprint                             string
	Repo                                    RepoConfig
	Artifacts                               []HostedStageArtifact
}

// FinalizeHostedStage constructs metadata and signs exclusively on the owner's
// machine. Pack indexes existing DEBs; it never invokes the package build engine.
func FinalizeHostedStage(ctx context.Context, destination string, opts HostedSignOptions) (HostedStageResult, error) {
	stageOpts := HostedStageOptions{StageID: opts.StageID, SSHConfig: opts.SSHConfig}
	result, err := StageRepositoryArtifacts(ctx, destination, "inspect", stageOpts)
	if err != nil {
		return result, err
	}
	if opts.ManifestSHA256 == "" || result.Stage.ManifestSHA256 != opts.ManifestSHA256 {
		return result, fmt.Errorf("stage differs from approved artifact manifest")
	}
	if err = verifyApprovedStage(result.Stage, opts.Artifacts); err != nil {
		return result, err
	}
	if result.Stage.State == "PUBLISHED" {
		return result, nil
	}
	if opts.GPG == "" || len(opts.Keyring) == 0 {
		return result, fmt.Errorf("owner signing identity and explicit public verification keyring required")
	}
	result, err = StageRepositoryArtifacts(ctx, destination, "freeze", stageOpts)
	if err != nil {
		return result, err
	}
	if result.Stage.State == "PUBLISHED" {
		return result, nil
	}
	stage := result.Stage
	if err = verifyApprovedStage(stage, opts.Artifacts); err != nil {
		return result, err
	}
	root, err := os.MkdirTemp("", "tpa-owner-finalize-")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(root)
	packages := filepath.Join(root, "packages")
	if err = os.Mkdir(packages, 0700); err != nil {
		return result, err
	}
	for _, a := range stage.Artifacts {
		if filepath.Base(a.Filename) != a.Filename || a.State != "ready" || a.Size <= 0 {
			return result, fmt.Errorf("invalid frozen stage artifact")
		}
		path := filepath.Join(packages, a.Filename)
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return result, err
		}
		hash := sha256.New()
		bounded := &stageDownloadWriter{w: io.MultiWriter(file, hash), remaining: a.Size}
		fetch := stageOpts
		fetch.Filename = a.Filename
		fetch.Download = bounded
		_, err = StageRepositoryArtifacts(ctx, destination, "fetch", fetch)
		closeErr := file.Close()
		if err != nil {
			return result, err
		}
		if closeErr != nil {
			return result, closeErr
		}
		if bounded.remaining != 0 || hex.EncodeToString(hash.Sum(nil)) != a.SHA256 {
			return result, fmt.Errorf("staged artifact checksum mismatch: %s", a.Filename)
		}
		control, err := ParsePackage(path)
		if err != nil {
			return result, err
		}
		if control.Name != a.Package || control.Version != a.Version || control.Architecture != a.Architecture {
			return result, fmt.Errorf("staged artifact identity mismatch")
		}
	}
	repo := opts.Repo
	if repo.Codename == "" {
		repo.Codename = "stable"
	}
	if repo.Suite == "" {
		repo.Suite = "stable"
	}
	if repo.Components == "" {
		repo.Components = "main"
	}
	if repo.Codename != "stable" || repo.Components != "main" {
		return result, fmt.Errorf("current SSH receiver requires stable/main; channels use independent repository IDs")
	}
	finished := filepath.Join(root, "finished")
	cfg := Config{InDir: packages, OutDir: finished, GPG: opts.GPG, Repo: repo}
	if err = Pack(cfg); err != nil {
		return result, fmt.Errorf("owner-side metadata/signature finalization: %w", err)
	}
	_, err = PublishRepositoryToTPARunSSH(ctx, finished, destination, HostedPublishOptions{SSHConfig: opts.SSHConfig, Keyring: opts.Keyring, Fingerprint: opts.Fingerprint, GenerationID: stage.GenerationID, ParentGeneration: stage.BaseGeneration, StageID: stage.ID})
	if err != nil {
		return result, err
	}
	return StageRepositoryArtifacts(ctx, destination, "inspect", stageOpts)
}
func verifyApprovedStage(stage *HostedReleaseStage, artifacts []HostedStageArtifact) error {
	if err := validateHostedStageArtifacts(artifacts); err != nil {
		return err
	}
	if len(stage.Artifacts) != len(artifacts) {
		return fmt.Errorf("stage artifact count differs from approved manifest")
	}
	expected := map[string]HostedStageArtifact{}
	for _, a := range artifacts {
		expected[a.Filename] = a
	}
	for _, a := range stage.Artifacts {
		e, ok := expected[a.Filename]
		if !ok || a.Package != e.Package || a.Version != e.Version || a.Architecture != e.Architecture || a.Size != e.Size || a.SHA256 != e.SHA256 {
			return fmt.Errorf("stage artifacts differ from approved manifest")
		}
		delete(expected, a.Filename)
	}
	return nil
}

type stageDownloadWriter struct {
	w         io.Writer
	remaining int64
}

func (w *stageDownloadWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.remaining {
		return 0, fmt.Errorf("staged artifact exceeds approved size")
	}
	n, err := w.w.Write(p)
	w.remaining -= int64(n)
	return n, err
}
func NewHostedStageID() (string, error) {
	var b [16]byte
	_, err := rand.Read(b[:])
	return hex.EncodeToString(b[:]), err
}
