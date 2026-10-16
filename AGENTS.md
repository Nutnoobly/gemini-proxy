# AGENTS.md - Guidelines for GeminiProxy

## Project Overview
GeminiProxy is a lightweight, high-performance Go proxy daemon that allows Hermes Agent to use Google Gemini models (including Gemini 4 Argon, Gemini 3.8 Flash, and Gemini 3.1 Pro) via the local authenticated Google Antigravity CLI (`agy`).

### Key Components
- `main.go`: CLI entrypoint, subcommand routing (`serve`, `setup-hermes`, `service`, `test`), and platform-aware listener initialization.
- `pkg/api`: HTTP server handling OpenAI-compatible endpoints (`/v1/chat/completions`, `/v1/models`), SSE streaming, and Ollama/Hermes capability probes.
- `pkg/worker`: Worker pool managing persistent background `agy` CLI processes (`--input-format stream-json --output-format stream-json`).
- `pkg/prompt`: Formats OpenAI messages and tool declarations; parses `<tool_call>` XML blocks from model outputs.
- `pkg/service`: Cross-platform process watcher and service management:
  - `watcher.go`: Core watcher state and timer coordination.
  - `watcher_linux.go`: Linux process detection via `/proc/*/cmdline` (`!windows`).
  - `watcher_windows.go`: Windows process detection via Win32 Toolhelp32 snapshot API (`windows`).
  - `systemd.go`: Linux systemd user service installation, socket activation, and management (`!windows`).
  - `service_windows.go`: Windows background service management via user Startup scripts and Scheduled Tasks (`windows`).
  - `hermes.go`: Automates configuration of `~/.hermes/config.yaml` (`%USERPROFILE%\.hermes\config.yaml` on Windows).
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
  - Linux:
    ```bash
    go build -o gemini-proxy .
    ```
  - Windows:
    ```powershell
    go build -o gemini-proxy.exe .
    ```

- **Run Tests / Verification**:
  ```bash
  go test ./...
  ./gemini-proxy test         # On Windows: .\gemini-proxy.exe test
  ```

- **Run Server Directly**:
  - Linux:
    ```bash
    ./gemini-proxy serve --port 8888
    ```
  - Windows:
    ```powershell
    .\gemini-proxy.exe serve --port 8888
    ```

- **Service Management**:
  - Linux (Socket-activated systemd user service):
    ```bash
    ./gemini-proxy service install   # Install socket-activated systemd service
    ./gemini-proxy service status    # Check status of socket and service
    ./gemini-proxy service restart   # Restart service
    ./gemini-proxy service stop      # Stop service
    ```
  - Windows (Logon Startup script & Scheduled Task):
    ```powershell
    .\gemini-proxy.exe service install   # Install logon startup service
    .\gemini-proxy.exe service status    # Check service running status & PID
    .\gemini-proxy.exe service restart   # Restart service
    .\gemini-proxy.exe service stop      # Stop service
    ```

- **Hermes Setup**:
  ```bash
  ./gemini-proxy setup-hermes      # On Windows: .\gemini-proxy.exe setup-hermes
  ```

---

## Usage Accounting Invariants

- `pkg/worker/usage.go` converts agy's conversation-cumulative terminal counters into request deltas. Never add overlapping step/result totals or repeatedly charge previous turns.
- `pkg/api/usage.go` is the shared OpenAI mapper: total prompt includes cache reads; output already includes reasoning. Validate changes against actual upstream semantics, not model-name assumptions. Evidence is in `docs/usage-accounting.md`.
- `pkg/api/usage_tracker.go` owns versioned proxy-wide counters, process ownership and atomic persistence. Record each successful upstream completion once, including unknown usage separately. Missing metadata is not measured zero and must not be estimated as actual usage.
- Streaming usage is opt-in (`stream_options.include_usage=true`): finish chunk, usage-only `choices: []` chunk, then `[DONE]`. Preserve plain-text and tool-call behavior.
- Usage endpoints are read-only GET. Do not add shared reset behavior or erase state as part of a session reset. Tests inject temporary state paths; never write real user state.
- `integrations/hermes/agy-usage` owns the standalone command plugin; `integrations/hermes/install_agy_usage.py` installs without overwriting. It queries its explicit configured proxy URL and registers no tools/hooks. Do not edit Hermes core, existing `/usage`, inference routing, prompts or caching for this integration.
- Verify with `go test ./...`, `go test -race ./...`, `go vet ./...`, `hermes --run-module unittest discover -s integrations/hermes/agy-usage/tests -v`, and `hermes plugins doctor integrations/hermes/agy-usage --ci`. Cross-compile Windows changes; do not claim native Windows runtime coverage from Linux builds.
- Live acceptance must verify real non-streaming/streaming counters, restart persistence, and `/usage` plus `/agy-usage` in the same Hermes session. Usage GETs must not create model calls or mutate session counters. Never invent quota, prices or context lengths.

## Agent Conventions
- Write clean, idiomatic Go code formatted with `gofmt`.
- Maintain thread safety across concurrent components (`pkg/worker`, `pkg/api`) using proper synchronization primitives.
- Keep changes minimal and focused on the task requirements.
