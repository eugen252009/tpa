package aptpackage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

func validDebianVersion(value string) bool {
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
func ValidateControl(c Control) error {
	var problems []error
	add := func(field, message, anchor string) {
		problems = append(problems, Diagnostic{field, message, controlPolicyURL + anchor})
	}
	if len(c.Name) < 2 || !validRepositoryPackageName(c.Name) {
		message := fmt.Sprintf("invalid package name %q; use at least two characters, starting with a lowercase letter or digit; only lowercase letters, digits, '+', '-', and '.' are allowed", c.Name)
		if strings.ContainsAny(c.Name, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
			message = fmt.Sprintf("invalid package name %q: uppercase letters in the name are not allowed; use lowercase letters (for example, my-new-apt-package)", c.Name)
		}
		add("Package", message, "#package")
	}
	if !validDebianVersion(c.Version) {
		add("Version", fmt.Sprintf("invalid version %q; use [epoch:]upstream_version[-debian_revision], starting the upstream version with a digit (for example, 1.2.3 or 1.2.3~rc1)", c.Version), "#version")
	}
	if !validRepositoryArchitecture(c.Architecture) || c.Architecture == "any" || strings.HasPrefix(c.Architecture, "any-") || strings.HasSuffix(c.Architecture, "-any") || c.Architecture == "x86-64" || c.Architecture == "aarch64" {
		add("Architecture", fmt.Sprintf("invalid binary package architecture %q; use one Debian architecture such as amd64 or arm64, or all for architecture-independent content (not x86_64/aarch64 or source-package wildcards)", c.Architecture), "#architecture")
	}
	for _, field := range []struct{ name, value, anchor string }{
		{"Maintainer", c.Maintainer, "#maintainer"}, {"Description", c.Description, "#description"},
	} {
		if strings.TrimSpace(field.value) == "" {
			add(field.name, "required field is empty", field.anchor)
		}
	}
	for _, field := range []struct{ name, value string }{
		{"Package", c.Name}, {"Version", c.Version}, {"Architecture", c.Architecture}, {"Maintainer", c.Maintainer},
		{"Homepage", c.Homepage}, {"Section", c.Section}, {"Priority", c.Priority}, {"Essential", c.Essential}, {"Multi-Arch", c.MultiArch},
	} {
		if strings.ContainsFunc(field.value, unicode.IsControl) {
			add(field.name, "must be a single line without control characters; do not inject additional control fields", "#syntax-of-control-files")
		}
	}
	if strings.ContainsAny(c.Description, "\r\x00") {
		add("Description", "contains a forbidden control character; use newline-separated description lines", "#description")
	}
	for _, field := range []struct{ name, value, allowed, anchor string }{
		{"Priority", c.Priority, "required important standard optional extra", "#priority"},
		{"Essential", c.Essential, "yes no", "#essential"},
		{"Multi-Arch", c.MultiArch, "no same foreign allowed", "#multi-arch"},
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
	if c.Architecture == "all" && c.MultiArch == "same" {
		add("Multi-Arch", "same cannot be combined with Architecture: all; choose the intended architecture/policy explicitly", "#multi-arch")
	}
	for _, field := range []struct{ name, value string }{
		{"Depends", c.Depends}, {"Pre-Depends", c.PreDepends}, {"Recommends", c.Recommends}, {"Suggests", c.Suggests},
		{"Breaks", c.Breaks}, {"Conflicts", c.Conflicts}, {"Replaces", c.Replaces}, {"Provides", c.Provides}, {"Built-Using", c.BuiltUsing},
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
				if match[4] != "" && !validDebianVersion(match[4]) {
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

// CheckPackage is read-only, including maintainer-script permissions. Build
// handles executable modes automatically, so those are not reported as errors.
func CheckPackage(root string) (CheckReport, error) {
	var problems []error
	data, err := readRepositoryTreeFile(filepath.Join(root, "DEBIAN", "control"), maxControlFileSize)
	if err != nil {
		problems = append(problems, Diagnostic{"DEBIAN/control", fmt.Sprintf("cannot read package metadata: %v; select the package root with -in", err), controlPolicyURL})
	} else {
		_, err = ParseControl(data)
		if err != nil {
			problems = append(problems, err)
		}
	}
	for _, name := range []string{"preinst", "postinst", "prerm", "postrm"} {
		info, err := os.Lstat(filepath.Join(root, "DEBIAN", name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() {
			problems = append(problems, Diagnostic{"DEBIAN/" + name, "maintainer script must be a readable regular file, not a directory or symlink", "https://www.debian.org/doc/debian-policy/ch-maintainerscripts.html"})
		}
	}
	err = errors.Join(problems...)
	return CheckReport{Valid: err == nil, Diagnostics: ErrorDiagnostics(err)}, err
}
