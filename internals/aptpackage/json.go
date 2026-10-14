package aptpackage

import "errors"

func validJSONKeys(c Control) error {
	return errors.Join(ValidateControl(c), validateMetadataKeys(c.Metadata))
}

func JSONBuild(cfg Config) error {
	if err := validJSONKeys(cfg.Control); err != nil {
		return err
	}
	return InitPackage(cfg)
}
