package aptpackage

import (
	"fmt"
)

type BuildContext struct {
	Control Control
	OutDir  string
	InDir   string
}
type PackPackage struct {
	Context BuildContext
	GPG     string
	Force   bool
	Flat    bool
}

type Control struct {
	Package      string
	Version      string
	Architecture string
	Maintainer   string
	Description  string
	Depends      *string
	Homepage     *string
	Section      *string
	Priority     *string
}

func (c *Control) Init(PackageName, Architecture, Version, Maintainer, Description string) {
	c.Package = PackageName
	c.Architecture = Architecture
	c.Version = Version
	c.Maintainer = Maintainer
	c.Description = Description
}

func (c *Control) Render() string {
	output := fmt.Sprintf("Package: %s\nVersion: %s\nArchitecture: %s\nMaintainer: %s\nDescription: %s\n",
		c.Package, c.Version, c.Architecture, c.Maintainer, c.Description)

	if c.Depends != nil {
		output += fmt.Sprintf("Depends: %s\n", *c.Depends)
	}
	if c.Homepage != nil {
		output += fmt.Sprintf("Homepage: %s\n", *c.Homepage)
	}
	if c.Section != nil {
		output += fmt.Sprintf("Section: %s\n", *c.Section)
	}
	if c.Priority != nil {
		output += fmt.Sprintf("Priority: %s\n", *c.Priority)
	}

	return output
}
