package aptpackage

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type repositoryBrowserIndex struct {
	Format   string                   `json:"format"`
	Version  int                      `json:"version"`
	Packages []repositoryBrowserEntry `json:"packages"`
}

type repositoryBrowserEntry struct {
	Metadata map[string]string         `json:"metadata"`
	Artifact repositoryBrowserArtifact `json:"artifact"`
	Fields   []repositoryBrowserField  `json:"-"`
}

type repositoryBrowserArtifact struct {
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
	Link     string `json:"-"`
}

type repositoryBrowserField struct {
	Name  string
	Value string
}

// writeRepositoryBrowserFiles builds one canonical view from the package
// indexes named by this distribution's Release file, then renders both
// convenience formats from that view.
func writeRepositoryBrowserFiles(root string, repo RepoConfig) error {
	component := repo.Components
	if component == "" {
		component = "main"
	}
	codename := repo.Codename
	if codename == "" {
		codename = "stable"
	}
	distRoot := filepath.Join(root, "dists", codename)
	releaseData, err := os.ReadFile(filepath.Join(distRoot, "Release"))
	if err != nil {
		return fmt.Errorf("read repository Release for browser index: %w", err)
	}
	checksums, err := parseReleaseChecksums(releaseData)
	if err != nil {
		return fmt.Errorf("parse repository Release for browser index: %w", err)
	}
	componentPrefix := component + "/binary-"
	var indexPaths []string
	for relative := range checksums {
		directory := path.Dir(relative)
		if path.Base(relative) != "Packages" || !strings.HasPrefix(directory, componentPrefix) {
			continue
		}
		architecture := strings.TrimPrefix(directory, componentPrefix)
		if architecture == "" || strings.Contains(architecture, "/") {
			continue
		}
		indexPath, err := safeRepositoryPath(distRoot, relative)
		if err != nil {
			return fmt.Errorf("invalid package index path %q: %w", relative, err)
		}
		info, err := os.Lstat(indexPath)
		if err != nil {
			return fmt.Errorf("inspect package index %s: %w", indexPath, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("package index is not a regular file: %s", indexPath)
		}
		indexPaths = append(indexPaths, indexPath)
	}
	sort.Strings(indexPaths)
	if len(indexPaths) == 0 {
		return fmt.Errorf("no Packages indexes found under %s", distRoot)
	}

	index := repositoryBrowserIndex{
		Format: "tpa-repository-index", Version: 1,
		Packages: make([]repositoryBrowserEntry, 0),
	}
	for _, indexPath := range indexPaths {
		data, err := os.ReadFile(indexPath)
		if err != nil {
			return fmt.Errorf("read package index %s: %w", indexPath, err)
		}
		stanzas, err := parseRawControlStanzas(data)
		if err != nil {
			return fmt.Errorf("parse package index %s: %w", indexPath, err)
		}
		for _, stanza := range stanzas {
			metadata, err := parseRepositoryBrowserMetadata(stanza.raw)
			if err != nil {
				return fmt.Errorf("parse package metadata in %s: %w", indexPath, err)
			}
			entry, err := newRepositoryBrowserEntry(metadata)
			if err != nil {
				return fmt.Errorf("prepare package metadata in %s: %w", indexPath, err)
			}
			index.Packages = append(index.Packages, entry)
		}
	}
	sort.Slice(index.Packages, func(i, j int) bool {
		first, second := index.Packages[i], index.Packages[j]
		for _, field := range []string{"Package", "Version", "Architecture"} {
			if first.Metadata[field] != second.Metadata[field] {
				return first.Metadata[field] < second.Metadata[field]
			}
		}
		return first.Artifact.Filename < second.Artifact.Filename
	})

	jsonData, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("encode repository package index: %w", err)
	}
	jsonData = append(jsonData, '\n')
	if err := os.WriteFile(filepath.Join(root, "repository.json"), jsonData, 0o644); err != nil {
		return fmt.Errorf("write repository package index: %w", err)
	}

	var htmlData bytes.Buffer
	if err := repositoryBrowserTemplate.Execute(&htmlData, index); err != nil {
		return fmt.Errorf("render repository browser: %w", err)
	}
	if err := os.WriteFile(filepath.Join(root, "index.html"), htmlData.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write repository package browser: %w", err)
	}
	return nil
}

