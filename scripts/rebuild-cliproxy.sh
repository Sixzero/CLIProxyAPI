#!/usr/bin/env bash
# rebuild-cliproxy.sh — build our patched CLIProxyAPI and restart the service.
#
# Targets (pick with --local / --remote / --all, default --local):
#   local   /home/six/cliproxyapi/cli-proxy-api      (user unit, this machine)
#   remote  todoforai:/root/cliproxyapi/cli-proxy-api (user unit under root;
#           needs XDG_RUNTIME_DIR=/run/user/0, or systemctl reports it inactive)
#
# The binary cannot be overwritten while running ("Text file busy"), so each
# target stops the service, swaps the file and starts it again. In-flight
# requests are dropped for those ~2s; the remote proxy serves live traffic.
#
# Our fork's patches are listed in FORK_NOTES.md — currently enriched
# auth_unavailable errors, no remote model-catalog fetch, and MCP tool-name
# aliasing disabled by default (CPA_CLAUDE_MCP_TOOL_ALIAS=1 restores upstream).
#
# Typical flow after an upstream pull:
#   git fetch upstream && git rebase upstream/main   # resolve patch conflicts
#   go test ./internal/runtime/executor/
#   ./scripts/rebuild-cliproxy.sh --all

set -euo pipefail

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN_NAME="cli-proxy-api"
SERVICE="${CLIPROXY_SERVICE:-cliproxyapi.service}"
INSTALL_DIR="${CLIPROXY_INSTALL_DIR:-/home/six/cliproxyapi}"
REMOTE_HOST="${CLIPROXY_REMOTE_HOST:-todoforai}"
REMOTE_DIR="${CLIPROXY_REMOTE_DIR:-/root/cliproxyapi}"

do_local=false
do_remote=false
case "${1:---local}" in
  --local) do_local=true ;;
  --remote) do_remote=true ;;
  --all) do_local=true; do_remote=true ;;
  *) echo "usage: $0 [--local|--remote|--all]" >&2; exit 2 ;;
esac

cd "$REPO_DIR"

if $do_local; then
  echo "==> local: go build -> $INSTALL_DIR/$BIN_NAME"
  mkdir -p "$INSTALL_DIR"
  go build -o "/tmp/$BIN_NAME.local" ./cmd/server
  cp -f "$INSTALL_DIR/$BIN_NAME" "$INSTALL_DIR/$BIN_NAME.bak.$(date +%Y%m%d-%H%M%S)" 2>/dev/null || true
  systemctl --user stop "$SERVICE"
  mv -f "/tmp/$BIN_NAME.local" "$INSTALL_DIR/$BIN_NAME"
  systemctl --user daemon-reload
  systemctl --user start "$SERVICE"
  sleep 3
  systemctl --user is-active "$SERVICE"
fi

if $do_remote; then
  # Static build: the server's glibc is not guaranteed to match this machine's.
  echo "==> remote: go build -> $REMOTE_HOST:$REMOTE_DIR/$BIN_NAME"
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" \
    -o "/tmp/$BIN_NAME.remote" ./cmd/server
  scp -q "/tmp/$BIN_NAME.remote" "$REMOTE_HOST:$REMOTE_DIR/$BIN_NAME.new"
  ssh "$REMOTE_HOST" "export XDG_RUNTIME_DIR=/run/user/0; cd '$REMOTE_DIR' \
    && cp -f '$BIN_NAME' '$BIN_NAME.bak.\$(date +%Y%m%d-%H%M%S)' \
    && systemctl --user stop '$SERVICE' && sleep 1 \
    && mv -f '$BIN_NAME.new' '$BIN_NAME' && chmod +x '$BIN_NAME' \
    && systemctl --user start '$SERVICE' && sleep 3 \
    && systemctl --user is-active '$SERVICE'"
fi

echo "==> done. A 401 from the smoke test means the server is up (auth missing):"
echo "    curl -s -o /dev/null -w '%{http_code}\\n' http://localhost:8317/v1/models"
