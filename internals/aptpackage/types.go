// Package aptpackage serves as a high-performance helper for automating
// the generation and management of Debian package archives and repository structures.
// It abstracts complex packaging, indexing, and filesystem synchronization processes,
// enabling efficient, routing-based processing of repository builds at the filesystem level.
package aptpackage

import (
	"fmt"
	"strings"
)

const JSONSCHEMA = `export interface Control {
    control: ControlClass;
    repo:    Repo;
    indir:   string;
    outdir:  string;
    gpg:     string;
}

export interface ControlClass {
    name:         string;
    version:      string;
    architecture: string;
    maintainer:   string;
    description:  string;
    depends:      string;
    homepage:     string;
    section:      string;
    priority:     string;
    preinstbody:  string;
    postinstbody: string;
    prermbody:    string;
    postrmbody:   string;
    preDepends:   string;
    recommends:   string;
    suggests:     string;
    breaks:       string;
    conflicts:    string;
    replaces:     string;
    provides:     string;
    builtUsing:   string;
    essential:    string;
    multiArch:    string;
}

export interface Repo {
    origin:        string;
    label:         string;
    suite:         string;
    architectures: string;
    components:    string;
    codename:      string;
    description:   string;
}
`

type BuildContext struct {
	Control Control
	OutDir  string
	InDir   string
}

type PackPackage struct {
	Context BuildContext
	GPG     string
	Flat    bool
}

type RepoConfig struct {
	Origin        string `json:"origin"`
	Label         string `json:"label"`
	Suite         string `json:"suite"`
	Architectures string `json:"architectures"`
	Components    string `json:"components"`
	Codename      string `json:"codename"`
	Description   string `json:"description"`
}

type Control struct {
	Name         string `json:"name"`
	Version      string `json:"version"`
	Architecture string `json:"architecture"`
	Maintainer   string `json:"maintainer"`
	Description  string `json:"description"`
	Depends      string `json:"depends"`
	Homepage     string `json:"homepage"`
	Section      string `json:"section"`
	Priority     string `json:"priority"`

	PreInstBody  string `json:"preinstbody"`
	PostInstBody string `json:"postinstbody"`
	PreRmBody    string `json:"prermbody"`
	PostRmBody   string `json:"postrmbody"`

	PreDepends string `json:"preDepends"`
	Recommends string `json:"recommends"`
	Suggests   string `json:"suggests"`
	Breaks     string `json:"breaks"`
	Conflicts  string `json:"conflicts"`
	Replaces   string `json:"replaces"`
	Provides   string `json:"provides"`
	BuiltUsing string `json:"builtUsing"`

	Essential string `json:"essential"`
	MultiArch string `json:"multiArch"`
}

type Config struct {
	Control Control    `json:"control"`
	Repo    RepoConfig `json:"repo"`

	InDir  string `json:"indir"`
	OutDir string `json:"outdir"`
	GPG    string `json:"gpg"`
}

func (c *Control) Render() string {
	var b strings.Builder

	fmt.Fprintf(&b, "Package: %s\n", c.Name)
	fmt.Fprintf(&b, "Version: %s\n", c.Version)
	fmt.Fprintf(&b, "Architecture: %s\n", c.Architecture)
	fmt.Fprintf(&b, "Maintainer: %s\n", c.Maintainer)
	fmt.Fprintf(&b, "Description: %s\n", c.Description)

	fields := []struct {
		Label string
		Value string
	}{
		{"Depends", c.Depends},
		{"Homepage", c.Homepage},
		{"Section", c.Section},
		{"Priority", c.Priority},
		{"Pre-Depends", c.PreDepends},
		{"Recommends", c.Recommends},
		{"Suggests", c.Suggests},
		{"Breaks", c.Breaks},
		{"Conflicts", c.Conflicts},
		{"Replaces", c.Replaces},
		{"Provides", c.Provides},
		{"Built-Using", c.BuiltUsing},
		{"Essential", c.Essential},
		{"Multi-Arch", c.MultiArch},
	}

	for _, f := range fields {
		if f.Value != "" {
			fmt.Fprintf(&b, "%s: %s\n", f.Label, f.Value)
			continue
		}
	}

	return b.String()
}
