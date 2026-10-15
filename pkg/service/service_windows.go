//go:build windows

package service

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

const (
	detachedProcess = 0x00000008
	createNoWindow  = 0x08000000
)

// InstallService configures Windows startup and immediately launches a detached background instance.
func InstallService(binaryPath string, port int, idleTimeout string) error {
	absBinary, err := filepath.Abs(binaryPath)
	if err != nil {
		return fmt.Errorf("failed to get absolute binary path: %w", err)
	}

	if idleTimeout == "" {
		idleTimeout = "10m"
	}

	// 1. Write run_proxy.bat in binary directory for Scheduled Task support
	localBatPath := filepath.Join(filepath.Dir(absBinary), "run_proxy.bat")
	localBatContent := fmt.Sprintf(`@"%s" serve --port %d --idle-timeout %s
`, absBinary, port, idleTimeout)
	_ = os.WriteFile(localBatPath, []byte(localBatContent), 0644)
	log.Printf("[Windows Service] Written runner script to: %s", localBatPath)

	// 2. Write Startup folder launcher for automatic launch at user logon
	appData := os.Getenv("APPDATA")
	if appData != "" {
		startupDir := filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs", "Startup")
		if err := os.MkdirAll(startupDir, 0755); err == nil {
			batPath := filepath.Join(startupDir, "gemini-proxy.bat")
			batContent := fmt.Sprintf(`@start "" "%s" serve --port %d --idle-timeout %s
`, absBinary, port, idleTimeout)
			_ = os.WriteFile(batPath, []byte(batContent), 0644)
			log.Printf("[Windows Service] Written startup launcher to: %s", batPath)
		}
	}

	// Stop any existing instance first
	_ = StopService()

	// Launch background instance: try Scheduled Task first if registered, otherwise detached process
	if err := exec.Command("schtasks", "/run", "/tn", "GeminiProxy").Run(); err == nil {
		log.Println("[Windows Service] Started background instance via Scheduled Task 'GeminiProxy'")
		return nil
	}

	if err := startDetached(absBinary, port, idleTimeout); err != nil {
		log.Printf("[Windows Service] Warning: could not launch background instance immediately: %v", err)
	} else {
		log.Printf("[Windows Service] Started background instance")
	}

	return nil
}

func startDetached(absBinary string, port int, idleTimeout string) error {
	cmd := exec.Command(absBinary, "serve", "--port", strconv.Itoa(port), "--idle-timeout", idleTimeout)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: detachedProcess | createNoWindow,
	}
	logPath := filepath.Join(os.TempDir(), "gemini-proxy-service.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}
	return cmd.Start()
}

// StatusService checks whether the Windows startup script exists and if gemini-proxy is running.
func StatusService() error {
	appData := os.Getenv("APPDATA")
	batPath := filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs", "Startup", "gemini-proxy.bat")

	installed := false
	if _, err := os.Stat(batPath); err == nil {
		installed = true
		fmt.Printf("✓ Startup launcher installed at: %s\n", batPath)
	} else {
		fmt.Printf("○ Startup launcher is not installed.\n")
	}

	if _, err := exec.Command("schtasks", "/query", "/tn", "GeminiProxy").CombinedOutput(); err == nil {
		fmt.Printf("✓ Scheduled Task 'GeminiProxy' is registered.\n")
	}

	pids := findProcessPIDs("gemini-proxy.exe")
	if len(pids) > 0 {
		fmt.Printf("● Service is RUNNING (PID(s): %v)\n", pids)
	} else {
		fmt.Println("○ Service is NOT RUNNING")
	}

	if !installed && len(pids) == 0 {
		fmt.Println("Run 'gemini-proxy service install' to install and start the background service.")
	}

	return nil
}

// StopService terminates running gemini-proxy background instances (excluding self).
func StopService() error {
	_ = exec.Command("schtasks", "/end", "/tn", "GeminiProxy").Run()
	pids := findProcessPIDs("gemini-proxy.exe")
	if len(pids) == 0 {
		log.Println("[Windows Service] No running GeminiProxy instances found")
		return nil
	}
	for _, pid := range pids {
		_ = exec.Command("taskkill", "/F", "/PID", fmt.Sprintf("%d", pid)).Run()
	}
	log.Printf("[Windows Service] Stopped %d running instance(s)", len(pids))
	return nil
}

// RestartService restarts GeminiProxy.
func RestartService() error {
	_ = StopService()

	if err := exec.Command("schtasks", "/run", "/tn", "GeminiProxy").Run(); err == nil {
		log.Println("[Windows Service] GeminiProxy service restarted via Scheduled Task")
		return nil
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return startDetached(exe, 8888, "10m")
}

func findProcessPIDs(targetExe string) []uint32 {
	snapshot, err := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer syscall.CloseHandle(snapshot)

	var entry syscall.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))

	if err := syscall.Process32First(snapshot, &entry); err != nil {
		return nil
	}

	myPID := uint32(os.Getpid())
	var pids []uint32

	for {
		if entry.ProcessID != myPID {
			exeName := strings.ToLower(syscall.UTF16ToString(entry.ExeFile[:]))
			if exeName == strings.ToLower(targetExe) {
				pids = append(pids, entry.ProcessID)
			}
		}

		if err := syscall.Process32Next(snapshot, &entry); err != nil {
			break
		}
	}

	return pids
}
