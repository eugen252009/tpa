package aptpackage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const hostedGenerationProtocol = "tparun-prebuilt-ssh"

var hostedIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

type HostedPublishOptions struct {
	StageID          string
	LegacyFullTree   bool
	SSHConfig        string
	Keyring          []byte
	Fingerprint      string
	GenerationID     string
	ParentGeneration string
}
type HostedPublishResult struct {
	SSHApplicationBytes  int64  `json:"ssh_application_bytes"`
	ProtocolControlBytes int64  `json:"protocol_control_bytes"`
	CAS                  bool   `json:"cas"`
	UploadedBytes        int64  `json:"uploaded_bytes"`
	ReusedUniqueBytes    int64  `json:"reused_unique_bytes"`
	LogicalBytes         int64  `json:"logical_bytes"`
	UniqueCASBytes       int64  `json:"unique_cas_bytes"`
	ID                   string `json:"id"`
	State                string `json:"state"`
	GenerationID         string `json:"generation_id"`
	APTSigner            string `json:"apt_signer"`
}

// PublishRepositoryToTPARunSSH streams an already-complete owner-signed stable
// repository. Default protocol2 negotiates fixed chunks; LegacyFullTree is an
// explicit compatibility option, never an automatic downgrade.
// No files are modified, no package/repository build or signing is performed.
func PublishRepositoryToTPARunSSH(ctx context.Context, directory, destination string, opts HostedPublishOptions) (HostedPublishResult, error) {
	var result HostedPublishResult
	if opts.StageID != "" && (!hostedIDPattern.MatchString(opts.StageID) || opts.LegacyFullTree) {
		return result, fmt.Errorf("staged publication requires a valid stage identity and chunk-CAS protocol")
	}
	if !strings.HasPrefix(destination, "tparun+ssh://") {
		return result, fmt.Errorf("explicit tparun+ssh:// destination required; no fallback to ordinary SSH")
	}
	dest, err := ParseRepositoryDestination("ssh://" + strings.TrimPrefix(destination, "tparun+ssh://"))
	if err != nil {
		return result, err
	}
	repo := strings.TrimPrefix(dest.RemotePath, "/")
	if !hostedIDPattern.MatchString(repo) || (dest.User != "" && dest.User != "tparun") {
		return result, fmt.Errorf("TPA.run destination requires protocol user tparun and one 32-hex repository ID")
	}
	if len(opts.Keyring) == 0 {
		return result, fmt.Errorf("explicit owner public keyring required for finished-repository publication")
	}
	if opts.GenerationID == "" {
		var token [16]byte
		if _, err := rand.Read(token[:]); err != nil {
			return result, err
		}
		opts.GenerationID = hex.EncodeToString(token[:])
	}
	if !hostedIDPattern.MatchString(opts.GenerationID) || (opts.ParentGeneration != "" && !hostedIDPattern.MatchString(opts.ParentGeneration)) {
		return result, fmt.Errorf("invalid generation/parent identity")
	}
	location, err := ParseRepositoryLocator(directory)
	if err != nil {
		return result, err
	}
	reader, err := OpenRepositoryReader(location)
	if err != nil {
		return result, err
	}
	_, verifyErr := VerifyRepository(ctx, reader, RepositoryOptions{Codename: "stable", Keyring: opts.Keyring, Fingerprint: opts.Fingerprint, RequireSigned: true})
	closeErr := reader.Close()
	if verifyErr != nil {
		return result, fmt.Errorf("verify completed local repository: %w", verifyErr)
	}
	if closeErr != nil {
		return result, closeErr
	}
	if err := verifyRepositoryBrowserFiles(directory, RepoConfig{Codename: "stable", Components: "main"}, true); err != nil {
		return result, err
	}
	manifest, err := CreateGenerationManifest(directory, repo, opts.GenerationID, opts.ParentGeneration)
	if err != nil {
		return result, err
	}
	// A finite wall timeout is independent from payload RAM; server policy can
	// impose a smaller transfer deadline. Content is always streamed from files.
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	args := []string{"-T", "-oBatchMode=yes", "-oStrictHostKeyChecking=yes", "-oConnectTimeout=15", "-oForwardAgent=no", "-oClearAllForwardings=yes", "-oPermitLocalCommand=no"}
	if opts.SSHConfig != "" {
		args = append(args, "-F", opts.SSHConfig)
	}
	if dest.PortExplicit {
		args = append(args, "-p", strconv.Itoa(dest.Port))
	}
	command := "tparun-v1 generation-cas "
	if opts.LegacyFullTree {
		command = "tparun-v1 generation "
	}
	args = append(args, "-l", "tparun", "--", dest.Host, command+repo)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return result, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return result, err
	}
	diagnostic := &hostedDiagnostic{}
	cmd.Stderr = diagnostic
	if err := cmd.Start(); err != nil {
		return result, err
	}
	waited := false
	defer func() {
		stdin.Close()
		if !waited {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	if !opts.LegacyFullTree {
		result, err = publishHostedCAS(directory, manifest, stdin, stdout, opts.StageID)
		if err != nil {
			return result, err
		}
		err = cmd.Wait()
		waited = true
		if err != nil {
			return result, fmt.Errorf("CAS SSH completion (activation may have occurred): %w: %s", err, diagnostic.String())
		}
		return result, nil
	}
	decoder := json.NewDecoder(io.LimitReader(stdout, 128<<10))
	var ready struct {
		Version         int
		Protocol, State string
		MaxUploadBytes  int64 `json:"max_upload_bytes"`
	}
	if err := decoder.Decode(&ready); err != nil {
		return result, fmt.Errorf("TPA.run SSH capability/admission failed: %w: %s", err, diagnostic.String())
	}
	if ready.Version != 1 || ready.Protocol != hostedGenerationProtocol || ready.State != "ready" {
		return result, fmt.Errorf("required TPA.run generation protocol unavailable")
	}
	writer := multipart.NewWriter(stdin)
	header := struct {
		Version     int    `json:"version"`
		ContentType string `json:"content_type"`
	}{1, writer.FormDataContentType()}
	if err := json.NewEncoder(stdin).Encode(header); err != nil {
		return result, err
	}
	part, err := writer.CreateFormField("manifest")
	if err != nil {
		return result, err
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return result, err
	}
	if _, err := part.Write(append(data, '\n')); err != nil {
		return result, err
	}
	send := func(file GenerationFile) error {
		part, err := writer.CreateFormField("file:" + file.Path)
		if err != nil {
			return err
		}
		f, err := os.Open(filepath.Join(directory, filepath.FromSlash(file.Path)))
		if err != nil {
			return err
		}
		n, copyErr := io.Copy(part, io.LimitReader(f, file.Size+1))
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if n != file.Size {
			return fmt.Errorf("local generation file changed size during transfer: %s", file.Path)
		}
		return nil
	}
	found := false
	for _, file := range manifest.Files {
		if file.Path == "dists/stable/InRelease" {
			found = true
			if err := send(file); err != nil {
				return result, err
			}
			break
		}
	}
	if !found {
		return result, fmt.Errorf("stable InRelease missing")
	}
	for _, file := range manifest.Files {
		if file.Path != "dists/stable/InRelease" {
			if err := send(file); err != nil {
				return result, err
			}
		}
	}
	if err := writer.Close(); err != nil {
		return result, err
	}
	if err := stdin.Close(); err != nil {
		return result, err
	}
	var reply struct {
		Version, Status int
		Result          json.RawMessage
	}
	if err := decoder.Decode(&reply); err != nil {
		return result, fmt.Errorf("TPA.run SSH result unavailable (activation may be unknown): %w: %s", err, diagnostic.String())
	}
	waitErr := cmd.Wait()
	waited = true
	if reply.Version != 1 || reply.Status != 202 || waitErr != nil {
		return result, fmt.Errorf("TPA.run generation result (activation may have occurred): status=%d result=%s SSH=%v diagnostic=%s", reply.Status, reply.Result, waitErr, diagnostic.String())
	}
	if err := json.Unmarshal(reply.Result, &result); err != nil {
		return result, err
	}
	if result.GenerationID != manifest.GenerationID || result.State != "succeeded" {
		return result, fmt.Errorf("unexpected generation activation result")
	}
	return result, nil
}

type hostedDiagnostic struct {
	mu   sync.Mutex
	data []byte
}

func (b *hostedDiagnostic) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	if len(b.data) < 16<<10 {
		remaining := (16 << 10) - len(b.data)
		if len(p) > remaining {
			p = p[:remaining]
		}
		b.data = append(b.data, p...)
	}
	return n, nil
}
func (b *hostedDiagnostic) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(string(b.data))
}
