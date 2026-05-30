package aptpackage

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func Build(c BuildContext) {
	out, err := os.ReadFile(c.InDir + "/DEBIAN/control")
	if err != nil {
		fmt.Println(err)
		return
	}
	contolfile, err := ParseControl(out)
	if err != nil {
		fmt.Println(err)
		return
	}
	c.Control = contolfile
	c.Control.Package = strings.ToLower(c.Control.Package)
	os.MkdirAll(c.OutDir, 0o755)
	scripts := []string{"postinst", "preinst", "prerm", "postrm"}
	for _, s := range scripts {
		fmt.Println(c.InDir + "/DEBIAN/" + s)
		os.Chmod(c.OutDir+"/DEBIAN/"+s, 0o755)
	}
	packagename := fmt.Sprintf("%s_%s_%s.deb", c.Control.Package, c.Control.Version, c.Control.Architecture)
	cmd := exec.Command(
		"dpkg-deb",
		"--root-owner-group",
		"--build",
		c.InDir,
		c.OutDir,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		fmt.Printf("Fehler beim Bauen von %s: %v\n", packagename, err)
	} else {
		fmt.Printf("Paket %s.deb erfolgreich erstellt!\n", packagename)
	}
}
