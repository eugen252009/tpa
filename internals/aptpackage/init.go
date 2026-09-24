package aptpackage

import (
	"os"
	"path/filepath"
	"time"
)

func InitPackage(cfg Config) error {
	dirs := []string{
		cfg.OutDir + "/DEBIAN",
		cfg.OutDir + "/usr/local/bin",
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	var control string
	var err error
	if cfg.Provenance != nil && !*cfg.Provenance {
		control, err = cfg.Control.Render()
	} else {
		control, err = cfg.Control.RenderWithProvenance(time.Now().UTC())
	}
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(cfg.OutDir, "DEBIAN", "control"), []byte(control), 0o644); err != nil {
		return err
	}

	scripts := map[string]string{
		"preinst":  cfg.Scripts.PreInst,
		"postinst": cfg.Scripts.PostInst,
		"prerm":    cfg.Scripts.PreRm,
		"postrm":   cfg.Scripts.PostRm,
	}
	for name, body := range scripts {
		data := []byte("#!/bin/sh\nset -e\n")
		if body != "" {
			data = []byte(body)
		}
		if err := os.WriteFile(filepath.Join(cfg.OutDir, "DEBIAN", name), data, 0o755); err != nil {
			return err
		}
	}
	return nil
}
