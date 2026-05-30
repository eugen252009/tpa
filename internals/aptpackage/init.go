package aptpackage

import (
	"fmt"
	"os"
)


func InitPackage(controlfile Control) {
	dirs := []string{controlfile.Package + "/DEBIAN", controlfile.Package + "/usr/bin"}
	for _, dir := range dirs { os.MkdirAll(dir, 0755) }

	controlfile.Init(
		controlfile.Package,
		controlfile.Architecture,
		controlfile.Version,
		controlfile.Maintainer,
		controlfile.Description)
	os.WriteFile(
		controlfile.Package+"/DEBIAN/control", 
		[]byte(controlfile.Render()),
		0644)

	for _, file := range []string{"postinst", "preinst", "prerm", "postrm"} {
		os.WriteFile(
			controlfile.Package+"/DEBIAN/"+file,
			[]byte("#!/bin/sh\nset -e\n"),
			0755,
		)
	}
	fmt.Printf("Paket-Struktur für '%s' erstellt.\n", controlfile.Package)
}

