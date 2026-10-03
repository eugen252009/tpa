//go:build linux

package aptpackage

import (
	"io"
	"os"

	"golang.org/x/sys/unix"
)

func copyLifecycleFile(source, destination *os.File) error {
	if err := unix.IoctlFileClone(int(destination.Fd()), int(source.Fd())); err == nil {
		return nil
	}
	if err := destination.Truncate(0); err != nil {
		return err
	}
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err := destination.Seek(0, io.SeekStart); err != nil {
		return err
	}
	_, err := io.Copy(destination, source)
	return err
}
