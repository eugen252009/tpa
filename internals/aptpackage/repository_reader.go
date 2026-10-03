package aptpackage

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/eugen252009/tpa/internal/version"
)

type RepositoryLocatorKind string

const (
	RepositoryLocal RepositoryLocatorKind = "local"
	RepositoryHTTP  RepositoryLocatorKind = "http"
	RepositoryHTTPS RepositoryLocatorKind = "https"
	RepositorySSH   RepositoryLocatorKind = "ssh"
)

type RepositoryLocator struct {
	Kind RepositoryLocatorKind
	Path string
	URL  *url.URL
}

// ParseRepositoryLocator preserves filesystem syntax and explicit schemes.
// Existing ambiguous relative paths are local; absent bare values are remote
// only when their first path component is recognizably host-like.
func ParseRepositoryLocator(value string) (RepositoryLocator, error) {
	if strings.TrimSpace(value) == "" || value != strings.TrimSpace(value) {
		return RepositoryLocator{}, fmt.Errorf("repository locator is empty or has surrounding whitespace")
	}
	if strings.HasPrefix(value, "file://") {
		u, err := url.Parse(value)
		if err != nil || u.Host != "" || u.Path == "" || u.RawQuery != "" || u.Fragment != "" {
			return RepositoryLocator{}, fmt.Errorf("invalid file repository locator")
		}
		return RepositoryLocator{Kind: RepositoryLocal, Path: filepath.FromSlash(u.Path)}, nil
	}
	if scheme, rest, ok := strings.Cut(value, "://"); ok {
		u, err := url.Parse(scheme + "://" + rest)
		if err != nil || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || strings.Contains(u.Path, "\\") || strings.ContainsRune(u.Path, 0) || hasDotDotPath(u.Path) {
			return RepositoryLocator{}, fmt.Errorf("invalid repository URL")
		}
		switch strings.ToLower(u.Scheme) {
		case "http":
			if u.User != nil {
				return RepositoryLocator{}, fmt.Errorf("HTTP repository URLs must not embed credentials")
			}
			return RepositoryLocator{Kind: RepositoryHTTP, URL: u}, nil
		case "https":
			if u.User != nil {
				return RepositoryLocator{}, fmt.Errorf("HTTP repository URLs must not embed credentials")
			}
			return RepositoryLocator{Kind: RepositoryHTTPS, URL: u}, nil
		case "ssh":
			if u.Path == "" || !strings.HasPrefix(u.Path, "/") {
				return RepositoryLocator{}, fmt.Errorf("SSH repository URL requires an absolute path")
			}
			if u.User != nil && (strings.ContainsAny(u.User.String(), "\r\n") || func() bool { _, hasPassword := u.User.Password(); return hasPassword }()) {
				return RepositoryLocator{}, fmt.Errorf("invalid SSH user")
			}
			return RepositoryLocator{Kind: RepositorySSH, URL: u}, nil
		default:
			return RepositoryLocator{}, fmt.Errorf("unsupported repository URL scheme %q", u.Scheme)
		}
	}
	if clearlyLocalPath(value) {
		return RepositoryLocator{Kind: RepositoryLocal, Path: value}, nil
	}
	if _, err := os.Stat(value); err == nil {
		return RepositoryLocator{Kind: RepositoryLocal, Path: value}, nil
	}
	first := strings.SplitN(filepath.ToSlash(value), "/", 2)[0]
	if hostLike(first) {
		u, err := url.Parse("https://" + value)
		if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.Contains(u.Path, "\\") || strings.ContainsRune(u.Path, 0) || hasDotDotPath(u.Path) {
			return RepositoryLocator{}, fmt.Errorf("invalid remote repository locator")
		}
		return RepositoryLocator{Kind: RepositoryHTTPS, URL: u}, nil
	}
	return RepositoryLocator{Kind: RepositoryLocal, Path: value}, nil
}

func hasDotDotPath(value string) bool {
	for _, segment := range strings.Split(value, "/") {
		if segment == ".." {
			return true
		}
	}
	return false
}

func clearlyLocalPath(value string) bool {
	if value == "." || value == ".." || strings.HasPrefix(value, "./") || strings.HasPrefix(value, "../") || filepath.IsAbs(value) || strings.HasPrefix(value, "~/") {
		return true
	}
	// Preserve native drive-letter paths on Windows.
	return len(value) >= 3 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':' && (value[2] == '/' || value[2] == '\\')
}

var hostPortPattern = regexp.MustCompile(`^[^/:]+:[0-9]+$`)

func hostLike(value string) bool {
	if strings.HasPrefix(value, "[") {
		if closeBracket := strings.IndexByte(value, ']'); closeBracket > 1 {
			tail := value[closeBracket+1:]
			return tail == "" || (strings.HasPrefix(tail, ":") && len(tail) > 1 && allASCIIDigits(tail[1:]))
		}
	}
	if value == "localhost" || strings.Contains(value, ".") || hostPortPattern.MatchString(value) {
		return true
	}
	return false
}

func allASCIIDigits(value string) bool {
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return value != ""
}

type RepositoryReader interface {
	Open(context.Context, string) (io.ReadCloser, error)
	Close() error
}

