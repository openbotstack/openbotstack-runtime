#!/usr/bin/env bash
# ctl.sh — OpenBotStack service controller
#
# Usage:
#   ./ctl.sh start        Start in background, print bootstrap info
#   ./ctl.sh stop         Graceful shutdown (SIGTERM → SIGKILL after 15s)
#   ./ctl.sh status       Print running/stopped + pid
#   ./ctl.sh restart      stop + start
#   ./ctl.sh fg           Start in foreground (for debugging)
#
# On first run (no database), the admin API key is extracted and displayed.

set -euo pipefail

cd "$(dirname "$0")"

PID_FILE="./openbotstack.pid"
LOG_FILE="./logs/openbotstack.log"
BINARY="./openbotstack"

# ---- helpers ----------------------------------------------------------

is_running() {
    [ -f "${PID_FILE}" ] && kill -0 "$(cat "${PID_FILE}")" 2>/dev/null
}

ts() { printf '[%(%m-%d %H:%M:%S)T] ' -1; }
info()  { echo "$(ts) $*"; }
warn()  { echo "$(ts) [WARN] $*" >&2; }

bootstrap_info() {
    local addr="${OBS_SERVER_ADDR:-:8080}"
    # Expand :8080 → http://127.0.0.1:8080 for curl.
    local base_url
    if echo "${addr}" | grep -q '^:'; then
        base_url="http://127.0.0.1${addr}"
    elif echo "${addr}" | grep -q '^0\.0\.0\.0'; then
        base_url="http://127.0.0.1:$(echo "${addr}" | cut -d: -f2-)"
    else
        base_url="http://${addr}"
    fi

    # Fetch version info from the running server.
    local ver_info
    ver_info=$(curl -s "${base_url}/version" 2>/dev/null || true)

    echo ""
    echo "══════════════════════════════════════════════════════════"
    echo "  OpenBotStack"
    if [ -n "${ver_info}" ]; then
        echo "  Version : $(echo "${ver_info}" | grep -o '"version":"[^"]*"' | cut -d'"' -f4 || echo unknown)"
        echo "  Commit  : $(echo "${ver_info}" | grep -o '"commit":"[^"]*"' | cut -d'"' -f4 || echo -)"
        echo "  Go      : $(echo "${ver_info}" | grep -o '"go_version":"[^"]*"' | cut -d'"' -f4 || echo -)"
    else
        echo "  Version : unknown (server not reachable yet)"
    fi
    echo "  PID     : $(cat "${PID_FILE}")"
    echo "  Addr    : ${addr}"
    echo "  Log     : ${LOG_FILE}"
    echo "══════════════════════════════════════════════════════════"

    # On first run the binary prints a default admin API key to stdout.
    # Capture it from the startup log so the operator can save it.
    local boot_log
    boot_log=$(head -100 "${LOG_FILE}" 2>/dev/null || true)
    local key_line
    key_line=$(echo "${boot_log}" | grep -o 'obs_[a-f0-9]\{32\}' | head -1 || true)
    if [ -n "${key_line}" ]; then
        echo ""
        echo "  ╔════════════════════════════════════════════════════╗"
        echo "  ║  FIRST RUN — Default Admin API Key               ║"
        echo "  ║  ${key_line}              ║"
        echo "  ║  Tenant: default  User: admin  Role: admin       ║"
        echo "  ║  SAVE THIS KEY — it will not be shown again.     ║"
        echo "  ╚════════════════════════════════════════════════════╝"
        echo ""
    fi
}

# ---- commands ----------------------------------------------------------

cmd_start() {
    if is_running; then
        warn "already running (pid $(cat "${PID_FILE}"))"
        exit 1
    fi

    # Source .env if present.
    if [ -f .env ]; then
        set -a; source .env; set +a
    fi

    # Ensure runtime directories.
    mkdir -p logs data/skills

    info "starting..."
    nohup "${BINARY}" >> "${LOG_FILE}" 2>&1 &
    echo $! > "${PID_FILE}"

    # Wait for the binary to either bind its port or crash.
    sleep 2
    if ! is_running; then
        warn "FAILED — check ${LOG_FILE}"
        tail -20 "${LOG_FILE}"
        rm -f "${PID_FILE}"
        exit 1
    fi

    info "started (pid $(cat "${PID_FILE}"))"
    bootstrap_info
}

cmd_stop() {
    if ! is_running; then
        info "not running"
        rm -f "${PID_FILE}"
        return 0
    fi
    local pid; pid=$(cat "${PID_FILE}")
    info "stopping (pid ${pid})..."
    kill "${pid}" 2>/dev/null || true
    for _ in $(seq 1 15); do
        kill -0 "${pid}" 2>/dev/null || { rm -f "${PID_FILE}"; info "stopped"; return 0; }
        sleep 1
    done
    warn "force-killing (pid ${pid})"
    kill -9 "${pid}" 2>/dev/null || true
    rm -f "${PID_FILE}"
    info "force-stopped"
}

cmd_status() {
    if is_running; then
        local pid; pid=$(cat "${PID_FILE}")
        info "running (pid ${pid})"
        # Show recent log tail for quick health check.
        echo "  --- last 5 log lines ---"
        tail -5 "${LOG_FILE}" 2>/dev/null | sed 's/^/  /' || true
    else
        info "stopped"
    fi
}

cmd_restart() {
    cmd_stop
    sleep 1
    cmd_start
}

cmd_fg() {
    if [ -f .env ]; then
        set -a; source .env; set +a
    fi
    mkdir -p logs data/skills
    info "starting in foreground..."
    exec "${BINARY}"
}

# ---- main -------------------------------------------------------------

case "${1:-}" in
    start)    cmd_start ;;
    stop)     cmd_stop ;;
    status)   cmd_status ;;
    restart)  cmd_restart ;;
    fg)       cmd_fg ;;
    *)
        echo "Usage: $0 {start|stop|status|restart|fg}"
        echo ""
        echo "  start    Start in background, print bootstrap info"
        echo "  stop     Graceful shutdown"
        echo "  status   Show running/stopped + recent logs"
        echo "  restart  stop + start"
        echo "  fg       Foreground mode (for debugging)"
        exit 1
        ;;
esac
