#!/bin/sh
set -eu

profile="${MCP_PROFILE:-ecom_2_0}"
port="${MCP_GATEWAY_PORT:-8811}"
mkdir -p data
gateway_pid_file="data/mcp-gateway.pid"
gateway_token_file="$PWD/data/.mcp-gateway-token"
gateway_log_file="$PWD/data/mcp-gateway.log"
launchd_label="com.artbass.teamwork.mcp-gateway"
docker_bin="$(command -v docker)"

if ! docker mcp profile show "$profile" >/dev/null 2>&1; then
  echo "Профиль Docker MCP '$profile' не найден." >&2
  exit 1
fi

if command -v openssl >/dev/null 2>&1; then
  MCP_GATEWAY_AUTH_TOKEN="$(openssl rand -hex 24)"
else
  MCP_GATEWAY_AUTH_TOKEN="$(date +%s)-$$-jira-do-mcp"
fi
export MCP_GATEWAY_AUTH_TOKEN

gateway_mode=process
if [ "$(uname -s)" = Darwin ] && command -v launchctl >/dev/null 2>&1; then
  gateway_mode=launchd
  launchctl remove "$launchd_label" >/dev/null 2>&1 || true
  # launchctl removal is asynchronous. Reusing the label or truncating the log
  # before the previous job exits can make submit fail and produce a sparse log.
  for _ in $(seq 1 50); do
    launchctl list "$launchd_label" >/dev/null 2>&1 || break
    sleep 0.1
  done
  if launchctl list "$launchd_label" >/dev/null 2>&1; then
    echo "Предыдущий Docker MCP Gateway не завершился." >&2
    exit 1
  fi
  umask 077
  printf '%s' "$MCP_GATEWAY_AUTH_TOKEN" >"$gateway_token_file"
  : >"$gateway_log_file"
  launchctl submit -l "$launchd_label" -- \
    /bin/sh "$PWD/scripts/run-mcp-gateway.sh" "$gateway_token_file" "$profile" "$port" "$gateway_log_file" "$docker_bin"
else
  if [ -f "$gateway_pid_file" ]; then
    previous_pid="$(cat "$gateway_pid_file" 2>/dev/null || true)"
    case "$previous_pid" in ''|*[!0-9]*) previous_pid='' ;; esac
    previous_command=''
    if [ -n "$previous_pid" ]; then previous_command="$(ps -p "$previous_pid" -o command= 2>/dev/null || true)"; fi
    if [ -n "$previous_pid" ] && echo "$previous_command" | grep -q 'docker mcp gateway run' && kill -0 "$previous_pid" 2>/dev/null; then
      kill "$previous_pid" >/dev/null 2>&1 || true
    fi
  fi
  nohup docker mcp gateway run --profile "$profile" --transport streaming --port "$port" \
    >"$gateway_log_file" 2>&1 </dev/null &
  gateway_pid=$!
  echo "$gateway_pid" >"$gateway_pid_file"
fi

ready=0
for _ in $(seq 1 30); do
  if grep -q 'Start streaming server' "$gateway_log_file" 2>/dev/null; then ready=1; break; fi
  gateway_alive=1
  if [ "$gateway_mode" = launchd ]; then
    launchctl list "$launchd_label" >/dev/null 2>&1 || gateway_alive=0
  else
    kill -0 "$gateway_pid" 2>/dev/null || gateway_alive=0
  fi
  if [ "$gateway_alive" -ne 1 ]; then
    echo "Docker MCP Gateway завершился. Последние строки:" >&2
    tail -30 "$gateway_log_file" >&2
    rm -f "$gateway_token_file"
    rm -f "$gateway_pid_file"
    exit 1
  fi
  sleep 1
done

if [ "$ready" -ne 1 ]; then
  echo "Docker MCP Gateway не запустился за 30 секунд." >&2
  exit 1
fi

docker compose up --build -d
echo "TeamWork запущен: http://localhost:${APP_PORT:-8080}"
if [ "$gateway_mode" = launchd ]; then
  echo "Docker MCP Gateway управляется launchd: $launchd_label"
else
  echo "Docker MCP Gateway PID: $gateway_pid"
fi

if [ "${MCP_GATEWAY_FOREGROUND:-0}" = "1" ] && [ "$gateway_mode" = process ]; then
  cleanup() {
    kill "$gateway_pid" >/dev/null 2>&1 || true
  }
  trap cleanup EXIT INT TERM
  echo "Gateway работает в режиме супервизора. Для остановки нажмите Ctrl+C."
  wait "$gateway_pid"
fi
