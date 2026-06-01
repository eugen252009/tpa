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
