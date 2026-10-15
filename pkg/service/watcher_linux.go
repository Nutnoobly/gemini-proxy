//go:build !windows

package service

import (
	"os"
	"path/filepath"
	"strconv"
)

// IsHermesRunning checks /proc to determine if any Hermes Agent process is currently alive.
func IsHermesRunning() bool {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false
	}

	myPID := os.Getpid()

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pidStr := entry.Name()
		pid, err := strconv.Atoi(pidStr)
		if err != nil || pid == myPID {
			continue
		}

		cmdlineBytes, err := os.ReadFile(filepath.Join("/proc", pidStr, "cmdline"))
		if err != nil {
			continue
		}

		if isHermesCmdline(string(cmdlineBytes)) {
			return true
		}
	}

	return false
}
