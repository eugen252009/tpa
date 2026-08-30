//go:build linux

package aptpackage

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func publishDirectory(staging, live string) error {
	if info, err := os.Stat(live); os.IsNotExist(err) {
		if err := os.Rename(staging, live); err != nil {
			return err
		}
		return syncDirectory(filepathDir(live))
	} else if err != nil {
		return err
	} else if !info.IsDir() {
		return fmt.Errorf("live path is not a directory")
	}
	// renameat2(RENAME_EXCHANGE) swaps two directory names without exposing a
	// missing or partially populated live path. Both paths are siblings.
	if err := unix.Renameat2(unix.AT_FDCWD, staging, unix.AT_FDCWD, live, unix.RENAME_EXCHANGE); err != nil {
		return fmt.Errorf("rename exchange: %w", err)
	}
	return syncDirectory(filepathDir(live))
}

func filepathDir(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[:i]
		}
	}
	return "."
}

func syncDirectory(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}
