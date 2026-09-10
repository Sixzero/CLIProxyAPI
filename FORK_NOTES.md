# Fork Notes (Sixzero/CLIProxyAPI)

Local patches on top of `upstream/main` (router-for-me/CLIProxyAPI), plus
runtime/deployment state worth remembering.

## Retired patches (superseded by upstream — 2026-08-13 rebase onto d757063c)

Goal is to keep this list shrinking: prefer upstream absorbing our behavior
over carrying patches.

- **Remote model catalog fetch disabled** (2026-08-19, `modelsURLs = []`)
  — upstream's `--local-model` flag does exactly this ("Local model mode:
  using embedded model catalogs, remote model updates disabled"), so the
  patch was reverted and the flag added to the systemd unit's `ExecStart`.
- **Anthropic thinking display** (2026-08-19, never committed) — the empty
  thinking text is fixed with an upstream `payload.default` config rule
  instead of a code patch, see "Anthropic thinking summaries" below.

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

### 2. MCP tool-name aliasing disabled by default

**Files:** `internal/runtime/executor/claude_executor_request.go`,
`internal/runtime/executor/claude_mcp_alias_fork_test.go`,
plus one added line per opted-in test in
`internal/runtime/executor/claude_executor_test.go` and
`internal/runtime/executor/claude_executor_thinking_signature_test.go`

Upstream (`e8d1b79c`, refined by `f3e25ab2`/`afdd251c`/`842dbe63`/`93c378b7`)
rewrites every *custom* tool a cloaked OAuth caller declares (names that
already follow the MCP convention and typed Anthropic server tools are
left alone) into
`mcp__<hash12>__<hash12>_<name>`, restoring the original name from a
request-local reverse map on the response. It is an anti-fingerprint
measure, but it renames the tools the model reasons about and produced
`mcp__…__bash` "invalid response" errors for us.

`prepareClaudeOAuthToolNamesForUpstream` now returns the body untouched
with an empty reverse map (making every restore path a no-op) unless
`CPA_CLAUDE_MCP_TOOL_ALIAS=1`.

Tests keep the *fork's* behaviour as the suite default: the seven upstream
tests that describe aliasing opt back in with a one-line
`enableClaudeMCPToolAliasForTest(t)`, so a new upstream test that assumes
aliasing fails loudly instead of silently hiding a regression in our
default. Four fork-owned tests pin the disabled default — one on the
helper and one per request path (execute, stream, count_tokens), because a
call site bypassing the helper would still alias tools in production.

Note the cheaper-looking alternative does *not* work: sending a
`claude-cli/...` User-Agent no longer bypasses cloaking. Since the
2026-08 rebase `DetectClaudeCodeRequest` requires four strong signals —
`X-App: cli`, a plausible versioned native UA, the
`claude-code-20250219` beta, and a well-formed `metadata.user_id` —
so a UA alone leaves `Confirmed=false` and the request still cloaked.

### 3. claude-fable 429 no longer cools sibling Claude models

`sdk/cliproxy/auth/conductor_cooldown.go` — Anthropic rejects
`claude-fable-5` with the same unified 5h/7d "rejected" headers it uses
for a genuine account-wide limit, but fable has a smaller subscription
quota than the rest of the line-up. The credential-scoped fan-out
therefore cooled every Claude model on the credential (opus included)
for up to 7 days until a restart. `isCredentialFanoutExempt` skips the
fan-out for `claude-fable-*`; the rejected model still cools through its
own ModelState with the full header deadline. Regression test:
`TestAuthManager_ClaudeFable429DoesNotCoolSiblingModels` (fails with the
guard reverted). If another narrow-quota model appears, extend the
prefix check.

### 4. (external) Julia client fix

Not in this repo, but required for the passthrough to do anything:
`/home/six/repo/OpenRouter.jl/src/schemas.jl` —
`build_messages(::AnthropicSchema, ...)` no longer folds `sys_msg` into
the first user message; it emits a top-level `system` field so the proxy
can see it.

## Runtime setup (not code patches — deployment state)

These live in `~/.cli-proxy-api/*.json` (auth dir) and `config.yaml`, not in
the repo. Kept here so they aren't lost / re-investigated.

### Anthropic thinking summaries (config, not a patch)

Cloaked/translated requests always claim `cc_entrypoint=cli`, so
`claudeCodeCLIBetas` sends `redact-thinking-2026-02-12` and Anthropic
answers with thinking blocks carrying a signature and an **empty**
`thinking` field — thinking tokens billed, nothing shown. Upstream's only
switch is `claudeThinkingDisplaySet`: a body-level `thinking.display`
suppresses that beta (upstream `9b114239`).

Fixed with zero code, in `~/cliproxyapi/config.yaml` (hot-reloaded):

```yaml
payload:
  default:                       # only fills the field when the caller omitted it
    - models:
        - name: "claude-*"
          protocol: "claude"
          exist:
            - "thinking.type"    # REQUIRED: without it every non-thinking
                                 # request gets a bare display and Anthropic
                                 # returns "thinking.type: Field required"
      params:
        "thinking.display": "summarized"
```

Verified: `(high)` suffix, explicit `thinking.type=enabled`, streaming and
the OpenAI chat-completions path all return thinking text; a request
without thinking still works; a caller-supplied `display:"omitted"` is
preserved; `tool_choice:{"type":"any"}` still succeeds (upstream's
`disableThinkingIfToolChoiceForced` removes the whole `thinking` object).

