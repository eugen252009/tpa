//go:build !linux

package aptpackage

import (
	"io"
	"os"
)

func copyLifecycleFile(source, destination *os.File) error {
	_, err := io.Copy(destination, source)
	return err
}
