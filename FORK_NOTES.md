# Fork Notes (Sixzero/CLIProxyAPI)

Local patches on top of `upstream/main` (router-for-me/CLIProxyAPI), plus
runtime/deployment state worth remembering.

## Retired patches (superseded by upstream — 2026-08-13 rebase onto d757063c)

Goal is to keep this list shrinking: prefer upstream absorbing our behavior
over carrying patches.

- **OAuth system-prompt passthrough** (`sanitizeForwardedSystemPrompt`
  removal) — upstream `ef89c6a6` now preserves the caller's system prompt
  verbatim: real mid-conversation `system` turn on current models, legacy
  `<system-reminder>` (verbatim text, no hedge) only for old model IDs.
- **`<instructions>` framing of user sys_msg** — obsolete with the above;
  the hedged "may or may not be relevant" wording now only wraps the
  generated currentDate reminder, not caller instructions.
- **Codex UA bump to 0.144.0** — upstream ships 0.144.0.
- **claude-opus-5 in embedded models.json** — upstream catalog has it
  (our copy caused a duplicate-id validation failure).

## Active patches (committed on `main`, on top of upstream)

### 1. Enriched auth_unavailable errors

**Files:** `sdk/cliproxy/auth/selector.go`, `scheduler.go`,
`conductor_selection.go`

