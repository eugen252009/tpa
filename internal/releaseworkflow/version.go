package releaseworkflow

import (
	"fmt"
	"regexp"
	"strings"
)

// The accepted prerelease grammar is intentionally narrower than SemVer. It
// preserves Debian ordering for alpha, beta, and rc releases while rejecting
// build metadata and arbitrary identifiers whose Debian ordering is ambiguous.
var releaseTagRE = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-(alpha|beta|rc)(?:\.(0|[1-9][0-9]*))?)?$`)

func DebianVersionFromTag(tag string) (string, error) {
	matches := releaseTagRE.FindStringSubmatch(tag)
	if matches == nil {
		return "", fmt.Errorf("tag %q is not a supported release tag (expected vMAJOR.MINOR.PATCH with optional -alpha[.N], -beta[.N], or -rc[.N])", tag)
	}
	version := strings.Join(matches[1:4], ".")
	if matches[4] != "" {
		version += "~" + matches[4]
		if matches[5] != "" {
			version += "." + matches[5]
		}
	}
	return version, nil
}
