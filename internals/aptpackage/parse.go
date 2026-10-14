package aptpackage

import (
	"fmt"
	"strings"
)

func ParsePackage(path string) (Control, error) {
	output, err := readPackageControl(path)
	if err != nil {
		return Control{}, err
	}
	return ParseControl(output)
}

func ParseControl(output []byte) (Control, error) {
	if len(output) > maxControlFileSize {
		return Control{}, Diagnostic{"control", "control metadata exceeds the supported size limit", controlPolicyURL}
	}
	paragraphs, err := parseRepositoryParagraphs(output, false)
	if err != nil {
		return Control{}, Diagnostic{"control", err.Error() + "; use Field: value, indent continuation lines, and define each field only once", controlPolicyURL + "#syntax-of-control-files"}
	}
	if len(paragraphs) == 0 {
		return Control{}, ValidateControl(Control{})
	}
	if len(paragraphs) != 1 {
		return Control{}, Diagnostic{"control", fmt.Sprintf("expected one package control stanza, got %d; use an indented dot for blank description lines", len(paragraphs)), controlPolicyURL + "#description"}
	}
	c := controlFromFields(paragraphs[0])
	return c, ValidateControl(c)
}

// controlFromFields preserves unknown fields without treating index-derived
// Filename/Size/SHA256 as user-supplied metadata overrides.
func controlFromFields(fields map[string]string) Control {
	c := Control{Metadata: make(map[string]any)}
	targets := map[string]*string{
		"package": &c.Name, "version": &c.Version, "architecture": &c.Architecture,
		"maintainer": &c.Maintainer, "description": &c.Description, "depends": &c.Depends,
		"homepage": &c.Homepage, "section": &c.Section, "priority": &c.Priority,
		"pre-depends": &c.PreDepends, "recommends": &c.Recommends, "suggests": &c.Suggests,
		"breaks": &c.Breaks, "conflicts": &c.Conflicts, "replaces": &c.Replaces,
		"provides": &c.Provides, "built-using": &c.BuiltUsing, "essential": &c.Essential, "multi-arch": &c.MultiArch,
	}
	for key, value := range fields {
		lower := strings.ToLower(key)
		if target, ok := targets[lower]; ok {
			if lower != "description" {
				value = strings.ReplaceAll(value, "\n", " ")
			}
			*target = value
		} else {
			c.Metadata[key] = value
		}
	}
	return c
}