func newRepositoryBrowserEntry(fields map[string]string) (repositoryBrowserEntry, error) {
	filename, ok := getRepositoryField(fields, "Filename")
	if !ok {
		return repositoryBrowserEntry{}, fmt.Errorf("missing Filename field")
	}
	link, err := safeRepositoryArtifactLink(filename)
	if err != nil {
		return repositoryBrowserEntry{}, err
	}
	sizeText, ok := getRepositoryField(fields, "Size")
	if !ok {
		return repositoryBrowserEntry{}, fmt.Errorf("missing Size field")
	}
	size, err := strconv.ParseInt(sizeText, 10, 64)
	if err != nil || size < 0 {
		return repositoryBrowserEntry{}, fmt.Errorf("invalid artifact size %q", sizeText)
	}
	sha256, ok := getRepositoryField(fields, "SHA256")
	if !ok {
		return repositoryBrowserEntry{}, fmt.Errorf("missing SHA256 field")
	}

	metadata := make(map[string]string, len(fields)-3)
	fieldNames := make([]string, 0, len(fields)-3)
	for name, value := range fields {
		if strings.EqualFold(name, "Filename") || strings.EqualFold(name, "Size") || strings.EqualFold(name, "SHA256") {
			continue
		}
		metadata[name] = value
		fieldNames = append(fieldNames, name)
	}
	sort.Strings(fieldNames)
	browserFields := make([]repositoryBrowserField, 0, len(fieldNames))
	for _, name := range fieldNames {
		browserFields = append(browserFields, repositoryBrowserField{Name: name, Value: metadata[name]})
	}
	return repositoryBrowserEntry{
		Metadata: metadata,
		Artifact: repositoryBrowserArtifact{Filename: filename, Size: size, SHA256: sha256, Link: link},
		Fields:   browserFields,
	}, nil
}

func getRepositoryField(fields map[string]string, name string) (string, bool) {
	for field, value := range fields {
		if strings.EqualFold(field, name) {
			return value, true
		}
	}
	return "", false
}

func safeRepositoryArtifactLink(filename string) (string, error) {
	clean := path.Clean(filename)
	if filename == "" || strings.ContainsAny(filename, "\\\x00") || path.IsAbs(filename) ||
		clean != filename || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") ||
		!strings.HasPrefix(clean, "pool/") {
		return "", fmt.Errorf("unsafe repository artifact filename %q", filename)
	}
	// Escape URL-significant characters so metadata cannot add a query,
	// fragment, or encoded traversal to the repository-relative link.
	return (&url.URL{Path: filename}).EscapedPath(), nil
}

func parseRepositoryBrowserMetadata(raw []byte) (map[string]string, error) {
	metadata := make(map[string]string)
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	lastField := ""
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			if lastField == "" {
				return nil, fmt.Errorf("control continuation without a field")
			}
			metadata[lastField] += "\n" + strings.TrimLeft(line, " \t")
			continue
		}
		colon := strings.IndexByte(line, ':')
		if colon <= 0 {
			return nil, fmt.Errorf("invalid control field %q", line)
		}
		lastField = line[:colon]
		metadata[lastField] = strings.TrimSpace(line[colon+1:])
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return metadata, nil
}

var repositoryBrowserTemplate = template.Must(template.New("repository-browser").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Package repository</title>
  <style>
    :root { color-scheme: light dark; font: 16px/1.5 system-ui, sans-serif; }
    body { max-width: 1200px; margin: 2rem auto; padding: 0 1rem; }
    h1 { margin-bottom: .25rem; }
    .muted { opacity: .72; }
    table { width: 100%; border-collapse: collapse; margin-top: 1rem; }
    th, td { text-align: left; vertical-align: top; padding: .8rem; border: 1px solid #8886; }
    th { overflow-wrap: anywhere; }
    dl { display: grid; grid-template-columns: minmax(8rem, 1fr) 3fr; gap: .2rem 1rem; margin: 0; }
    dt { font-weight: 650; overflow-wrap: anywhere; }
    dd { margin: 0; white-space: pre-wrap; overflow-wrap: anywhere; }
    .artifact { overflow-wrap: anywhere; }
    @media (max-width: 700px) {
      table, tbody, tr, th, td { display: block; }
      thead { display: none; }
      tr { margin: 1rem 0; border: 1px solid #8886; }
      th, td { border: 0; border-bottom: 1px solid #8886; }
      dl { grid-template-columns: 1fr; gap: 0; }
      dd { margin-bottom: .5rem; }
    }
  </style>
</head>
<body>
  <h1>Package repository</h1>
  <p class="muted">{{len .Packages}} packages. Machine-readable inventory: <a href="repository.json">repository.json</a>.</p>
  <table>
    <thead><tr><th>Package / version</th><th>Architecture</th><th>Package metadata</th><th>Artifact</th></tr></thead>
    <tbody>
    {{range .Packages}}
      <tr>
        <th>{{index .Metadata "Package"}}<br>{{index .Metadata "Version"}}</th>
        <td>{{index .Metadata "Architecture"}}</td>
        <td><dl>{{range .Fields}}<dt>{{.Name}}</dt><dd>{{.Value}}</dd>{{end}}</dl></td>
        <td class="artifact"><a href="{{.Artifact.Link}}">{{.Artifact.Filename}}</a><br>Size: {{.Artifact.Size}} bytes<br>SHA256: {{.Artifact.SHA256}}</td>
      </tr>
    {{else}}
      <tr><td colspan="4">No packages are currently listed.</td></tr>
    {{end}}
    </tbody>
  </table>
</body>
</html>
`))
