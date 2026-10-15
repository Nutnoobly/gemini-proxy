//go:build windows

package service

import (
	"os"
	"strings"
	"syscall"
	"unsafe"
)

// IsHermesRunning checks running processes on Windows to determine if Hermes Agent is currently alive.
func IsHermesRunning() bool {
	snapshot, err := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(snapshot)

	var entry syscall.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))

	if err := syscall.Process32First(snapshot, &entry); err != nil {
		return false
	}

	myPID := uint32(os.Getpid())

	for {
		if entry.ProcessID != myPID {
			exeName := strings.ToLower(syscall.UTF16ToString(entry.ExeFile[:]))
			if exeName == "hermes.exe" || exeName == "hermes" || strings.HasPrefix(exeName, "hermes-") || strings.HasPrefix(exeName, "hermes_") {
				return true
			}
		}

		if err := syscall.Process32Next(snapshot, &entry); err != nil {
			break
		}
	}

	return false
}
