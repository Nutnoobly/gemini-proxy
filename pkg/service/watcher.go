package service

import (
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// HermesWatcher tracks the presence of Hermes processes on the system.
type HermesWatcher struct {
	hermesWasActive atomic.Bool
	stopCh          chan struct{}
	onHermesClose   func()
}

// NewHermesWatcher creates a watcher that triggers onHermesClose when Hermes exits.
func NewHermesWatcher(onHermesClose func()) *HermesWatcher {
	return &HermesWatcher{
		stopCh:        make(chan struct{}),
		onHermesClose: onHermesClose,
	}
}

// Arm manually marks that Hermes has been active (e.g. upon receiving a chat request).
func (w *HermesWatcher) Arm() {
	if w.hermesWasActive.CompareAndSwap(false, true) {
		log.Println("[Hermes Watcher] Armed: Hermes activity registered.")
	}
}

// Start runs the periodic check in a background goroutine.
func (w *HermesWatcher) Start() {
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		absentCounter := 0

		for {
			select {
			case <-w.stopCh:
				return
			case <-ticker.C:
				running := IsHermesRunning()
				if running {
					if w.hermesWasActive.CompareAndSwap(false, true) {
						log.Println("[Hermes Watcher] Active Hermes Agent process detected.")
					}
					absentCounter = 0
				} else if w.hermesWasActive.Load() {
					absentCounter++
					// 3 consecutive checks (6s) of absence triggers shutdown
					if absentCounter >= 3 {
						log.Println("[Hermes Watcher] Hermes Agent has closed. Triggering shutdown...")
						w.onHermesClose()
						return
					}
				}
			}
		}
	}()
}

// Stop stops the watcher goroutine.
func (w *HermesWatcher) Stop() {
	select {
	case <-w.stopCh:
	default:
		close(w.stopCh)
	}
}

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

func isHermesCmdline(raw string) bool {
	parts := strings.Split(raw, "\x00")
	var clean []string
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			clean = append(clean, trimmed)
		}
	}
	if len(clean) == 0 {
		return false
	}

	exe := filepath.Base(clean[0])
	// Filter out self and common utility commands
	switch exe {
	case "gemini-proxy", "grep", "cat", "vim", "nvim", "nano", "git", "go", "ps", "less", "tail":
		return false
	}

	if strings.Contains(raw, "gemini-proxy") || strings.Contains(exe, "agy") {
		return false
	}

	if exe == "hermes" {
		return true
	}

	for _, p := range clean {
		base := filepath.Base(p)
		if base == "hermes" && !strings.HasSuffix(p, ".log") && !strings.HasSuffix(p, ".txt") {
			return true
		}
		if strings.Contains(p, "hermes_cli") || strings.Contains(p, "hermes-agent") {
			return true
		}
	}

	return false
}
