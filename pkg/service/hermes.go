package service

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ConfigureHermes updates ~/.hermes/config.yaml to route through GeminiProxy.
func ConfigureHermes(baseURL, model string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("could not determine home directory: %w", err)
	}

	configPath := filepath.Join(home, ".hermes", "config.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return fmt.Errorf("Hermes config file not found at %s. Please run `hermes setup` first", configPath)
	}

	// Create backup
	backupPath := fmt.Sprintf("%s.bak.%d", configPath, time.Now().Unix())
	if err := copyFile(configPath, backupPath); err != nil {
		return fmt.Errorf("failed to backup Hermes config: %w", err)
	}
	log.Printf("[Hermes] Created backup of config at: %s", backupPath)

	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	updated, err := updateModelSection(data, baseURL, model)
	if err != nil {
		return fmt.Errorf("failed to update config structure: %w", err)
	}

	if err := os.WriteFile(configPath, updated, 0644); err != nil {
		return fmt.Errorf("failed to write updated config: %w", err)
	}

	log.Printf("[Hermes] Successfully configured %s with provider=custom, base_url=%s, model=%s", configPath, baseURL, model)
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func updateModelSection(content []byte, baseURL, model string) ([]byte, error) {
	scanner := bufio.NewScanner(bytes.NewReader(content))
	var lines []string
	inModelSection := false
	modelSectionFound := false

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(line, "model:") {
			inModelSection = true
			modelSectionFound = true
			lines = append(lines, line)
			lines = append(lines, fmt.Sprintf("  default: %s", model))
			lines = append(lines, "  provider: custom")
			lines = append(lines, fmt.Sprintf("  base_url: %s", baseURL))
			continue
		}

		if inModelSection {
			// Check if we are still inside indented model block
			if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
				if strings.HasPrefix(trimmed, "default:") ||
					strings.HasPrefix(trimmed, "provider:") ||
					strings.HasPrefix(trimmed, "base_url:") {
					// Skip existing default, provider, base_url entries
					continue
				}
				lines = append(lines, line)
				continue
			} else {
				inModelSection = false
			}
		}

		lines = append(lines, line)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if !modelSectionFound {
		// Prepend model section if not found
		var newLines []string
		newLines = append(newLines, "model:")
		newLines = append(newLines, fmt.Sprintf("  default: %s", model))
		newLines = append(newLines, "  provider: custom")
		newLines = append(newLines, fmt.Sprintf("  base_url: %s", baseURL))
		newLines = append(newLines, "")
		lines = append(newLines, lines...)
	}

	return []byte(strings.Join(lines, "\n") + "\n"), nil
}
