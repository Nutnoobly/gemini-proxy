# Usage accounting contract

## Verified upstream semantics

Implementation was checked against installed Antigravity CLI (`agy`) 1.3.6 on October 16, 2026, its official headless-mode examples, and two consecutive live requests in one process.

Primary reference: https://antigravity.google/docs/cli/headless/

The official streaming-input example reports the second step as input 278, output 4, cached 30214, total 282; its terminal result reports cumulative input 30662 and output 8 after two turns. Thus step usage is not an additional charge to add to terminal usage; the terminal usage accumulates over the persistent conversation. Cached reads are separate from input/total.

Sanitized live high-reasoning observations (only usage fields, no private prompts or conversation IDs):

| Counter | First terminal result | Second terminal result | Second turn step/delta |
| --- | ---: | ---: | ---: |
| input_tokens | 16806 | 18973 | 2167 |
| output_tokens | 41 | 67 | 26 |
| thinking_tokens | 40 | 65 | 25 |
| cache_read_tokens | 0 | 14756 | 14756 |
| total_tokens | 16847 | 19040 | 2193 |

The one-token visible reply plus 25 thinking tokens is represented by 26 output tokens, not 51: thinking is already included in output. Subtracting consecutive terminal counters exactly reproduces the second step usage. A separate low-reasoning probe also showed second-turn cache reads larger than input tokens.

## Mapping

The worker subtracts its previous terminal cumulative counters once per completed upstream request. It does not add step_update counters. A new worker starts at zero; worker reuse does not reuse earlier token charges.

For the resulting request delta:

- OpenAI prompt_tokens = agy input_tokens + cache_read_tokens.
- OpenAI completion_tokens = agy output_tokens (already includes thinking).
- OpenAI total_tokens = prompt_tokens + completion_tokens.
- prompt_tokens_details.cached_tokens = cache_read_tokens (input subset).
- completion_tokens_details.reasoning_tokens = thinking_tokens (output subset).

Absent, null, incomplete, negative, contradictory, overflowing or regressing metadata is unavailable, not an estimate and not a fabricated zero. Complete explicitly reported zeros remain measured. After an unknown cumulative interval, one later result is used only to re-establish a baseline; subsequent known intervals can again be measured. Invalid usage must not discard an otherwise valid text response.

Tests cover these distinctions and retain only sanitized counter values. If a future agy version changes accounting shape, verify its contract before changing normalization.

## Client and tracker contracts

Non-streaming responses include usage only when measured. Streaming clients opt in with stream_options.include_usage=true; the finish chunk precedes a usage-only chunk with choices: [], followed by [DONE]. There is no usage-only chunk without opt-in, and no fake measurement when metadata is unavailable.

The tracker records one observed successful upstream completion regardless of stream usage opt-in. Failed/in-flight/canceled requests are not successful completions. A completed result may be recorded even if later client delivery fails. These totals include internal upstream work represented by agy's aggregate, not merely visible response text.

GET /v1/usage and GET /usage return schema_version 1 with global and per-model counters:

- completed_requests, measured_requests, unknown_usage_requests.
- prompt_tokens, completion_tokens, total_tokens.
- cached_tokens, reasoning_tokens (subsets, not extra totals).
- tracking_started_at, last_completed_at, models, persistence.

There is no reset endpoint. Counters start when tracking starts, are shared by all clients of that state file, and cannot reconstruct older unobserved requests. They are not Google account quota, remaining subscription capacity, a billing statement, or a guarantee of context-window size/pricing accuracy.

## State and failures

Linux: $XDG_STATE_HOME/gemini-proxy/usage.json, otherwise ~/.local/state/gemini-proxy/usage.json. macOS: ~/Library/Application Support/gemini-proxy/usage.json. Windows: %LOCALAPPDATA%\gemini-proxy\usage.json (home AppData\Local fallback). GEMINI_PROXY_USAGE_FILE overrides the file location for the proxy process.

The unfinished prototype's ~/.config/gemini-proxy/usage.json is not automatically migrated; its unversioned semantics are not trustworthy. It is left untouched. Start new verified counters instead of relabeling historical data.

State writes use a private same-directory temporary file, flush, atomic replacement and directory sync where supported. Stable sibling .lock files use process-lifetime kernel locks; they are not stale-file ownership markers and must not be deleted while serving. Windows uses LockFileEx and MoveFileExW; Windows filesystem ACLs determine access control, not POSIX mode bits. Windows code is cross-compiled during development; native Windows durability/permission behavior is not certified by a Linux run.

A corrupt/unsupported state or competing owner disables writes for that tracker and surfaces persistence.healthy=false plus a diagnostic, without overwriting the existing state. In-memory tracking/chat continue, so degraded totals are not durable history. Transient write failures preserve in-memory counters and are retried on later completions and Close. A successful final retry can recover persistence health. The API reports current health, not a promise of lossless accounting after process/system crashes.

Usage endpoints inherit the proxy's listener exposure. Keep the default loopback binding. The proxy does not add authentication; protect remotely exposed endpoints through your existing access controls rather than opening them publicly.
