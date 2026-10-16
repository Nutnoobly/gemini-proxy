//go:build windows

package api

import (
	"os"
	"syscall"
	"unsafe"
)

var usageKernel32 = syscall.NewLazyDLL("kernel32.dll")
var usageLockFileEx = usageKernel32.NewProc("LockFileEx")
var usageUnlockFileEx = usageKernel32.NewProc("UnlockFileEx")
var usageMoveFileEx = usageKernel32.NewProc("MoveFileExW")

func lockUsageFile(f *os.File) error {
	var overlapped syscall.Overlapped
	// Exclusive, non-blocking byte-range lock. The handle stays open for the
	// lifetime of the tracker; the OS releases the lock if the process exits.
	ok, _, err := usageLockFileEx.Call(f.Fd(), 3, 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)))
	if ok == 0 {
		return err
	}
	return nil
}
func unlockUsageFile(f *os.File) error {
	var overlapped syscall.Overlapped
	ok, _, err := usageUnlockFileEx.Call(f.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)))
	closeErr := f.Close()
	if ok == 0 {
		return err
	}
	return closeErr
}
func replaceUsageFile(from, to string) error {
	source, err := syscall.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	target, err := syscall.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	// Same-directory replacement with MOVEFILE_REPLACE_EXISTING|WRITE_THROUGH.
	// No copy/delete fallback is permitted.
	ok, _, err := usageMoveFileEx.Call(uintptr(unsafe.Pointer(source)), uintptr(unsafe.Pointer(target)), 9)
	if ok == 0 {
		return err
	}
	return nil
}

// Windows does not support POSIX directory fsync. The replacement above asks
// the filesystem to complete the move before returning.
func syncUsageDirectory(string) error { return nil }
