// Package aptpackage serves as a high-performance helper for automating
// the generation and management of Debian package archives and repository structures.
// It abstracts complex packaging, indexing, and filesystem synchronization processes,
// enabling efficient, routing-based processing of repository builds at the filesystem level.
package aptpackage

import (
	"fmt"
)

func validJSONKeys(c Control) bool {
	if c.Name == "" {
		return false
	}
	if c.Version == "" {
		return false
	}
	if c.Architecture == "" {
		return false
	}
	if c.Maintainer == "" {
		return false
	}
	if c.Description == "" {
		return false
	}
	return true
}

func JSONBuild(cfg Config) error {
	if !validJSONKeys(cfg.Control) {
		return fmt.Errorf("%s", "invalid JSON schema")
	}
	err := InitPackage(cfg)
	if err != nil {
		return err
	}
	return nil
}
