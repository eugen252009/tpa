package aptpackage

import (
	"fmt"
	"os"
	"os/exec"
)


func Build (c Control) {
	scripts := []string{"postinst", "preinst", "prerm", "postrm"}
	for _, s := range scripts {
		os.Chmod(c.Package+"/DEBIAN/"+s, 0755)
	}

	cmd := exec.Command("dpkg-deb", "--build", c.Package)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	
	if err := cmd.Run(); err != nil {
		fmt.Printf("Fehler beim Bauen von %s: %v\n", c.Package, err)
	} else {
		fmt.Printf("Paket %s.deb erfolgreich erstellt!\n", c.Package)
	}
}
