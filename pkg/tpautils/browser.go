package tpautils

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

//go:embed web/*
var browserFiles embed.FS

type BrowserOptions struct {
	RepositoryOptions
	Output  string // empty means the local repository root; never overwrites files
	BaseURL string // artifact repository URL, required for a separately hosted page
}
type BrowserIndex struct {
	Format     string           `json:"format"`
	Version    int              `json:"version"`
	BaseURL    string           `json:"base_url"`
	Repository RepositoryReport `json:"repository"`
}

// GenerateBrowser writes only convenience files, never APT indexes/signatures.
// Existing sidecars (including canonical TPA v1 files) are not overwritten.
// Use Output + BaseURL for TPA trees or repositories that already have a website.
func GenerateBrowser(ctx context.Context, locator string, options BrowserOptions) error {
	report, err := Inspect(ctx, locator, options.RepositoryOptions)
	if err != nil {
		return err
	}
	output := options.Output
	if output == "" {
		parsed, err := url.Parse(locator)
		if err != nil {
			return err
		}
		if parsed.Scheme != "" {
			return fmt.Errorf("remote repository requires an explicit local output directory and base URL")
		}
		output = locator
	}
	base := options.BaseURL
	if base == "" {
		sourceAbs, err := filepath.Abs(locator)
		if err != nil {
			return err
		}
		outAbs, err := filepath.Abs(output)
		if err != nil {
			return err
		}
		if sourceAbs != outAbs {
			return fmt.Errorf("separate browser output requires -base-url pointing to the hosted artifact repository")
		}
		base = "./"
	}
	u, err := url.Parse(base)
	if err != nil || u == nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "" && u.Scheme != "https" && u.Scheme != "http") || (u.Scheme == "" && u.Host != "") {
		return fmt.Errorf("base URL must be HTTP(S) or a relative repository path, without credentials/query/fragment")
	}
	if u.Scheme != "" && u.Host == "" {
		return fmt.Errorf("base URL requires a host")
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	data, err := json.MarshalIndent(BrowserIndex{"tpa-utils-repository-browser", 1, base, report}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if len(data) > 256<<20 {
		return fmt.Errorf("browser inventory exceeds 256 MiB")
	}
	info, err := os.Lstat(output)
	createdRoot := false
	if os.IsNotExist(err) {
		if err := os.MkdirAll(output, 0755); err != nil {
			return err
		}
		createdRoot = true
	} else if err != nil {
		return err
	} else if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("browser output must be a real directory")
	}
	// Never replace an existing website or the TPA format-v1 sidecar contract.
	for _, name := range []string{"index.html", "repository.json", "assets"} {
		if _, err := os.Lstat(filepath.Join(output, name)); err == nil {
			if createdRoot {
				_ = os.Remove(output)
			}
			return fmt.Errorf("browser output already contains %s; use a separate -out and -base-url", name)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	var created []string
	success := false
	createdAssets := false
	defer func() {
		if !success {
			for i := len(created) - 1; i >= 0; i-- {
				_ = os.Remove(created[i])
			}
			if createdAssets {
				_ = os.Remove(filepath.Join(output, "assets"))
			}
			if createdRoot {
				_ = os.Remove(output)
			}
		}
	}()
	if err := os.Mkdir(filepath.Join(output, "assets"), 0755); err != nil {
		return err
	}
	createdAssets = true
	for _, name := range []string{"assets/app.js", "assets/style.css", "repository.json", "index.html"} {
		if err := ctx.Err(); err != nil {
			return err
		}
		content := data
		if name != "repository.json" {
			content, err = browserFiles.ReadFile("web/" + filepath.Base(name))
			if err != nil {
				return err
			}
		}
		path := filepath.Join(output, filepath.FromSlash(name))
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if err != nil {
			return err
		}
		created = append(created, path)
		_, writeErr := f.Write(content)
		closeErr := f.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	success = true
	return nil
}
