package aptpackage

import "fmt"

func validJSONKeys(c Control) error {
	if c.Name == "" {
		return fmt.Errorf("invalid JSON schema: name is required")
	}
	if c.Version == "" {
		return fmt.Errorf("invalid JSON schema: version is required")
	}
	if c.Architecture == "" {
		return fmt.Errorf("invalid JSON schema: architecture is required")
	}
	if c.Maintainer == "" {
		return fmt.Errorf("invalid JSON schema: maintainer is required")
	}
	if c.Description == "" {
		return fmt.Errorf("invalid JSON schema: description is required")
	}
	return validateMetadataKeys(c.Metadata)
}

func JSONBuild(cfg Config) error {
	if err := validJSONKeys(cfg.Control); err != nil {
		return err
	}
	return InitPackage(cfg)
}
