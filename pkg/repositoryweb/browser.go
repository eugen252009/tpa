// Package repositoryweb renders the shared static repository explorer. It has no
// APT reader, signer, CLI or server dependency. Inventories remain authoritative
// in their existing formats; this package only prepares presentation.
package repositoryweb

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"net/url"
	"path"
	"sort"
	"strings"
)

// Package is the existing inspect/utility package view, shared without a reader dependency.
type Package struct {
	Package      string            `json:"package"`
	Version      string            `json:"version"`
	Architecture string            `json:"architecture"`
	Filename     string            `json:"filename"`
	Size         int64             `json:"size"`
	SHA256       string            `json:"sha256"`
	Control      map[string]string `json:"control,omitempty"`
}

//go:embed web/*
var files embed.FS

const PageSize = 50

// Asset returns the exact first-party assets used by both TPA and tpa-utils.
func Asset(name string) ([]byte, error) { return files.ReadFile("web/" + name) }

// ArtifactLink encodes a clean repository-relative pool path, never executable
// metadata, URL credentials, traversal, queries or fragments.
func ArtifactLink(filename string) (string, error) {
	clean := path.Clean(filename)
	if filename == "" || strings.ContainsAny(filename, "\\\x00") || path.IsAbs(filename) || clean != filename || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || !strings.HasPrefix(clean, "pool/") {
		return "", fmt.Errorf("unsafe repository artifact filename %q", filename)
	}
	return (&url.URL{Path: filename}).EscapedPath(), nil
}

func ValidateBase(base string) error {
	u, err := url.Parse(base)
	if err != nil || u == nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(base, "\\\x00") || (u.Scheme != "" && u.Scheme != "http" && u.Scheme != "https") || (u.Scheme == "" && u.Host != "") || (u.Scheme != "" && u.Host == "") {
		return fmt.Errorf("base URL must be HTTP(S) or a relative repository path, without credentials/query/fragment")
	}
	return nil
}

type Options struct {
	InlineAssets bool
	BaseURL      string
}
type field struct{ Name, Value string }
type preview struct {
	Package
	Link   string
	Fields []field
}
type page struct {
	Inline    bool
	CSS       template.CSS
	JS        template.JS
	Count     int
	Packages  []preview
	Truncated bool
}

// Render emits bounded, escaped static previews as well as the shared interactive
// explorer. InlineAssets keeps canonical TPA trees to the original two sidecars:
// no new protocol paths, CDN, server endpoint or second package database.
// The preview remains usable when scripts/fetch are disabled by host policy.
func Render(packages []Package, options Options) ([]byte, error) {
	return render(packages, options, false)
}

// PreviousStaticHeadingForVerification reproduces only the exact pre-fix heading
// representation. Readers use it to verify existing v1 trees, never generators.
func PreviousStaticHeadingForVerification(packages []Package, options Options) ([]byte, error) {
	return render(packages, options, true)
}

func render(packages []Package, options Options, previous bool) ([]byte, error) {
	base := options.BaseURL
	if base == "" {
		base = "./"
	}
	if err := ValidateBase(base); err != nil {
		return nil, err
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	p := page{Inline: options.InlineAssets, Count: len(packages), Truncated: len(packages) > PageSize}
	for _, pkg := range packages[:min(len(packages), PageSize)] {
		v := preview{Package: pkg}
		link, err := ArtifactLink(pkg.Filename)
		if err == nil {
			v.Link = base + link
			if base == "./" {
				v.Link = link
			}
		}
		names := make([]string, 0, len(pkg.Control))
		for name := range pkg.Control {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			v.Fields = append(v.Fields, field{name, pkg.Control[name]})
		}
		p.Packages = append(p.Packages, v)
	}
	if p.Inline {
		css, err := Asset("style.css")
		if err != nil {
			return nil, err
		}
		js, err := Asset("app.js")
		if err != nil {
			return nil, err
		}
		p.CSS = template.CSS(css)
		p.JS = template.JS(js)
	} // trusted embedded assets, NEVER package text
	source, err := Asset("index.html")
	if err != nil {
		return nil, err
	}
	if previous {
		source = bytes.Replace(source, []byte("{{.Package.Package}}"), []byte("{{.Package}}"), 1)
	}
	t, err := template.New("explorer").Parse(string(source))
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := t.Execute(&out, p); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
