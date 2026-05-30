package aptpackage

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

func ParsePackage(path string) (Control, error) {
	cmd := exec.Command("dpkg-deb", "-f", path)
	output, err := cmd.Output()
	if err != nil {
		return Control{}, fmt.Errorf("konnte Paket nicht lesen: %w", err)
	}
	return ParseControl(output)
}

func ParseControl(output []byte) (Control, error) {
	var c Control
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, ": ", 2)
		if len(parts) != 2 {
			continue
		}

		key := parts[0]
		value := parts[1]

		switch key {
		case "Package":
			c.Package = value
		case "Version":
			c.Version = value
		case "Architecture":
			c.Architecture = value
		case "Maintainer":
			c.Maintainer = value
		case "Description":
			c.Description = value
		case "Depends":
			c.Depends = &value
		case "Homepage":
			c.Homepage = &value
		case "Section":
			c.Section = &value
		case "Priority":
			c.Priority = &value
		}
	}

	if c.Package == "" {
		return c, fmt.Errorf("ungültiges Paket: kein Package-Name gefunden")
	}

	return c, nil
}
