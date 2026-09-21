# 🚀 GeminiProxy

**GeminiProxy** is a lightweight, high-performance Go proxy daemon that allows [Hermes Agent](https://github.com/NousResearch/hermes-agent) to seamlessly use **Google Gemini** models via your authenticated local **Google Antigravity CLI (`agy`)**.

Zero external API keys, zero cloud costs beyond your Google account quota, and full support for streaming and Hermes tool/function execution.

---

## 🌟 Key Features

- **Pure Google Gemini**: Direct integration with Google's flagship reasoning models: `gemini-3.8-flash-high` (default), `gemini-3.1-pro-high`, and `gemini-3.7-flash-high`.
- **Hermes Tool Calling**: Translates Hermes agent tools (bash/terminal, file editing, web search, etc.) into structured model prompts and converts model responses back into standard OpenAI `tool_calls`.
- **Persistent Worker Pool**: Keeps `agy` CLI background worker processes alive via `--input-format stream-json --output-format stream-json` for sub-second turn latency without repeated startup overhead.
- **Real-Time SSE Streaming**: Native Server-Sent Events (`text/event-stream`) for token-by-token streaming in the Hermes terminal and TUI.
- **Systemd User Service**: Runs quietly in the background as a user service (`gemini-proxy.service`), auto-starting on login so Hermes is always ready to go.
- **Hermes Auto-Configurator**: Built-in `setup-hermes` command that safely backs up and updates `~/.hermes/config.yaml`.

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
│ Google Gemini Intelligence (Gemini 3.8 Flash / 3.1 Pro) │
└─────────────────────────────────────────────────────────┘
```

---

## 🛠️ Installation & Setup

### 1. Build Binary
The binary is already compiled in the repository:
```bash
go build -o gemini-proxy .
```

### 2. Install & Start Systemd Service (On-Demand Socket Activation)
To install `gemini-proxy` with on-demand socket activation and automatic idle scale-to-zero:
```bash
./gemini-proxy service install
```
With socket activation:
- `gemini-proxy.socket` listens on port 8888 taking 0 MB RAM and 0% CPU while idle.
- When Hermes Agent sends a query, systemd activates `gemini-proxy` instantly.
- After 10 minutes of inactivity (configurable via `--idle-timeout`), `gemini-proxy` gracefully shuts down and frees all memory.
- When Hermes sends another query, it wakes up automatically!

Verify status:
```bash
./gemini-proxy service status
# or
systemctl --user status gemini-proxy.socket gemini-proxy.service
```

### 3. Configure Hermes Agent
Point your Hermes Agent to `gemini-proxy`:
```bash
./gemini-proxy setup-hermes
```
*(This automatically creates a timestamped backup of `~/.hermes/config.yaml` before updating).*

---

## 🧪 Verification

### Quick Synthetic Test
Test if the proxy can talk to `agy` and generate a response:
```bash
./gemini-proxy test
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

  service       Manage systemd on-demand background service
                Actions:
                  install              Install socket & service unit files, enable, and activate
                                       Flags: --port 8888, --idle-timeout 10m
                  status               Show socket and service status
                  stop                 Stop the background daemon and socket
                  restart              Restart the daemon and socket

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
| `gemini-3.8-flash-high`, `gemini 3.8 flash`, `flash` | `gemini-3.8-flash-high` | **Default.** Ultra-fast reasoning & coding. |
| `gemini-3.8-flash-medium` | `gemini-3.8-flash-medium` | Balanced reasoning flash model. |
| `gemini-3.8-flash-low` | `gemini-3.8-flash-low` | Lowest reasoning latency flash model. |
| `gemini-3.1-pro-high`, `gemini-pro`, `pro` | `gemini-3.1-pro-high` | Heavy reasoning & complex architectural design. |
| `gemini-3.7-flash-high` | `gemini-3.7-flash-high` | Gemini 3.7 high-effort reasoning. |

---

## 📜 Logs & Troubleshooting

- View proxy live logs:
  ```bash
  journalctl --user -u gemini-proxy -f
  ```
- View Hermes logs:
  ```bash
  hermes logs -f
  ```