func OpenRepositoryReader(locator RepositoryLocator) (RepositoryReader, error) {
	switch locator.Kind {
	case RepositoryLocal:
		root, err := filepath.Abs(locator.Path)
		if err != nil {
			return nil, err
		}
		info, err := os.Lstat(root)
		if err != nil {
			return nil, fmt.Errorf("open local repository: %w", err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("local repository root must be a real directory")
		}
		return localRepositoryReader{root: root}, nil
	case RepositoryHTTP, RepositoryHTTPS:
		client := &http.Client{Timeout: 30 * time.Minute, CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many repository HTTP redirects")
			}
			if request.URL.Scheme != "http" && request.URL.Scheme != "https" {
				return fmt.Errorf("repository redirect uses unsupported scheme %q", request.URL.Scheme)
			}
			if via[len(via)-1].URL.Scheme == "https" && request.URL.Scheme != "https" {
				return fmt.Errorf("repository HTTPS redirect may not downgrade to HTTP")
			}
			return nil
		}}
		return &httpRepositoryReader{root: *locator.URL, client: client}, nil
	case RepositorySSH:
		return sshRepositoryReader{url: *locator.URL}, nil
	default:
		return nil, fmt.Errorf("unsupported repository locator kind %q", locator.Kind)
	}
}

type localRepositoryReader struct{ root string }

func (r localRepositoryReader) Open(_ context.Context, relative string) (io.ReadCloser, error) {
	clean, err := repositoryRelativePath(relative)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(r.root)
	if err != nil {
		return nil, err
	}
	current := ""
	for _, part := range strings.Split(clean, "/") {
		if current == "" {
			current = part
		} else {
			current = path.Join(current, part)
		}
		info, statErr := root.Lstat(current)
		if statErr != nil {
			root.Close()
			return nil, statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			root.Close()
			return nil, fmt.Errorf("repository path %q contains a symbolic link", relative)
		}
	}
	file, err := root.Open(clean)
	if err != nil {
		root.Close()
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		root.Close()
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("repository entry %q is not a regular file", relative)
	}
	return &rootFile{File: file, root: root}, nil
}
func (localRepositoryReader) Close() error { return nil }

type rootFile struct {
	*os.File
	root *os.Root
}

func (f *rootFile) Close() error {
	fileErr := f.File.Close()
	rootErr := f.root.Close()
	if fileErr != nil {
		return fileErr
	}
	return rootErr
}

type httpRepositoryReader struct {
	root   url.URL
	client *http.Client
}

func (r *httpRepositoryReader) Open(ctx context.Context, relative string) (io.ReadCloser, error) {
	clean, err := repositoryRelativePath(relative)
	if err != nil {
		return nil, err
	}
	target := r.root
	target.Path = path.Join(target.Path, clean)
	target.RawPath = ""
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "TPA/"+version.Version)
	response, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch repository file %s: %w", relative, err)
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("fetch repository file %s: HTTP %s", relative, response.Status)
	}
	return response.Body, nil
}
func (r *httpRepositoryReader) Close() error { return nil }

type sshRepositoryReader struct{ url url.URL }

func (r sshRepositoryReader) Open(ctx context.Context, relative string) (io.ReadCloser, error) {
	clean, err := repositoryRelativePath(relative)
	if err != nil {
		return nil, err
	}
	host := r.url.Hostname()
	if host == "" || strings.ContainsAny(host, "\r\n") {
		return nil, fmt.Errorf("invalid SSH host")
	}
	userHost := host
	if r.url.User != nil {
		userHost = r.url.User.Username() + "@" + host
	}
	args := []string{"-T", "-oBatchMode=yes", "-oStrictHostKeyChecking=yes"}
	if port := r.url.Port(); port != "" {
		portNumber, err := strconv.Atoi(port)
		if err != nil || portNumber < 1 || portNumber > 65535 {
			return nil, fmt.Errorf("invalid SSH port")
		}
		args = append(args, "-p", port)
	}
	remotePath := path.Join(r.url.Path, clean)
	args = append(args, "--", userHost, "LC_ALL=C cat -- "+shellQuote(remotePath))
	cmd := exec.CommandContext(ctx, "ssh", args...)
	stderr := &limitedDiagnostic{}
	cmd.Stderr = stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		return nil, fmt.Errorf("start SSH repository read: %w", err)
	}
	return &sshReadCloser{ReadCloser: stdout, cmd: cmd, stderr: stderr}, nil
}
func (sshRepositoryReader) Close() error { return nil }

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }

type sshReadCloser struct {
	io.ReadCloser
	cmd     *exec.Cmd
	stderr  *limitedDiagnostic
	done    bool
	waitErr error
}

type limitedDiagnostic struct{ data []byte }

func (b *limitedDiagnostic) Write(p []byte) (int, error) {
	const limit = 64 << 10
	remaining := limit - len(b.data)
	if remaining > 0 {
		take := len(p)
		if take > remaining {
			take = remaining
		}
		b.data = append(b.data, p[:take]...)
	}
	return len(p), nil
}
func (b *limitedDiagnostic) String() string { return string(b.data) }

func (r *sshReadCloser) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if err == io.EOF && !r.done {
		r.done = true
		r.waitErr = r.cmd.Wait()
		if r.waitErr != nil {
			message := strings.TrimSpace(r.stderr.String())
			if strings.Contains(message, "No such file or directory") {
				return n, fmt.Errorf("%w: %s", os.ErrNotExist, message)
			}
			return n, fmt.Errorf("SSH repository read failed: %w: %s", r.waitErr, message)
		}
	}
	return n, err
}
func (r *sshReadCloser) Close() error {
	if !r.done {
		r.done = true
		_ = r.ReadCloser.Close()
		r.waitErr = r.cmd.Wait()
	}
	return r.waitErr
}

func repositoryRelativePath(value string) (string, error) {
	if value == "" || strings.Contains(value, "\\") || strings.ContainsRune(value, 0) || strings.HasPrefix(value, "/") {
		return "", fmt.Errorf("invalid repository-relative path %q", value)
	}
	clean := path.Clean(value)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || clean != value {
		return "", fmt.Errorf("repository path escapes or is not canonical: %q", value)
	}
	return clean, nil
}
