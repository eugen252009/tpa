//go:build linux

package aptpackage

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func withRepositoryLock(live string, operation func() error) error {
	abs, err := filepath.Abs(live)
	if err != nil {
		return fmt.Errorf("resolve repository lock path: %w", err)
	}
	if abs == string(filepath.Separator) {
		return fmt.Errorf("refusing to lock filesystem root")
	}
	lockPath := filepath.Join(filepath.Dir(abs), "."+filepath.Base(abs)+".tpa.lock")
	fd, err := unix.Open(lockPath, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return fmt.Errorf("open repository lock: %w", err)
	}
	lock := os.NewFile(uintptr(fd), lockPath)
	defer lock.Close()
	if err := unix.Flock(fd, unix.LOCK_EX); err != nil {
		return fmt.Errorf("lock repository: %w", err)
	}
	defer unix.Flock(fd, unix.LOCK_UN)
	return operation()
}
