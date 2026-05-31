package aptpackage

import "fmt"

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

func (c *Config) ToControl() Control {
	control := Control{
		Name:         c.Control.Name,
		Version:      c.Control.Version,
		Architecture: c.Control.Architecture,
		Maintainer:   c.Control.Maintainer,
		Description:  c.Control.Description,
		Depends:      "",
		Homepage:     "",
		Section:      "",
		Priority:     "",
		PreInstBody:  "",
		PostInstBody: "",
		PreRmBody:    "",
		PostRmBody:   "",
		PreDepends:   "",
		Recommends:   "",
		Suggests:     "",
		Breaks:       "",
		Conflicts:    "",
		Replaces:     "",
		Provides:     "",
		BuiltUsing:   "",
		Essential:    "",
		MultiArch:    "",
	}

	return control
}

func JSONBuild(cfg Config) error {
	cfg.ToControl()
	if !validJSONKeys(cfg.Control) {
		return fmt.Errorf("%s", "invalid JSON schema")
	}
	err := InitPackage(cfg)
	if err != nil {
		return err
	}
	return nil
}
