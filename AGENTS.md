# AGENTS.md - Guidelines for GeminiProxy

## Project Overview
GeminiProxy is a lightweight, high-performance Go proxy daemon that allows Hermes Agent to use Google Gemini models via the local authenticated Google Antigravity CLI (`agy`).

### Key Components
- `main.go`: CLI entrypoint and subcommand routing (`serve`, `setup-hermes`, `service`, `test`).
- `pkg/api`: HTTP server handling OpenAI-compatible endpoints (`/v1/chat/completions`, `/v1/models`), SSE streaming, and Ollama/Hermes capability probes.
- `pkg/worker`: Worker pool managing persistent background `agy` CLI processes (`--input-format stream-json --output-format stream-json`).
- `pkg/prompt`: Formats OpenAI messages and tool declarations; parses `<tool_call>` XML blocks from model outputs.
- `pkg/service`: Systemd user service installation, management, and on-demand socket activation.
- `pkg/types`: Core types and schemas.

---

## ⚠️ Critical Safety Rules (Mandatory User Confirmation)

To prevent irreversible data loss or unintentional state corruption, agents **MUST ALWAYS ask the user for confirmation** before executing destructive commands:

1. **`rm` (File / Directory Deletion)**:
   - **NEVER** run `rm`, `rm -rf`, `unlink`, or delete files/directories automatically without confirmation.
   - **ALWAYS** ask the user first, specifying the exact files or directories to be removed and the rationale.

2. **`git reset` (Git History & State Modification)**:
   - **NEVER** run `git reset` (`--soft`, `--mixed`, or `--hard`) without explicit prior user approval.
   - **ALWAYS** ask the user before executing commands that discard uncommitted changes, discard staging state, or rewrite branch history (e.g. `git reset`, `git restore`, `git checkout --`, `git clean`).

3. **Any Potentially Dangerous Command**:
   - If the model assesses or suspects that a command is potentially dangerous, destructive, or could lead to irreversible state changes or data loss, it **MUST ALWAYS ask the user for permission** before executing it.

---

## Development & Operational Commands

- **Build Binary**:
  ```bash
  go build -o gemini-proxy .
  ```

- **Run Tests / Verification**:
  ```bash
  go test ./...
  ./gemini-proxy test
  ```

- **Run Server Directly**:
  ```bash
  ./gemini-proxy serve --port 8080
  ```

- **Service Management**:
  ```bash
  ./gemini-proxy service install   # Install socket-activated systemd service
  ./gemini-proxy service status    # Check status of socket and service
  ./gemini-proxy service restart   # Restart service
  ./gemini-proxy service stop      # Stop service
  ```

- **Hermes Setup**:
  ```bash
  ./gemini-proxy setup-hermes      # Backs up and configures ~/.hermes/config.yaml
  ```

---

## Agent Conventions
- Write clean, idiomatic Go code formatted with `gofmt`.
- Maintain thread safety across concurrent components (`pkg/worker`, `pkg/api`) using proper synchronization primitives.
- Keep changes minimal and focused on the task requirements.
