//go:build !windows

package api

import (
	"errors"
	"os"
	"syscall"
)

// The stable sibling inode is never renamed or removed. Kernel locks are
// released on process exit, so stale lock files do not require unsafe cleanup.
func lockUsageFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}
func unlockUsageFile(f *os.File) error       { return f.Close() }
func replaceUsageFile(from, to string) error { return os.Rename(from, to) }
func syncUsageDirectory(path string) error {
	d, err := os.Open(path)
	if err != nil {
		return err
	}
	defer d.Close()
	err = d.Sync()
	// Some filesystems do not implement directory fsync.
	if errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTSUP) {
		return nil
	}
	return err
}
