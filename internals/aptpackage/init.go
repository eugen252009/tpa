package aptpackage

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

func InitPackage(cfg Config) error {
	var control string
	var err error
	if cfg.Provenance != nil && !*cfg.Provenance {
		control, err = cfg.Control.Render()
	} else {
		createdAt, timeErr := packageProvenanceTime()
		if timeErr != nil {
			return timeErr
		}
		control, err = cfg.Control.RenderWithProvenance(createdAt)
	}
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(cfg.OutDir, "DEBIAN"), 0o755); err != nil {
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

func packageProvenanceTime() (time.Time, error) {
	value, ok := os.LookupEnv("SOURCE_DATE_EPOCH")
	if !ok {
		return time.Now().UTC(), nil
	}
	epoch, err := strconv.ParseInt(value, 10, 64)
	if err != nil || epoch < 0 {
		return time.Time{}, fmt.Errorf("invalid SOURCE_DATE_EPOCH %q", value)
	}
	instant := time.Unix(epoch, 0).UTC()
	if instant.Year() < 1 || instant.Year() > 9999 {
		return time.Time{}, fmt.Errorf("SOURCE_DATE_EPOCH is outside the supported package provenance date range")
	}
	return instant, nil
}
