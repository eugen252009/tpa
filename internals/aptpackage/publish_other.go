//go:build !linux

package aptpackage

import "fmt"

func publishDirectory(_, _ string) error {
	return fmt.Errorf("atomic publish is supported only on Linux")
}