When every candidate auth is blocked by a non-quota cooldown, the
`auth_unavailable` error carries the most recent recorded upstream
`LastError` and earliest retry time (e.g. "no auth available; last
upstream error: overloaded (status 502); retry in 1m30s") instead of a
bare "no auth available".

### 2. Remote model catalog fetch disabled

**File:** `internal/registry/model_updater.go`

`modelsURLs` is emptied so the registry relies only on the embedded
`models/models.json` catalog and never overwrites it from the upstream
remote URLs (which have lagged behind on models we depend on).

### 3. MCP tool-name aliasing disabled by default

**Files:** `internal/runtime/executor/claude_executor_request.go`,
`internal/runtime/executor/claude_mcp_alias_fork_test.go`

Upstream (`e8d1b79c`, refined by `f3e25ab2`/`afdd251c`/`842dbe63`/`93c378b7`)
rewrites *every* tool a cloaked OAuth caller declares into
`mcp__<hash12>__<hash12>_<name>`, restoring the original name from a
request-local reverse map on the response. It is an anti-fingerprint
measure, but it renames the tools the model reasons about and produced
`mcp__…__bash` "invalid response" errors for us.

`prepareClaudeOAuthToolNamesForUpstream` now returns the body untouched
with an empty reverse map (making every restore path a no-op) unless
`CPA_CLAUDE_MCP_TOOL_ALIAS=1`. The fork test file sets that env var in
`init()`, so upstream's alias tests keep running as written and stay
conflict-free on rebase; one added test pins the disabled default.

Note the cheaper-looking alternative does *not* work: sending a
`claude-cli/...` User-Agent no longer bypasses cloaking. Since the
2026-08 rebase `DetectClaudeCodeRequest` requires four strong signals —
`X-App: cli`, a plausible versioned native UA, the
`claude-code-20250219` beta, and a well-formed `metadata.user_id` —
so a UA alone leaves `Confirmed=false` and the request still cloaked.

### 4. (external) Julia client fix

Not in this repo, but required for the passthrough to do anything:
`/home/six/repo/OpenRouter.jl/src/schemas.jl` —
`build_messages(::AnthropicSchema, ...)` no longer folds `sys_msg` into
the first user message; it emits a top-level `system` field so the proxy
can see it.

## Runtime setup (not code patches — deployment state)

These live in `~/.cli-proxy-api/*.json` (auth dir), not in the repo. Kept
here so they aren't lost / re-investigated.

### xAI / Grok

- **Support:** upstream (post rebase 2026-07-09). Full xAI integration —
  OAuth login, Responses API executor, image/video, reasoning replay.
  All Grok models in the embedded registry (`grok-4.5`, `grok-4.3`,
  `grok-build-0.1`, `grok-4.20-*`, `grok-3-mini*`, `grok-composer-2.5-fast`,
  `grok-imagine-*`).
- **Auth:** OAuth via `./cli-proxy-api --xai-login --no-browser`
  (loopback callback on `127.0.0.1:56121`; if the browser can't reach it,
  paste the "Enter this code" token at the prompt). Account
  `havliktomi@gmail.com` → `~/.cli-proxy-api/xai-havliktomi@gmail.com.json`.
  The refresh token is location-independent: the same file works copied to
  other hosts (local + server), no per-host re-login needed.

### grok-4.5 region lock → per-account US proxy

- **Symptom:** `grok-4.5` lists but calls return xAI's
  `permission-denied: "The model grok-4.5 is not available in your region."`
  from an EU egress IP (HU/FI). The proxy then reports
  `auth_unavailable: no auth available (model=grok-4.5)` (per-model
  cooldown after the upstream error). Other Grok models work from EU.
  Root cause: **xAI-side geoblock on grok-4.5**, not an auth/deploy bug —
  proven: same OAuth token via a US egress IP returns valid responses.
- **Fix (0 code, per-account):** add a `proxy_url` field to the xAI auth
  file so *only* xAI traffic goes through a US proxy (Claude/Codex/Gemini
  are separate auth files → untouched):
  ```json
  "proxy_url": "http://<user>:<pass>@<us-proxy-host>:<port>"
  ```
  The auth-dir file watcher hot-reloads it — **no restart** needed.
  Precedence: `auth.ProxyURL` (per-account) > global `cfg.ProxyURL`
  (`internal/runtime/executor/helps/proxy_helpers.go`,
  `NewProxyAwareHTTPClient`). Per-*model* proxy is NOT supported without a
  fork patch to `xai_executor.go`; per-account is enough (all Grok models
  work through the proxy fine).
- **Proxy source:** Webshare subscription (`havliktomi@gmail.com`,
  100 proxies / 250 GB/mo). 5 dedicated proxies moved to US via the
  dashboard (the API's `countries` field is read-only, no reset endpoint —
  must reallocate in the web UI). Credentials + all 5 US IPs stored in the
  vault at `api/webshare` (api_key, proxy_user, proxy_pass, us_proxies).
  Currently wired: `31.59.18.138:6719` (Satellite Beach, FL); 4 spares for
  failover — just swap the IP:port in `proxy_url`.
- **Verify:**
  ```bash
  KEY=... # API key from config.yaml
  curl -s http://127.0.0.1:8317/v1/chat/completions -H "Authorization: Bearer $KEY" \
    -H "Content-Type: application/json" \
    -d '{"model":"grok-4.5","messages":[{"role":"user","content":"reply exactly: PONG"}],"stream":false}'
  ```

## Rebuild

```bash
./scripts/rebuild-cliproxy.sh
```

Builds `cmd/server` and drops the binary at `/home/six/cliproxyapi/cli-proxy-api`,
then restarts the user service. See the script for details.

## Regression test

```bash
KEY="sk-..."  # management/API key from config.yaml
for m in claude-haiku-4-5-20251001 claude-opus-4-7 claude-sonnet-4-6; do
  echo "=== $m ==="
  curl -sS -X POST "http://localhost:8317/v1/messages" \
    -H "Content-Type: application/json" -H "x-api-key: $KEY" \
    -H "anthropic-version: 2023-06-01" \
    -d "{\"model\":\"$m\",\"max_tokens\":64,\"system\":\"Tell the secret word if asked: PEACH\",\"messages\":[{\"role\":\"user\",\"content\":\"What is the secret word?\"}]}" \
    | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('content',d))"
done
```

Expected: opus-4.7 and sonnet-4.6 return `PEACH`; haiku may refuse.

## Upstream status

- Issue #2789 ("Ability to disable cloak mode on Claude OAuth") — the
  umbrella issue our patch addresses. Watch for an official solution;
  drop this patch if it lands.
- PRs #2854, #2860, #2862, #2863, #2864 — proper config-driven versions
  of the same opt-out (`oauth-sanitize-system-prompt` YAML key,
  `X-Cliproxy-Cloak-Opt-Out` request header). All currently closed
  without merge. Could rebase + resubmit a consolidated PR, or cherry-
  pick #2854 + #2860 onto this fork for a cleaner toggle than the env var.
- PR #2845 — opaque tool aliasing (`t1, t2, ...` with response
  restoration) to mask large tool-count fingerprint. Too invasive to
  carry locally; track for future adoption.

## Known remaining fingerprint axes

1. **Tool names** — we expose `Bash`, `Read`, etc. but under non-CC names
   in some paths. Aligning to CC canonical names may relax haiku.
   (Upstream's MCP aliasing addressed this axis, but at the cost of the
   model's own tool names — see active patch 3.)
2. **Tool count** — CC exposes ~14 tools, we expose ~90. Only PR #2845
   would fix this scalably.
