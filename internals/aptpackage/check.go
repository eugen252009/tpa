package aptpackage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/eugen252009/tpa/pkg/debian"
)

const controlPolicyURL = "https://www.debian.org/doc/debian-policy/ch-controlfields.html"

type Diagnostic = debian.Diagnostic
type CheckReport = debian.CheckReport

func ErrorDiagnostics(err error) []Diagnostic { return debian.ErrorDiagnostics(err) }
func validDebianVersion(v string) bool        { return debian.ValidVersion(v) }
func ValidateControl(c Control) error {
	return debian.ValidateControl(debian.Fields{
		"Name":         c.Name,
		"Version":      c.Version,
		"Architecture": c.Architecture,
		"Maintainer":   c.Maintainer,
		"Description":  c.Description,
		"Homepage":     c.Homepage,
		"Section":      c.Section,
		"Priority":     c.Priority,
		"Essential":    c.Essential,
		"MultiArch":    c.MultiArch,
		"Depends":      c.Depends,
		"PreDepends":   c.PreDepends,
		"Recommends":   c.Recommends,
		"Suggests":     c.Suggests,
		"Breaks":       c.Breaks,
		"Conflicts":    c.Conflicts,
		"Replaces":     c.Replaces,
		"Provides":     c.Provides,
		"BuiltUsing":   c.BuiltUsing,
	})
}

// CheckPackage is read-only, including maintainer-script permissions. Build
// handles executable modes automatically, so those are not reported as errors.
func CheckPackage(root string) (CheckReport, error) {
	var problems []error
	data, err := readRepositoryTreeFile(filepath.Join(root, "DEBIAN", "control"), maxControlFileSize)
	if err != nil {
		problems = append(problems, Diagnostic{Field: "DEBIAN/control", Message: fmt.Sprintf("cannot read package metadata: %v; select the package root with -in", err), URL: controlPolicyURL})
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
			problems = append(problems, Diagnostic{Field: "DEBIAN/" + name, Message: "maintainer script must be a readable regular file, not a directory or symlink", URL: "https://www.debian.org/doc/debian-policy/ch-maintainerscripts.html"})
		}
	}
	err = errors.Join(problems...)
	return CheckReport{Valid: err == nil, Diagnostics: ErrorDiagnostics(err)}, err
}
