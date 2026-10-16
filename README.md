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

## 📊 Token Usage and Hermes `/agy-usage`

Hermes's existing `/usage` now receives measured input/output, cache and reasoning tokens through standard Chat Completions responses. No Hermes core edits are required. Token estimates are not passed off as real measurements; missing/invalid metadata stays unavailable. Costs and context capacity are not inferred from model names.

Streaming clients must opt in to usage:

```sh
curl -N http://127.0.0.1:8888/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"gemini-3.8-flash-high","messages":[{"role":"user","content":"Reply OK"}],"stream":true,"stream_options":{"include_usage":true}}'
```

The finish chunk is followed by one usage-only chunk (`choices: []`) and then `[DONE]`. Non-streaming responses carry the same measured usage without the streaming opt-in. agy 1.3.6 terminal counters are cumulative across its persistent process; the proxy calculates request deltas, includes cache reads in prompt totals, and does not add reasoning twice. See [the verified accounting contract](docs/usage-accounting.md).

Read persistent proxy-wide statistics:

```sh
curl -fsS http://127.0.0.1:8888/v1/usage
```

This is observed usage across proxy clients/sessions, not Google account quota, remaining capacity or billing. Both `/v1/usage` and `/usage` are read-only GET endpoints; there is no reset endpoint. Totals distinguish measured and unknown-usage completions. Linux state defaults to `$XDG_STATE_HOME/gemini-proxy/usage.json` or `~/.local/state/gemini-proxy/usage.json`; Windows uses `%LOCALAPPDATA%\gemini-proxy\usage.json`. `GEMINI_PROXY_USAGE_FILE` overrides the proxy state file. See the accounting contract for macOS paths, locking, permissions, persistence warnings and recovery limitations. Keep the proxy bound to loopback unless protected by appropriate access controls.

### Separate Hermes command (optional)

From this repository, install the bundled command-only plugin into the active Hermes profile:

```sh
printf '%s\n' 'import runpy; _ = runpy.run_path("integrations/hermes/install_agy_usage.py", run_name="__main__")' | hermes --run-module code -q
hermes plugins doctor agy-usage --ci
hermes config set plugins.entries.agy-usage.settings.base_url http://127.0.0.1:8888/v1
hermes plugins enable agy-usage --no-allow-tool-override
```

The installer refuses to overwrite an existing plugin. Start a new Hermes session and use `/agy-usage` or `/agy-usage --json`. This command makes only a read-only HTTP request: no model calls, tools, hooks or changes to session counters. It uses the explicitly configured URL, NOT the current model's provider route; switching models does not change which proxy it queries. It does not borrow provider credentials or follow redirects. `/usage` remains session-scoped; `/agy-usage` is proxy-wide. Neither resets the other's counters.

Disable with `hermes plugins disable agy-usage`. To uninstall later, use `hermes plugins remove agy-usage` and review its confirmation; removal does not erase proxy statistics. [Compatibility, installation and troubleshooting](integrations/hermes/agy-usage/COMPATIBILITY.md).

### Verification and troubleshooting

```sh
go test ./...
go test -race ./...
go vet ./...
hermes --run-module unittest discover -s integrations/hermes/agy-usage/tests -v
hermes plugins doctor integrations/hermes/agy-usage --ci
```

- Test `/usage` after a real prompt in the SAME Hermes session; a separate fresh session does not verify session accounting.
- A 404 from `/v1/usage` usually means the old proxy binary is still running or the URL is wrong.
- An unavailable/unknown measurement is not zero and is not replaced by an estimate.
- Check `persistence.healthy` and logs for disk, corrupt-state or competing-owner warnings; chat can work while durable statistics are unavailable. Never delete the ownership lock while a daemon is running.
- `/agy-usage` requires its explicit URL setting. A 401/403 from an authenticated reverse proxy is reported without reusing unrelated model credentials.
- The prototype's old unversioned `~/.config/gemini-proxy/usage.json` is not migrated or overwritten; tracking begins in the versioned state file.

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
