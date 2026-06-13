#!/usr/bin/env bash
# start.sh — OpenBotStack production launcher
#
# Usage:
#   ./start.sh              # start in foreground (default)
#   ./start.sh --daemon      # start in background via nohup
#   ./start.sh --status      # check if running
#   ./start.sh --stop        # stop running instance
#
# Environment:
#   Sources .env from the script directory before starting.
#   Set OBS_PID_FILE to override the pid file location.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
APP_DIR="${SCRIPT_DIR}/.."
cd "${APP_DIR}"

PID_FILE="${OBS_PID_FILE:-/var/run/openbotstack.pid}"
LOG_FILE="${OBS_LOG_FILE:-./logs/openbotstack.log}"
BINARY="./openbotstack"

# ---- helpers ----------------------------------------------------------

is_running() {
    if [ -f "${PID_FILE}" ]; then
        local pid
        pid=$(cat "${PID_FILE}" 2>/dev/null || true)
        if [ -n "${pid}" ] && kill -0 "${pid}" 2>/dev/null; then
            return 0
        fi
    fi
    return 1
}

log_msg() { printf '[%(%Y-%m-%d %H:%M:%S)T] %s\n' -1 "$*"; }

# ---- commands ---------------------------------------------------------

do_start() {
    if is_running; then
        log_msg "openbotstack is already running (pid $(cat "${PID_FILE}"))"
        exit 1
    fi

    # Source .env from the app directory (exports vars for the binary).
    if [ -f .env ]; then
        set -a
        # shellcheck source=/dev/null
        source .env
        set +a
    fi

    # Ensure runtime directories exist.
    mkdir -p logs data/skills

    if [ "${1:-}" = "--daemon" ]; then
        log_msg "Starting openbotstack in background..."
        nohup "${BINARY}" >> "${LOG_FILE}" 2>&1 &
        echo $! > "${PID_FILE}"
        sleep 1
        if is_running; then
            log_msg "openbotstack started (pid $(cat "${PID_FILE}"))"
        else
            log_msg "FAILED to start openbotstack — check ${LOG_FILE}"
            exit 1
        fi
    else
        log_msg "Starting openbotstack (foreground)..."
        exec "${BINARY}"
    fi
}

do_stop() {
    if ! is_running; then
        log_msg "openbotstack is not running"
        rm -f "${PID_FILE}"
        return 0
    fi
    local pid
    pid=$(cat "${PID_FILE}")
    log_msg "Stopping openbotstack (pid ${pid})..."
    kill "${pid}" 2>/dev/null || true
    # Wait up to 15s for graceful shutdown.
    for _ in $(seq 1 15); do
        if ! kill -0 "${pid}" 2>/dev/null; then
            rm -f "${PID_FILE}"
            log_msg "openbotstack stopped"
            return 0
        fi
        sleep 1
    done
    # Force kill if still alive.
    log_msg "Force killing openbotstack..."
    kill -9 "${pid}" 2>/dev/null || true
    rm -f "${PID_FILE}"
    log_msg "openbotstack force-stopped"
}

do_status() {
    if is_running; then
        log_msg "openbotstack is running (pid $(cat "${PID_FILE}"))"
    else
        log_msg "openbotstack is NOT running"
    fi
}

# ---- main -------------------------------------------------------------

case "${1:-}" in
    --stop)     do_stop ;;
    --status)   do_status ;;
    --help|-h)  sed -n '2,/^$/p' "$0" ;;
    *)          do_start "${1:-}" ;;
esac
