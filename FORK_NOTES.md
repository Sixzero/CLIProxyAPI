# Fork Notes (Sixzero/CLIProxyAPI)

Local patches on top of `upstream/main` (router-for-me/CLIProxyAPI), plus
runtime/deployment state worth remembering.
Do not lose these on re-clone — they are what makes user `system` prompts
actually reach Claude through the OAuth path without triggering 429, and
document how Grok is authed / region-unblocked.

## Active patches (uncommitted on `main`)

### 1. OAuth system-prompt passthrough (always on)

**File:** `internal/runtime/executor/claude_executor.go`

Removes the cloak-mode `sanitizeForwardedSystemPrompt` call (and the
function itself) so the user's real `system` text always flows through to
the model verbatim. Cloaking (device fingerprint + CC system block +
billing header → no 429) is preserved.

Previously this was gated behind `CLIPROXY_OAUTH_PASSTHROUGH_SYSTEM=1`;
that env var is no longer read — passthrough is the default and only
behavior.

**Behavior:**
- Cloaking ON (CC system prompt, device fingerprint, billing header preserved).
- User `system` text is prepended to the first user message verbatim (not
  replaced with the neutral 3-line reminder).
- Opus-4.7 and Sonnet-4.6 follow prepended instructions (PEACH test passes).
- Haiku-4.5 still refuses — it prioritizes CC's authoritative system block.
  That is a model-behavior limit, not a proxy-code bug.

### 2. Remote model catalog fetch disabled

**File:** `internal/registry/model_updater.go`

`modelsURLs` is emptied so the registry relies only on the embedded
`models/models.json` catalog and never overwrites it from the upstream
remote URLs (which have lagged behind on models we depend on).

### 3. (external) Julia client fix

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
2. **Tool count** — CC exposes ~14 tools, we expose ~90. Only PR #2845
   would fix this scalably.
