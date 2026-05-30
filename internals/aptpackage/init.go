package aptpackage

import (
	"fmt"
	"os"
)

func InitPackage(controlfile BuildContext) {
	dirs := []string{controlfile.OutDir, controlfile.OutDir + "/DEBIAN", controlfile.OutDir + "/usr/bin"}
	fmt.Println(dirs)
	for _, dir := range dirs {
		os.MkdirAll(dir, 0o755)
	}

	controlfile.Control.Init(
		controlfile.Control.Package,
		controlfile.Control.Architecture,
		controlfile.Control.Version,
		controlfile.Control.Maintainer,
		controlfile.Control.Description,
	)
	os.WriteFile(
		controlfile.OutDir+"/DEBIAN/control",
		[]byte(controlfile.Control.Render()),
		0o644)

	for _, file := range []string{"postinst", "preinst", "prerm", "postrm"} {
		os.WriteFile(
			controlfile.OutDir+"/DEBIAN/"+file,
			[]byte("#!/bin/sh\nset -e\n"),
			0o755,
		)
	}
	fmt.Printf("Paket-Struktur für '%s' erstellt.\n", controlfile.InDir+"/"+controlfile.Control.Package)
}