Note the `anthropic-beta: interleaved-thinking-2025-05-14` header the Julia
client sends (`OpenRouterCLIProxyAPI.jl`, `ANTHROPIC_THINKING_HEADERS`) is a
**no-op** for cloaked requests: the beta list is rebuilt server-side and an
unconfirmed caller's own betas are dropped. It only appeared to work in
early 2026-08 because the beta list looked different then.

### Model catalog: remote fetch is ON again (2026-09-02)

`--local-model` was dropped from both `ExecStart`s. It pinned the registry to
the embedded `models/models.json`, and that file did not have
`claude-fable-5-1` while the remote catalog
(`router-for-me/models/models.json`) did — the opposite of the lag that made
us add the flag in 2026-08. Without the flag the catalog refreshes from
remote on startup and every 3 h. If remote lags again, the alternative is a
fork commit adding the model to the embedded JSON + rebuild, not the flag.

### Claude Code version we impersonate (config, not a patch)

Anthropic gates new models on the *client version* in the cloaked headers:
`claude-fable-5-1` answered `Claude Code 2.1.220 does not support this
model; version 2.1.251 or newer is required`. The version comes from the
User-Agent baseline in `claude_device_profile.go`, overridable per host in
`config.yaml` (hot-reloaded, no rebuild):

```yaml
claude-header-defaults:
  user-agent: "claude-cli/2.1.251 (external, cli)"
```

Applied on local and `ssh todoforai` (2026-09-02); a new host needs it too.
Bump the number the next time a model rejects with the same message.
Regression-checked after the bump: opus-4.7 / sonnet-4.6 / opus-5(high) /
fable-5(high) still answer (see "Regression test" below).

The `payload` block in `config.yaml` is likewise per-host state.

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

### Dead Claude auth looks like broken weighted-round-robin (2026-09-10)

One Claude auth got ~1% of new bindings despite `weighted-round-robin`. Its
refresh token had expired (`journalctl -u cliproxyapi | grep "Refresh token
expired"`, `expired` in the JSON stuck in the past), so the selector skipped it.
Routing was fine. Check: `jq '{email,expired}' ~/.cli-proxy-api/claude-*.json`.

Fix: re-login locally (`./cli-proxy-api -config config.yaml -claude-login
-no-browser`, keep it running with a live stdin until the `localhost:54545`
callback lands; each restart mints a new `state`), then `scp` the JSON to
`todoforai:/root/.cli-proxy-api/` — auth-dir is hot-reloaded, no restart needed.
Restarting `cliproxyapi` is OK; on **prod** never directly restart the backend
(pm2 `backend-4000`) or the agent — they deploy with zero downtime; a manual
restart drops live connections.
Don't set `weight` by hand: the todoforai backend's `ProviderWeightService`
recomputes it hourly as remaining weekly quota / hours until reset, via the
management API. Note `session-affinity` (3h) means only *new* bindings follow
the weights, not total request share.

## Rebuild / deploy

```bash
./scripts/rebuild-cliproxy.sh            # local only (default)
./scripts/rebuild-cliproxy.sh --remote   # todoforai server only
./scripts/rebuild-cliproxy.sh --all      # both
```

Two deployments run this fork, each as a **user** systemd unit
(`cliproxyapi.service`):

| target | binary | notes |
| --- | --- | --- |
| local | `/home/six/cliproxyapi/cli-proxy-api` | `systemctl --user ...` as `six` |
| `ssh todoforai` | `/root/cliproxyapi/cli-proxy-api` | user unit **under root**: needs `XDG_RUNTIME_DIR=/run/user/0`, otherwise a system-level `systemctl is-active cliproxyapi` wrongly reports `inactive`. Built static (`CGO_ENABLED=0`) since the server's glibc may differ. |

**Trap (2026-08-18):** a *second*, system-level unit
`/etc/systemd/system/cli-proxy-api.service` (note the dashes) also runs the
same binary as user `six` and had been holding port 8317 since Aug 15, so
the user unit restart-looped on "address already in use" and the rebuilt
binary never took effect (deleted inode still served requests). If a fresh
build seems to change nothing, check `ss -tlnp | grep 8317` and
`ls -l /proc/<pid>/exe` for a `(deleted)` target. The system unit should
probably be disabled for good (`sudo systemctl disable --now
cli-proxy-api`), needs root.

**Server (2026-09-02):** the mirror image of the same trap — a *system*
unit `/etc/systemd/system/cliproxyapi.service` (root) held 8317 while the
*user* unit restart-looped. Resolved by disabling the user unit; on
`ssh todoforai` the **system unit is the live one** now
(`systemctl restart cliproxyapi`, no `XDG_RUNTIME_DIR` dance).

The running binary can't be overwritten (`Text file busy`), so the script
stops the service, swaps the file (keeping a timestamped `.bak.*`) and
starts it — the server drops in-flight requests for ~2s. `curl`ing
`/v1/models` without a key returns `401`, which is the "it's up" signal.

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
- Worth upstreaming from this fork: (a) the enriched `auth_unavailable`
  error (patch 1) and (b) a config toggle for MCP tool aliasing (patch 2),
  which would leave the fork with zero code patches. Both are currently
  carried only because upstream offers no switch.

## Known remaining fingerprint axes

1. **Tool names** — we expose `Bash`, `Read`, etc. but under non-CC names
   in some paths. Aligning to CC canonical names may relax haiku.
   (Upstream's MCP aliasing addressed this axis, but at the cost of the
   model's own tool names — see active patch 2.)
2. **Tool count** — CC exposes ~14 tools, we expose ~90. Only PR #2845
   would fix this scalably.
