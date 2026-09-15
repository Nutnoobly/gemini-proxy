package service

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

const serviceUnitTemplate = `[Unit]
Description=GeminiProxy - Antigravity CLI Proxy for Hermes Agent
After=network.target
Wants=gemini-proxy.socket

[Service]
Type=simple
ExecStart=%s serve --port %d --idle-timeout %s
Restart=on-failure
RestartSec=3
Environment="PATH=%s"

[Install]
WantedBy=default.target
`

const socketUnitTemplate = `[Unit]
Description=GeminiProxy Socket for Hermes Agent

[Socket]
ListenStream=127.0.0.1:%d
NoDelay=true

[Install]
WantedBy=sockets.target
`

// InstallService writes the systemd user unit and socket files and enables them.
func InstallService(binaryPath string, port int, idleTimeout string) error {
	absBinary, err := filepath.Abs(binaryPath)
	if err != nil {
		return fmt.Errorf("failed to get absolute binary path: %w", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("could not get user home: %w", err)
	}

	unitDir := filepath.Join(home, ".config", "systemd", "user")
	if err := os.MkdirAll(unitDir, 0755); err != nil {
		return fmt.Errorf("failed to create systemd user dir: %w", err)
	}

	if idleTimeout == "" {
		idleTimeout = "10m"
	}

	serviceUnitPath := filepath.Join(unitDir, "gemini-proxy.service")
	socketUnitPath := filepath.Join(unitDir, "gemini-proxy.socket")
	pathEnv := os.Getenv("PATH")
	serviceContent := fmt.Sprintf(serviceUnitTemplate, absBinary, port, idleTimeout, pathEnv)
	socketContent := fmt.Sprintf(socketUnitTemplate, port)

	if err := os.WriteFile(serviceUnitPath, []byte(serviceContent), 0644); err != nil {
		return fmt.Errorf("failed to write systemd service unit file: %w", err)
	}
	log.Printf("[Systemd] Written service unit file to: %s", serviceUnitPath)

	if err := os.WriteFile(socketUnitPath, []byte(socketContent), 0644); err != nil {
		return fmt.Errorf("failed to write systemd socket unit file: %w", err)
	}
	log.Printf("[Systemd] Written socket unit file to: %s", socketUnitPath)

	if out, err := exec.Command("systemctl", "--user", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("daemon-reload failed: %s (%w)", string(out), err)
	}

	// Stop any previously running service instance so socket can bind to the port
	_ = exec.Command("systemctl", "--user", "stop", "gemini-proxy.service", "gemini-proxy.socket").Run()

	// Enable and start socket activation so proxy runs on demand and scales to zero when idle
	if out, err := exec.Command("systemctl", "--user", "enable", "--now", "gemini-proxy.socket").CombinedOutput(); err != nil {
		return fmt.Errorf("failed to enable and start socket: %s (%w)", string(out), err)
	}
	if out, err := exec.Command("systemctl", "--user", "enable", "gemini-proxy.service").CombinedOutput(); err != nil {
		return fmt.Errorf("failed to enable service: %s (%w)", string(out), err)
	}

	binDir := filepath.Join(home, ".local", "bin")
	if _, err := os.Stat(binDir); err == nil {
		symlinkTarget := filepath.Join(binDir, "gemini-proxy")
		_ = os.Remove(symlinkTarget)
		if err := os.Symlink(absBinary, symlinkTarget); err == nil {
			log.Printf("[Systemd] Symlinked binary to: %s", symlinkTarget)
		}
	}

	log.Printf("[Systemd] Service & Socket enabled successfully (idle scale-to-zero: %s)", idleTimeout)
	return nil
}

// StatusService checks the systemd service and socket status.
func StatusService() error {
	cmd := exec.Command("systemctl", "--user", "status", "gemini-proxy.socket", "gemini-proxy.service")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// StopService stops the systemd service and socket.
func StopService() error {
	out, err := exec.Command("systemctl", "--user", "stop", "gemini-proxy.service", "gemini-proxy.socket").CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to stop service: %s (%w)", string(out), err)
	}
	log.Println("[Systemd] Service and socket stopped")
	return nil
}

// RestartService restarts the systemd socket and service.
func RestartService() error {
	out, err := exec.Command("systemctl", "--user", "restart", "gemini-proxy.socket", "gemini-proxy.service").CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to restart service: %s (%w)", string(out), err)
	}
	log.Println("[Systemd] Service and socket restarted")
	return nil
}
