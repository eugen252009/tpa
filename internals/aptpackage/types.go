// Package aptpackage implements Debian package and APT repository construction.
package aptpackage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
)

const (
	JSONSCHEMA = `export interface Config {
    control: ControlClass;
    scripts?: MaintainerScripts;
    provenance?: boolean;
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

    // Additional user-defined control fields; TPA accepts string, number,
    // and boolean scalar values and normalizes keys to Debian field names.
    [key: string]: unknown;
}

export interface MaintainerScripts {
    preinst?:  string;
    postinst?: string;
    prerm?:   string;
    postrm?:   string;
}

export interface Repo {
    origin:      string;
    label:       string;
    suite:       string;
    components:  string;
    codename:    string;
    description: string;
}
`
	TPAVersion = "0.5.0"
)

type RepoConfig struct {
	Origin      string `json:"origin"`
	Label       string `json:"label"`
	Suite       string `json:"suite"`
	Components  string `json:"components"`
	Codename    string `json:"codename"`
	Description string `json:"description"`
}

type MaintainerScripts struct {
	PreInst  string `json:"preinst,omitempty"`
	PostInst string `json:"postinst,omitempty"`
	PreRm    string `json:"prerm,omitempty"`
	PostRm   string `json:"postrm,omitempty"`
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

	// Metadata contains arbitrary scalar package-control values. JSON input
	// preserves the original key spelling; Debian output uses normalized field
	// names. It is intentionally excluded from the JSON struct fields above so
	// known fields remain typed.
	Metadata map[string]any `json:"-"`

	legacyScripts map[string]string
}

type Config struct {
	Control    Control           `json:"control"`
	Scripts    MaintainerScripts `json:"scripts,omitempty"`
	Provenance *bool             `json:"provenance,omitempty"`
	Repo       RepoConfig        `json:"repo"`

	InDir  string `json:"indir"`
	OutDir string `json:"outdir"`
	GPG    string `json:"gpg"`

	// Workers limits independent package-local repository work. It is a Go/CLI
	// execution option, not repository metadata or JSON configuration.
	Workers int `json:"-"`
}

var knownJSONControlKeys = map[string]bool{
	"name": true, "version": true, "architecture": true, "maintainer": true,
	"description": true, "depends": true, "homepage": true, "section": true,
	"priority": true, "preDepends": true, "recommends": true, "suggests": true,
	"breaks": true, "conflicts": true, "replaces": true, "provides": true,
	"builtUsing": true, "essential": true, "multiArch": true,
	"preinstbody": true, "postinstbody": true, "prermbody": true, "postrmbody": true,
}

var reservedControlFields = map[string]bool{
	"Package": true, "Version": true, "Architecture": true, "Maintainer": true,
	"Description": true, "Depends": true, "Homepage": true, "Section": true,
	"Priority": true, "Pre-Depends": true, "Recommends": true, "Suggests": true,
	"Breaks": true, "Conflicts": true, "Replaces": true, "Provides": true,
	"Built-Using": true, "Essential": true, "Multi-Arch": true,
	"Filename": true, "Size": true, "SHA256": true,
}

func (c *Control) UnmarshalJSON(data []byte) error {
	type plainControl Control
	var decoded plainControl = plainControl(*c)
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	metadata := make(map[string]any)
	legacy := make(map[string]string)
	for key, value := range raw {
		if knownJSONControlKeys[key] {
			if key == "preinstbody" || key == "postinstbody" || key == "prermbody" || key == "postrmbody" {
				var script string
				if err := json.Unmarshal(value, &script); err != nil {
					return fmt.Errorf("legacy script %q must be a string: %w", key, err)
				}
				legacy[key] = script
			}
			continue
		}
		scalar, err := decodeScalar(value)
		if err != nil {
			return fmt.Errorf("control metadata %q: %w", key, err)
		}
		if err := validateMetadataValue(key, scalar); err != nil {
			return err
		}
		metadata[key] = scalar
	}
	if err := validateMetadataKeys(metadata); err != nil {
		return err
	}
	*c = Control(decoded)
	c.Metadata = metadata
	c.legacyScripts = legacy
	return nil
}

