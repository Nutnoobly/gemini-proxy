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

[Service]
Type=simple
ExecStart=%s serve --port %d
Restart=always
RestartSec=3
Environment="PATH=%s"

[Install]
WantedBy=default.target
`

// InstallService writes the systemd user unit file and enables/starts it.
func InstallService(binaryPath string, port int) error {
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

	unitPath := filepath.Join(unitDir, "gemini-proxy.service")
	pathEnv := os.Getenv("PATH")
	content := fmt.Sprintf(serviceUnitTemplate, absBinary, port, pathEnv)

	if err := os.WriteFile(unitPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write systemd unit file: %w", err)
	}
	log.Printf("[Systemd] Written unit file to: %s", unitPath)

	if out, err := exec.Command("systemctl", "--user", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("daemon-reload failed: %s (%w)", string(out), err)
	}

	if out, err := exec.Command("systemctl", "--user", "enable", "--now", "gemini-proxy").CombinedOutput(); err != nil {
		return fmt.Errorf("failed to enable and start service: %s (%w)", string(out), err)
	}

	log.Printf("[Systemd] Service gemini-proxy enabled and started successfully")
	return nil
}

// StatusService checks the systemd service status.
func StatusService() error {
	cmd := exec.Command("systemctl", "--user", "status", "gemini-proxy")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// StopService stops the systemd service.
func StopService() error {
	out, err := exec.Command("systemctl", "--user", "stop", "gemini-proxy").CombinedOutput();
	if err != nil {
		return fmt.Errorf("failed to stop service: %s (%w)", string(out), err)
	}
	log.Println("[Systemd] Service stopped")
	return nil
}

// RestartService restarts the systemd service.
func RestartService() error {
	out, err := exec.Command("systemctl", "--user", "restart", "gemini-proxy").CombinedOutput();
	if err != nil {
		return fmt.Errorf("failed to restart service: %s (%w)", string(out), err)
	}
	log.Println("[Systemd] Service restarted")
	return nil
}
