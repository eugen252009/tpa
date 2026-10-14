package debian

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

const controlPolicyURL = "https://www.debian.org/doc/debian-policy/ch-controlfields.html"
const relationshipPolicyURL = "https://www.debian.org/doc/debian-policy/ch-relationships.html"

// Diagnostic is a user-actionable failure. URLs are explanatory, never trust inputs.
type Diagnostic struct {
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
	URL     string `json:"url"`
}

func (d Diagnostic) Error() string {
	if d.Field == "" {
		return d.Message + "\nSee: " + d.URL
	}
	return d.Field + ": " + d.Message + "\nSee: " + d.URL
}

// ErrorDiagnostics preserves all independent errors and their wrapping context.
func ErrorDiagnostics(err error) []Diagnostic {
	if err == nil {
		return []Diagnostic{}
	}
	if d, ok := err.(Diagnostic); ok {
		return []Diagnostic{d}
	}
	if group, ok := err.(interface{ Unwrap() []error }); ok {
		var result []Diagnostic
		for _, child := range group.Unwrap() {
			result = append(result, ErrorDiagnostics(child)...)
		}
		return result
	}
	if wrapper, ok := err.(interface{ Unwrap() error }); ok {
		result := ErrorDiagnostics(wrapper.Unwrap())
		prefix := strings.TrimSuffix(err.Error(), wrapper.Unwrap().Error())
		for i := range result {
			result[i].Message = prefix + result[i].Message
		}
		return result
	}
	return []Diagnostic{{Message: err.Error(), URL: "https://github.com/eugen252009/tpa#readme"}}
}