func (c Control) MarshalJSON() ([]byte, error) {
	type plainControl Control
	base, err := json.Marshal(plainControl(c))
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(base, &fields); err != nil {
		return nil, err
	}
	for key, value := range c.Metadata {
		if err := validateMetadataValue(key, value); err != nil {
			return nil, err
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		fields[key] = encoded
	}
	return json.Marshal(fields)
}

func (c *Config) UnmarshalJSON(data []byte) error {
	type plainConfig Config
	decoded := plainConfig(*c)
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	for key, value := range decoded.Control.legacyScripts {
		switch key {
		case "preinstbody":
			if decoded.Scripts.PreInst == "" {
				decoded.Scripts.PreInst = value
			}
		case "postinstbody":
			if decoded.Scripts.PostInst == "" {
				decoded.Scripts.PostInst = value
			}
		case "prermbody":
			if decoded.Scripts.PreRm == "" {
				decoded.Scripts.PreRm = value
			}
		case "postrmbody":
			if decoded.Scripts.PostRm == "" {
				decoded.Scripts.PostRm = value
			}
		}
	}
	decoded.Control.legacyScripts = nil
	*c = Config(decoded)
	return nil
}

func (c Control) Render() (string, error) {
	return c.render(nil)
}

func (c Control) RenderWithProvenance(now time.Time) (string, error) {
	metadata := cloneMetadata(c.Metadata)
	if !hasMetadataField(metadata, "TPA-Version") {
		metadata["tpaVersion"] = TPAVersion
	}
	if !hasMetadataField(metadata, "Created-At") {
		metadata["createdAt"] = now.UTC().Format(time.RFC3339)
	}
	return c.render(metadata)
}

func (c Control) render(metadata map[string]any) (string, error) {
	if metadata == nil {
		metadata = c.Metadata
	}
	if err := validateMetadataKeys(metadata); err != nil {
		return "", err
	}
	var b strings.Builder
	known := []struct {
		Label string
		Value string
	}{
		{"Package", c.Name}, {"Version", c.Version}, {"Architecture", c.Architecture},
		{"Maintainer", c.Maintainer}, {"Description", c.Description},
		{"Depends", c.Depends}, {"Homepage", c.Homepage}, {"Section", c.Section},
		{"Priority", c.Priority}, {"Pre-Depends", c.PreDepends},
		{"Recommends", c.Recommends}, {"Suggests", c.Suggests}, {"Breaks", c.Breaks},
		{"Conflicts", c.Conflicts}, {"Replaces", c.Replaces}, {"Provides", c.Provides},
		{"Built-Using", c.BuiltUsing}, {"Essential", c.Essential}, {"Multi-Arch", c.MultiArch},
	}
	for _, field := range known {
		if field.Value != "" {
			fmt.Fprintf(&b, "%s: %s\n", field.Label, field.Value)
		}
	}

	keys := make([]string, 0, len(metadata))
	for key := range metadata {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		field, err := metadataFieldName(key)
		if err != nil {
			return "", err
		}
		value := scalarString(metadata[key])
		fmt.Fprintf(&b, "%s: %s\n", field, value)
	}
	return b.String(), nil
}

func cloneMetadata(source map[string]any) map[string]any {
	if len(source) == 0 {
		return make(map[string]any)
	}
	clone := make(map[string]any, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

func hasMetadataField(metadata map[string]any, wanted string) bool {
	for key := range metadata {
		field, err := metadataFieldName(key)
		if err == nil && field == wanted {
			return true
		}
	}
	return false
}

func decodeScalar(raw json.RawMessage) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	switch value.(type) {
	case string, bool, json.Number:
		return value, nil
	default:
		return nil, fmt.Errorf("only string, number, and boolean values are supported")
	}
}

func scalarString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case json.Number:
		return typed.String()
	default:
		return fmt.Sprint(typed)
	}
}

func validateMetadataValue(key string, value any) error {
	if _, err := metadataFieldName(key); err != nil {
		return fmt.Errorf("invalid control metadata key %q: %w", key, err)
	}
	switch typed := value.(type) {
	case string:
		for _, r := range typed {
			if r < 0x20 || r == 0x7f {
				return fmt.Errorf("control metadata %q contains a control character", key)
			}
		}
	case bool, json.Number:
		return nil
	default:
		return fmt.Errorf("control metadata %q must be a string, number, or boolean", key)
	}
	return nil
}

func validateMetadataKeys(metadata map[string]any) error {
	seen := make(map[string]string, len(metadata))
	for key, value := range metadata {
		field, err := metadataFieldName(key)
		if err != nil {
			return fmt.Errorf("invalid control metadata key %q: %w", key, err)
		}
		if reservedControlFields[field] {
			return fmt.Errorf("control metadata key %q would override standard field %q", key, field)
		}
		if previous, exists := seen[field]; exists {
			return fmt.Errorf("control metadata keys %q and %q normalize to the same field %q", previous, key, field)
		}
		seen[field] = key
		if err := validateMetadataValue(key, value); err != nil {
			return err
		}
	}
	return nil
}

func metadataFieldName(key string) (string, error) {
	if strings.TrimSpace(key) == "" {
		return "", fmt.Errorf("field name is empty")
	}
	var words []string
	var word []rune
	runes := []rune(key)
	flush := func() {
		if len(word) != 0 {
			words = append(words, string(word))
			word = nil
		}
	}
	for i, current := range runes {
		if unicode.IsControl(current) || current == ':' {
			return "", fmt.Errorf("contains a control character or colon")
		}
		if current == '-' || current == '_' || unicode.IsSpace(current) {
			flush()
			continue
		}
		if len(word) > 0 && unicode.IsUpper(current) {
			previous := runes[i-1]
			var next rune
			if i+1 < len(runes) {
				next = runes[i+1]
			}
			if unicode.IsLower(previous) || (unicode.IsUpper(previous) && unicode.IsLower(next)) {
				flush()
			}
		}
		word = append(word, current)
	}
	flush()
	if len(words) == 0 {
		return "", fmt.Errorf("does not contain a field name")
	}
	for i, word := range words {
		letters := []rune(strings.ToLower(word))
		letters[0] = unicode.ToUpper(letters[0])
		words[i] = string(letters)
	}
	field := strings.Join(words, "-")
	if strings.EqualFold(field, "TPA-Version") {
		field = "TPA-Version"
	}
	for _, r := range field {
		if r > 0x7f || !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-') {
			return "", fmt.Errorf("contains invalid field characters")
		}
	}
	return field, nil
}
