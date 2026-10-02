package aptpackage

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"
)

func ParsePackage(path string) (Control, error) {
	output, err := readPackageControl(path)
	if err != nil {
		return Control{}, err
	}
	return ParseControl(output)
}

func ParseControl(output []byte) (Control, error) {
	c := Control{Metadata: make(map[string]any)}
	var lastField string
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 64*1024), maxControlFileSize)

	for scanner.Scan() {
		line := scanner.Text()

		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') && lastField != "" {
			switch lastField {
			case "Description":
				c.Description += "\n" + strings.TrimLeft(line, " ")
			case "Depends":
				c.Depends += " " + strings.TrimLeft(line, " ")
			default:
				if previous, ok := c.Metadata[lastField].(string); ok {
					c.Metadata[lastField] = previous + "\n" + strings.TrimLeft(line, " \t")
				}
			}
			continue
		}

		parts := strings.SplitN(line, ": ", 2)
		if len(parts) != 2 {
			continue
		}

		key := parts[0]
		value := parts[1]
		lastField = key

		switch key {
		case "Package":
			c.Name = value
		case "Version":
			c.Version = value
		case "Architecture":
			c.Architecture = value
		case "Maintainer":
			c.Maintainer = value
		case "Description":
			c.Description = value
		case "Depends":
			c.Depends = value
		case "Homepage":
			c.Homepage = value
		case "Section":
			c.Section = value
		case "Priority":
			c.Priority = value
		case "Pre-Depends":
			c.PreDepends = value
		case "Recommends":
			c.Recommends = value
		case "Suggests":
			c.Suggests = value
		case "Breaks":
			c.Breaks = value
		case "Conflicts":
			c.Conflicts = value
		case "Replaces":
			c.Replaces = value
		case "Provides":
			c.Provides = value
		case "Built-Using":
			c.BuiltUsing = value
		case "Essential":
			c.Essential = value
		case "Multi-Arch":
			c.MultiArch = value
		default:
			c.Metadata[key] = value
		}
	}
	if err := scanner.Err(); err != nil {
		return c, fmt.Errorf("scan control metadata: %w", err)
	}

	if c.Name == "" {
		return c, fmt.Errorf("invalid package: no package name found")
	}
	if c.Version == "" {
		return c, fmt.Errorf("invalid package: no version found")
	}
	if c.Architecture == "" {
		return c, fmt.Errorf("invalid package: no architecture found")
	}
	if c.Maintainer == "" {
		return c, fmt.Errorf("invalid package: no maintainer found")
	}
	if c.Description == "" {
		return c, fmt.Errorf("invalid package: no description found")
	}

	return c, nil
}
