# Hermes compatibility and scope

`agy-usage` is a standalone, command-only plugin. It registers `/agy-usage [--json]` and no model tools or hooks. Hermes core, its `/usage` handler, inference routing, prompt construction and session counters are unchanged.

## Explicit endpoint, not current-session inference routing

The installed Hermes command callback receives only the raw argument string. `ctx.get_config()` exposes plugin settings, not the effective session model route; `ctx.llm` intentionally does not expose provider credentials. Accordingly this plugin reads only its explicitly configured `plugins.entries.agy-usage.settings.base_url`.

Switching `/model` or provider does not retarget this command. Its text output names the configured proxy and labels statistics proxy-wide. It remains useful when the current model is not GeminiProxy. It does not discover or borrow any inference API credentials. An authenticated reverse proxy will return a concise authentication error; use a protected local connection instead of embedding credentials in the URL. Redirects and URL credentials are rejected.

## Installation

From the GeminiProxy repository, start `hermes --run-module code -q` and enter `import runpy; _ = runpy.run_path("integrations/hermes/install_agy_usage.py", run_name="__main__")`, then exit the console. On Bash, the README provides the equivalent piped invocation. The installer resolves the active home with `hermes_constants.get_hermes_home()` and copies the plugin into its `plugins/agy-usage` directory, excluding tests and Python caches. It refuses to overwrite an existing destination. No files belong inside the Hermes source checkout.

Then run:

```sh
hermes plugins doctor agy-usage --ci
hermes config set plugins.entries.agy-usage.settings.base_url http://127.0.0.1:8888/v1
hermes plugins enable agy-usage --no-allow-tool-override
```

Start a new Hermes session for discovery; do not interrupt an existing session. Use `/agy-usage` or `/agy-usage --json`. There is no reset subcommand.

Disable without deleting files:

```sh
hermes plugins disable agy-usage
```

To uninstall later, use `hermes plugins remove agy-usage` after checking its confirmation prompt. Disabling/removing the plugin does not erase the proxy's persistent counters.

## Verification

Dependency-free plugin tests:

```sh
hermes --run-module unittest discover -s integrations/hermes/agy-usage/tests -v
hermes plugins doctor integrations/hermes/agy-usage --ci
```

The runtime inspected during implementation was Hermes checkout `d0288be5b3330d2442e3907185b8e9d0958297bb`. Supported APIs used: `PluginContext.get_config` and `PluginContext.register_command`. No private CLI/agent references are accessed.

HTTP requests use a three-second socket timeout, a 256 KiB maximum response, strict JSON/schema checks and read-only GET. Malformed or truncated HTTP responses produce a safe diagnostic without response payloads. Base URLs must be ASCII; percent-encode Unicode path components (for example, `/私密/v1` becomes `/%E7%A7%81%E5%AF%86/v1`). Raw Unicode URLs are rejected before making a request, without echoing the configured URL. A slowly trickling response can take longer than one socket timeout; use a trusted loopback endpoint, not an arbitrary public server.