type CheckReport struct {
	Valid       bool         `json:"valid"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

var debianVersion = regexp.MustCompile(`^(?:[0-9]+:)?[0-9][A-Za-z0-9.+:~\-]*$`)
var debianRevision = regexp.MustCompile(`^[A-Za-z0-9+.~]+$`)
var dependencyAlternative = regexp.MustCompile(`^([a-z0-9][a-z0-9+.-]+)(?::([a-z0-9][a-z0-9-]*))?(?:\s*\(\s*(<<|<=|=|>=|>>)\s*([^\s()]+)\s*\))?$`)

func ValidVersion(value string) bool {
	if !debianVersion.MatchString(value) {
		return false
	}
	// A colon must delimit a numeric epoch; it is otherwise only allowed in
	// the upstream part when an epoch exists.
	upstream := value
	if colon := strings.IndexByte(value, ':'); colon >= 0 {
		for _, r := range value[:colon] {
			if r < '0' || r > '9' {
				return false
			}
		}
		epoch := strings.TrimLeft(value[:colon], "0")
		if len(epoch) > 10 || len(epoch) == 10 && epoch > "2147483647" {
			return false
		}
		upstream = value[colon+1:]
	}
	if upstream == "" || upstream[0] < '0' || upstream[0] > '9' {
		return false
	}
	if dash := strings.LastIndexByte(upstream, '-'); dash >= 0 {
		return dash > 0 && debianRevision.MatchString(upstream[dash+1:])
	}
	return true
}

// ValidateControl collects supported binary-package metadata checks. It is not
// a replacement for Lintian, dependency resolution, or runtime testing.
func ValidateControl(c Fields) error {
	var problems []error
	add := func(field, message, anchor string) {
		problems = append(problems, Diagnostic{field, message, controlPolicyURL + anchor})
	}
	if len(c["Name"]) < 2 || !ValidPackageName(c["Name"]) {
		message := fmt.Sprintf("invalid package name %q; use at least two characters, starting with a lowercase letter or digit; only lowercase letters, digits, '+', '-', and '.' are allowed", c["Name"])
		if strings.ContainsAny(c["Name"], "ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
			message = fmt.Sprintf("invalid package name %q: uppercase letters in the name are not allowed; use lowercase letters (for example, my-new-apt-package)", c["Name"])
		}
		add("Package", message, "#package")
	}
	if !ValidVersion(c["Version"]) {
		add("Version", fmt.Sprintf("invalid version %q; use [epoch:]upstream_version[-debian_revision], starting the upstream version with a digit (for example, 1.2.3 or 1.2.3~rc1)", c["Version"]), "#version")
	}
	if !ValidArchitecture(c["Architecture"]) || c["Architecture"] == "any" || strings.HasPrefix(c["Architecture"], "any-") || strings.HasSuffix(c["Architecture"], "-any") || c["Architecture"] == "x86-64" || c["Architecture"] == "aarch64" {
		add("Architecture", fmt.Sprintf("invalid binary package architecture %q; use one Debian architecture such as amd64 or arm64, or all for architecture-independent content (not x86_64/aarch64 or source-package wildcards)", c["Architecture"]), "#architecture")
	}
	for _, field := range []struct{ name, value, anchor string }{
		{"Maintainer", c["Maintainer"], "#maintainer"}, {"Description", c["Description"], "#description"},
	} {
		if strings.TrimSpace(field.value) == "" {
			add(field.name, "required field is empty", field.anchor)
		}
	}
	for _, field := range []struct{ name, value string }{
		{"Package", c["Name"]}, {"Version", c["Version"]}, {"Architecture", c["Architecture"]}, {"Maintainer", c["Maintainer"]},
		{"Homepage", c["Homepage"]}, {"Section", c["Section"]}, {"Priority", c["Priority"]}, {"Essential", c["Essential"]}, {"Multi-Arch", c["MultiArch"]},
	} {
		if strings.ContainsFunc(field.value, unicode.IsControl) {
			add(field.name, "must be a single line without control characters; do not inject additional control fields", "#syntax-of-control-files")
		}
	}
	if strings.ContainsAny(c["Description"], "\r\x00") {
		add("Description", "contains a forbidden control character; use newline-separated description lines", "#description")
	}
	for _, field := range []struct{ name, value, allowed, anchor string }{
		{"Priority", c["Priority"], "required important standard optional extra", "#priority"},
		{"Essential", c["Essential"], "yes no", "#essential"},
		{"Multi-Arch", c["MultiArch"], "no same foreign allowed", "#multi-arch"},
	} {
		if field.value == "" {
			continue
		}
		valid := false
		for _, allowed := range strings.Fields(field.allowed) {
			if field.value == allowed {
				valid = true
			}
		}
		if !valid {
			add(field.name, "invalid value; choose one of: "+field.allowed, field.anchor)
		}
	}
	if c["Architecture"] == "all" && c["MultiArch"] == "same" {
		add("Multi-Arch", "same cannot be combined with Architecture: all; choose the intended architecture/policy explicitly", "#multi-arch")
	}
	for _, field := range []struct{ name, value string }{
		{"Depends", c["Depends"]}, {"Pre-Depends", c["PreDepends"]}, {"Recommends", c["Recommends"]}, {"Suggests", c["Suggests"]},
		{"Breaks", c["Breaks"]}, {"Conflicts", c["Conflicts"]}, {"Replaces", c["Replaces"]}, {"Provides", c["Provides"]}, {"Built-Using", c["BuiltUsing"]},
	} {
		if field.value == "" {
			continue
		}
		valid := !strings.ContainsAny(field.value, "\r\n\x00")
		for _, group := range strings.Split(field.value, ",") {
			alternatives := strings.Split(group, "|")
			if len(alternatives) > 1 && (field.name == "Provides" || field.name == "Built-Using" || field.name == "Breaks" || field.name == "Conflicts" || field.name == "Replaces") {
				valid = false
			}
			for _, alternative := range alternatives {
				match := dependencyAlternative.FindStringSubmatch(strings.TrimSpace(alternative))
				if match == nil {
					valid = false
					continue
				}
				if match[4] != "" && !ValidVersion(match[4]) {
					valid = false
				}
				if field.name == "Provides" && match[3] != "" && match[3] != "=" {
					valid = false
				}
				if field.name == "Built-Using" && match[3] != "=" {
					valid = false
				}
			}
		}
		if !valid {
			problems = append(problems, Diagnostic{field.name, "invalid binary-package relationship; use comma-separated package names, optional alternatives with | where supported, and versions such as libc6 (>= 2.36); Provides and Built-Using only support exact (=) versions; source-only restrictions and unresolved ${variables} do not belong in a built binary control file", relationshipPolicyURL})
		}
	}
	return errors.Join(problems...)
}

// Fields uses the existing TPA modeled field names (Name, Version, Depends, ...).
// Unknown/custom fields are validated by Control.Render in the TPA schema layer.
type Fields map[string]string

func ValidPackageName(s string) bool {
	if len(s) < 2 {
		return false
	}
	for i, r := range s {
		valid := r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
		if !valid && (i == 0 || (r != '+' && r != '-' && r != '.')) {
			return false
		}
	}
	return true
}
func ValidArchitecture(s string) bool {
	if s == "" || s == "source" {
		return false
	}
	for i, r := range s {
		valid := r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
		if !valid && (i == 0 || r != '-') {
			return false
		}
	}
	return true
}

// ValidateField applies the same rules used for complete binary controls.
// Cross-field restrictions require ValidateControl.
func ValidateField(field, value string) error {
	names := map[string]string{"package": "Name", "name": "Name", "version": "Version", "architecture": "Architecture", "dependency": "Depends", "depends": "Depends", "maintainer": "Maintainer", "description": "Description", "priority": "Priority", "essential": "Essential", "multi-arch": "MultiArch", "provides": "Provides", "built-using": "BuiltUsing", "pre-depends": "PreDepends", "recommends": "Recommends", "suggests": "Suggests", "breaks": "Breaks", "conflicts": "Conflicts", "replaces": "Replaces"}
	name, ok := names[strings.ToLower(field)]
	if !ok {
		return fmt.Errorf("unsupported metadata field %q", field)
	}
	c := Fields{"Name": "validation-fixture", "Version": "1.0", "Architecture": "amd64", "Maintainer": "Validation", "Description": "Validation"}
	c[name] = value
	return ValidateControl(c)
}
