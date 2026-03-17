#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BACKEND_DIR="$ROOT_DIR/backend"
FRONTEND_DIR="$ROOT_DIR/frontend"
RUN_DIR="$ROOT_DIR/.run"

BACKEND_PORT="${BACKEND_PORT:-8080}"
FRONTEND_PORT="${FRONTEND_PORT:-5173}"
GO_CACHE_DIR="${GO_CACHE_DIR:-/tmp/go-build}"

backend_pid=""
frontend_pid=""

mkdir -p "${RUN_DIR}"

cleanup() {
  if [[ -n "${frontend_pid}" ]] && kill -0 "${frontend_pid}" 2>/dev/null; then
    kill "${frontend_pid}" 2>/dev/null || true
    wait "${frontend_pid}" 2>/dev/null || true
  fi

  if [[ -n "${backend_pid}" ]] && kill -0 "${backend_pid}" 2>/dev/null; then
    kill "${backend_pid}" 2>/dev/null || true
    wait "${backend_pid}" 2>/dev/null || true
  fi

  rm -f "${RUN_DIR}/backend.pid" "${RUN_DIR}/frontend.pid"
}

trap cleanup EXIT INT TERM

wait_for_http() {
  local url="$1"
  local name="$2"
  local retries="${3:-30}"
  local i

  for ((i = 1; i <= retries; i++)); do
    if curl -fsS "$url" >/dev/null 2>&1; then
      echo "${name} is ready: ${url}"
      return 0
    fi
    sleep 1
  done

  echo "${name} failed to become ready: ${url}" >&2
  return 1
}

echo "Starting backend on http://localhost:${BACKEND_PORT}"
(
  cd "${BACKEND_DIR}"
  APP_PORT="${BACKEND_PORT}" GOCACHE="${GO_CACHE_DIR}" go run .
) &
backend_pid=$!
echo "${backend_pid}" > "${RUN_DIR}/backend.pid"

echo "Starting frontend on http://localhost:${FRONTEND_PORT}"
(
  cd "${FRONTEND_DIR}"
  npm run dev -- --host 0.0.0.0 --port "${FRONTEND_PORT}"
) &
frontend_pid=$!
echo "${frontend_pid}" > "${RUN_DIR}/frontend.pid"

wait_for_http "http://localhost:${BACKEND_PORT}/api/health" "Backend"
wait_for_http "http://localhost:${FRONTEND_PORT}" "Frontend"

echo
echo "WaitWhat is running."
echo "Frontend: http://localhost:${FRONTEND_PORT}"
echo "Backend:  http://localhost:${BACKEND_PORT}"
echo "Press Ctrl+C to stop both services."
echo

wait "${backend_pid}" "${frontend_pid}"
