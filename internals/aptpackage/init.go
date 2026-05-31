package aptpackage

import (
	"os"
)

func InitPackage(cfg Config) error {
	dirs := []string{cfg.OutDir, cfg.OutDir + "/DEBIAN", cfg.OutDir + "/usr/bin"}
	for _, dir := range dirs {
		err := os.MkdirAll(dir, 0o755)
		if err != nil {
			return err
		}
	}

	err := os.WriteFile(
		cfg.OutDir+"/DEBIAN/control",
		[]byte(cfg.Control.Render()),
		0o644)
	if err != nil {
		return err
	}
	scriptfile := []string{"preinst", "postinst", "prerm", "postrm"}
	for _, file := range scriptfile {
		data := []byte("#!/bin/sh\nset -e\n")
		if file == scriptfile[0] && cfg.Control.PreInstBody != "" {
			data = []byte(cfg.Control.PreInstBody)
		}
		if file == scriptfile[1] && cfg.Control.PostInstBody != "" {
			data = []byte(cfg.Control.PostInstBody)
		}
		if file == scriptfile[2] && cfg.Control.PreRmBody != "" {
			data = []byte(cfg.Control.PreRmBody)
		}
		if file == scriptfile[3] && cfg.Control.PostRmBody != "" {
			data = []byte(cfg.Control.PostRmBody)
		}
		err := os.WriteFile(
			cfg.OutDir+"/DEBIAN/"+file,
			data,
			0o755,
		)
		if err != nil {
			panic(err)
		}
	}
	return nil
}
