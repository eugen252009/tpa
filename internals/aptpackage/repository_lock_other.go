//go:build !linux

package aptpackage

func withRepositoryLock(_ string, operation func() error) error {
	return operation()
}
