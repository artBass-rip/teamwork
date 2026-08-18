#!/bin/sh
set -eu

mkdir -p data
jira_key_file="$PWD/data/.jira-token-key"
legacy_pid_file="$PWD/data/mcp-gateway.pid"
legacy_token_file="$PWD/data/.mcp-gateway-token"

# Upgrade cleanup: stop a Gateway left by TeamWork versions older than 1.7.
if [ "$(uname -s)" = Darwin ] && command -v launchctl >/dev/null 2>&1; then
  launchctl remove com.artbass.teamwork.mcp-gateway >/dev/null 2>&1 || true
fi
if [ -f "$legacy_pid_file" ]; then
  legacy_pid="$(sed -n '1p' "$legacy_pid_file" 2>/dev/null || true)"
  case "$legacy_pid" in ''|*[!0-9]*) legacy_pid='' ;; esac
  if [ -n "$legacy_pid" ]; then
    legacy_command="$(ps -p "$legacy_pid" -o command= 2>/dev/null || true)"
    if echo "$legacy_command" | grep -q 'docker mcp gateway run'; then kill "$legacy_pid" >/dev/null 2>&1 || true; fi
  fi
fi
rm -f "$legacy_pid_file" "$legacy_token_file"

if command -v openssl >/dev/null 2>&1; then
  MCP_INTERNAL_AUTH_TOKEN="$(openssl rand -hex 24)"
else
  MCP_INTERNAL_AUTH_TOKEN="$(date +%s)-$$-teamwork-mcp"
fi
export MCP_INTERNAL_AUTH_TOKEN

if [ ! -s "$jira_key_file" ]; then
  umask 077
  if command -v openssl >/dev/null 2>&1; then openssl rand -hex 32 >"$jira_key_file"; else echo "Для генерации ключа Jira OAuth требуется openssl." >&2; exit 1; fi
fi
chmod 600 "$jira_key_file"
JIRA_TOKEN_ENCRYPTION_KEY="$(cat "$jira_key_file")"
export JIRA_TOKEN_ENCRYPTION_KEY

docker compose up --build -d
if [ "${1:-}" = "--pull-model" ]; then
  docker compose exec -T ollama ollama pull "${OLLAMA_MODEL:-qwen2.5:3b}"
fi
echo "TeamWork запущен: http://localhost:${APP_PORT:-8080}"
echo "Встроенный Jira MCP: jira-mcp:8081"
