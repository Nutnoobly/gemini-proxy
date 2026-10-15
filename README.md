# 🚀 GeminiProxy

**GeminiProxy** is a lightweight, high-performance Go proxy daemon that allows [Hermes Agent](https://github.com/NousResearch/hermes-agent) to seamlessly use **Google Gemini** models via your authenticated local **Google Antigravity CLI (`agy`)**.

Zero external API keys, zero cloud costs beyond your Google account quota, and full support for streaming and Hermes tool/function execution.

---

## 🌟 Key Features

- **Pure Google Gemini**: Direct integration with Google's flagship reasoning models: `gemini-4-argon` (frontier reasoning & agentic workflows), `gemini-3.8-flash-high` (default), `gemini-3.1-pro-high`, and `gemini-3.7-flash-high`.
- **Hermes Tool Calling**: Translates Hermes agent tools (bash/terminal, file editing, web search, etc.) into structured model prompts and converts model responses back into standard OpenAI `tool_calls`.
- **Persistent Worker Pool**: Keeps `agy` CLI background worker processes alive via `--input-format stream-json --output-format stream-json` for sub-second turn latency without repeated startup overhead.
- **Real-Time SSE Streaming**: Native Server-Sent Events (`text/event-stream`) for token-by-token streaming in the Hermes terminal and TUI.
- **Cross-Platform Background Service**: Runs quietly in the background as a user service — on Linux via `systemd` with on-demand socket activation, and on Windows via user startup scripts and scheduled tasks.
- **Process Lifecycle Watcher**: Automatically monitors the active Hermes Agent process (`/proc` on Linux, Win32 Toolhelp32 snapshot on Windows) and shuts down automatically when Hermes closes.
- **Hermes Auto-Configurator**: Built-in `setup-hermes` command that safely backs up and updates `~/.hermes/config.yaml` (`%USERPROFILE%\.hermes\config.yaml` on Windows).

---

## 🏛️ Architecture

```
┌─────────────────────────────────────────────────────────┐
│ User Terminal                                           │
│ $ hermes (or hermes chat -q "...")                      │
└───────────────────────────┬─────────────────────────────┘
                            │ OpenAI Wire Protocol
                            │ POST /v1/chat/completions (Stream / Non-stream)
                            ▼
┌─────────────────────────────────────────────────────────┐
│ GeminiProxy Daemon (127.0.0.1:8888)                     │
│  - OpenAI & Tool Call Formatter                         │
│  - Model Normalizer & Alias Mapper                      │
│  - Persistent Process Pool Manager                      │
│  - Real-Time SSE Streamer                               │
└───────────────────────────┬─────────────────────────────┘
                            │ NDJSON via stdin / stdout
                            │ (stream-json events: user -> step_update -> result)
                            ▼
┌─────────────────────────────────────────────────────────┐
│ Antigravity CLI (`agy`)                                 │
│  `agy --model gemini-3.8-flash-high --input-format ...` │
└───────────────────────────┬─────────────────────────────┘
                            │ Authenticated Google Cloud RPC
                            ▼
┌─────────────────────────────────────────────────────────┐
│ Google Gemini Intelligence (Gemini 4 Argon / 3.8 Flash) │
└─────────────────────────────────────────────────────────┘
```

---

## 🛠️ Installation & Setup

### 1. Build Binary

- **Linux / macOS**:
  ```bash
  go build -o gemini-proxy .
  ```
- **Windows (PowerShell)**:
  ```powershell
  go build -o gemini-proxy.exe .
  ```

### 2. Install & Start Background Service

#### Linux (Systemd with Socket Activation)
```bash
./gemini-proxy service install
```
- `gemini-proxy.socket` listens on port 8888 taking 0 MB RAM and 0% CPU while idle.
- When Hermes Agent sends a query, systemd activates `gemini-proxy` instantly.
- After 10 minutes of inactivity (configurable via `--idle-timeout`), `gemini-proxy` gracefully shuts down and frees all memory.
- Check status:
  ```bash
  ./gemini-proxy service status
  # or
  systemctl --user status gemini-proxy.socket gemini-proxy.service
  ```

#### Windows (Logon Startup & Scheduled Task)
```powershell
.\gemini-proxy.exe service install --port 8888 --idle-timeout 10m
```
- Installs a user startup script (`%APPDATA%\Microsoft\Windows\Start Menu\Programs\Startup\gemini-proxy.bat`) and runner script (`run_proxy.bat`).
- Automatically starts a detached background instance with zero admin rights required.
- Check status:
  ```powershell
  .\gemini-proxy.exe service status
  ```

### 3. Configure Hermes Agent
Point your Hermes Agent to `gemini-proxy`:
```bash
./gemini-proxy setup-hermes
# On Windows:
.\gemini-proxy.exe setup-hermes
```
*(This automatically creates a timestamped backup of `~/.hermes/config.yaml` or `%USERPROFILE%\.hermes\config.yaml` before updating).*

---

## 🧪 Verification

### Quick Synthetic Test
Test if the proxy can talk to `agy` and generate a response:
```bash
./gemini-proxy test
# On Windows:
.\gemini-proxy.exe test
```

### Test Hermes End-to-End
Run a direct one-shot query with Hermes:
```bash
hermes -z "What is 2+2? Answer in one word."
```

Test Hermes tool execution (Hermes will call its local bash tool via Gemini):
```bash
hermes -z "Use your terminal tool to run 'echo HELLO_GEMINI' and show the output"
```

---

## 📋 CLI Reference

```bash
gemini-proxy <command> [options]

Commands:
  serve         Start the proxy HTTP server in foreground (default)
                Flags:
                  --host <ip>          Bind IP (default: 127.0.0.1)
                  --port <port>        Listen port (default: 8888)
                  --idle-timeout <dur> Auto-shutdown after inactivity (default: 10m, 0 to disable)
                  --watch-hermes       Auto-shutdown when Hermes Agent closes (default: true)

  setup-hermes  Configure ~/.hermes/config.yaml for GeminiProxy
                Flags:
                  --base-url <url>     Proxy base URL (default: http://127.0.0.1:8888/v1)
                  --model <name>       Model name (default: gemini-3.8-flash-high)

  service       Manage background service (systemd on Linux, Startup/Tasks on Windows)
                Actions:
                  install              Install background service
                                       Flags: --port 8888, --idle-timeout 10m
                  status               Show service and process status
                  stop                 Stop the background daemon
                  restart              Restart the daemon

  test          Send a synthetic chat completion test
                Flags:
                  --url <url>          Proxy base URL (default: http://127.0.0.1:8888/v1)
                  --model <name>       Model name (default: gemini-3.8-flash-high)
```

---

## 🧠 Supported Gemini Models

You can specify any of the following model aliases in Hermes (`hermes -m <model>` or in `config.yaml`):

| Model Name / Alias | Canonical Model Slug | Description |
| :--- | :--- | :--- |
| `gemini-4-argon`, `gemini-4-argon-high`, `argon`, `argon-high` | `gemini-4-argon-high` | **Frontier.** Gemini 4 Argon flagship high-effort reasoning & agentic coding. |
| `gemini-4-argon-medium`, `argon-medium` | `gemini-4-argon-medium` | Gemini 4 Argon balanced reasoning model. |
| `gemini-4-argon-low`, `argon-low` | `gemini-4-argon-low` | Gemini 4 Argon low reasoning latency model. |
| `gemini-3.8-flash-high`, `gemini 3.8 flash`, `flash` | `gemini-3.8-flash-high` | **Default.** Ultra-fast reasoning & coding. |
| `gemini-3.8-flash-medium` | `gemini-3.8-flash-medium` | Balanced reasoning flash model. |
| `gemini-3.8-flash-low` | `gemini-3.8-flash-low` | Lowest reasoning latency flash model. |
| `gemini-3.1-pro-high`, `gemini-pro`, `pro` | `gemini-3.1-pro-high` | Heavy reasoning & complex architectural design. |
| `gemini-3.7-flash-high` | `gemini-3.7-flash-high` | Gemini 3.7 high-effort reasoning. |

---

## 📜 Logs & Troubleshooting

- **View Proxy Live Logs**:
  - **Linux**:
    ```bash
    journalctl --user -u gemini-proxy -f
    ```
  - **Windows**:
    Background service logs are written to:
    ```powershell
    Get-Content "$env:TEMP\gemini-proxy-service.log" -Wait
    ```
- **View Hermes Logs**:
  ```bash
  hermes logs -f
  ```
