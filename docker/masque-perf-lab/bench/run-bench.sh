#!/bin/sh
set -eu
# Трафик: iperf3 → socat TCP → SOCKS5 127.0.0.1:1080 → sing-box → MASQUE → iperf (BENCH_IPERF_TARGET_*).

SOCKS_PORT="${SOCKS_PORT:-1080}"
IPERF_TARGET_HOST="${IPERF_TARGET_HOST:-172.30.99.2}"
IPERF_TARGET_PORT="${IPERF_TARGET_PORT:-5201}"
RELAY_LISTEN_PORT="${RELAY_LISTEN_PORT:-15201}"
WAIT_SEC="${BENCH_WAIT_SOCKS_SEC:-12}"
CONNECT_TIMEOUT_MS="${IPERF_CONNECT_TIMEOUT_MS:-15000}"
BENCH_TIMEOUT_SEC="${BENCH_TIMEOUT_SEC:-120}"

echo "[bench] target via SOCKS: ${IPERF_TARGET_HOST}:${IPERF_TARGET_PORT} (connect-timeout ${CONNECT_TIMEOUT_MS}ms, wall-timeout ${BENCH_TIMEOUT_SEC}s)"

echo "[bench] waiting ${WAIT_SEC}s for sing-box SOCKS on 127.0.0.1:${SOCKS_PORT}..."
sleep "$WAIT_SEC"

socat TCP-LISTEN:"${RELAY_LISTEN_PORT}",fork,reuseaddr SOCKS5:127.0.0.1:"${IPERF_TARGET_HOST}":"${IPERF_TARGET_PORT}",socksport="${SOCKS_PORT}" &
SOCAT_PID=$!
sleep 2

IPERF_ARGS="${IPERF_ARGS:--t 10 -P 1}"
case "$IPERF_ARGS" in
  *--connect-timeout*) IPERF_CONNECT_FLAG="" ;;
  *) IPERF_CONNECT_FLAG="--connect-timeout ${CONNECT_TIMEOUT_MS}" ;;
esac

BUSY_RETRIES="${IPERF_BUSY_RETRIES:-3}"
BUSY_SLEEP="${IPERF_BUSY_SLEEP_SEC:-5}"
errf="$(mktemp)"
trap 'rm -f "$errf" 2>/dev/null; kill "$SOCAT_PID" 2>/dev/null || true' EXIT INT TERM

attempt=1
while [ "$attempt" -le "$BUSY_RETRIES" ]; do
  echo "[bench] iperf3 attempt ${attempt}/${BUSY_RETRIES} -c 127.0.0.1 -p ${RELAY_LISTEN_PORT} ${IPERF_CONNECT_FLAG} ${IPERF_ARGS}"
  set +e
  timeout "$BENCH_TIMEOUT_SEC" iperf3 -c 127.0.0.1 -p "${RELAY_LISTEN_PORT}" ${IPERF_CONNECT_FLAG} ${IPERF_ARGS} 2>"$errf"
  rc=$?
  set -e
  if [ -s "$errf" ]; then cat "$errf" >&2 || true; fi
  if [ "$rc" -eq 0 ]; then exit 0; fi
  if [ "$rc" -eq 124 ]; then
    echo "[bench] iperf3 wall-clock timeout (${BENCH_TIMEOUT_SEC}s) — проверьте MASQUE/SOCKS и доступ VPS → ${IPERF_TARGET_HOST}:${IPERF_TARGET_PORT}" >&2
    exit 124
  fi
  if grep -qi "busy" "$errf" 2>/dev/null; then
    echo "[bench] iperf server busy, sleep ${BUSY_SLEEP}s..."
    sleep "$BUSY_SLEEP"
    attempt=$((attempt + 1))
    continue
  fi
  exit "$rc"
done
echo "[bench] exhausted retries after busy responses"
exit 1
