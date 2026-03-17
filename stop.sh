#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RUN_DIR="$ROOT_DIR/.run"

stop_pid_file() {
  local name="$1"
  local file="$2"

  if [[ ! -f "$file" ]]; then
    echo "${name}: no pid file"
    return 0
  fi

  local pid
  pid="$(cat "$file")"

  if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
    echo "${name}: stopped pid ${pid}"
  else
    echo "${name}: pid ${pid} not running"
  fi

  rm -f "$file"
}

stop_pid_file "backend" "$RUN_DIR/backend.pid"
stop_pid_file "frontend" "$RUN_DIR/frontend.pid"
