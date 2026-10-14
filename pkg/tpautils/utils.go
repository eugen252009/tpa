// Package tpautils exposes Debian/APT utilities without the TPA executable,
// accounts, or TPA.run. Metadata rules live in pkg/debian; repository readers
// are shared with tpa inspect/verify, not reimplemented here.
package tpautils

import (
	"context"
	"github.com/eugen252009/tpa/internals/aptpackage"
	"github.com/eugen252009/tpa/pkg/debian"
)

type Control = aptpackage.Control
type Diagnostic = debian.Diagnostic
type RepositoryOptions = aptpackage.RepositoryOptions
type RepositoryReport = aptpackage.RepositoryReport

func ValidateField(field, value string) error  { return debian.ValidateField(field, value) }
func CompareVersions(a, b string) (int, error) { return debian.CompareVersions(a, b) }
func ErrorDiagnostics(err error) []Diagnostic  { return debian.ErrorDiagnostics(err) }

// ValidateControl includes custom metadata validation via the existing renderer.
func ValidateControl(c Control) error { _, err := c.Render(); return err }
func Inspect(ctx context.Context, locator string, options RepositoryOptions) (RepositoryReport, error) {
	location, err := aptpackage.ParseRepositoryLocator(locator)
	if err != nil {
		return RepositoryReport{}, err
	}
	reader, err := aptpackage.OpenRepositoryReader(location)
	if err != nil {
		return RepositoryReport{}, err
	}
	defer reader.Close()
	return aptpackage.InspectRepository(ctx, reader, options)
}
