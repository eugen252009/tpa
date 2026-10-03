// Package version contains the build-injected TPA version shared by CLI and package metadata.
package version

const (
	ProductName        = "TPA"
	ProductDescription = "Tool for Package Automation"
	ProjectURL         = "https://github.com/eugen252009/tpa"
	SupportEmail       = "tpa@lupricht.net"
)

// Version defaults to dev for ordinary source builds and is set by the release build script.
var Version = "dev"
