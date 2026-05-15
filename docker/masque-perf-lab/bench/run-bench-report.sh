#!/bin/sh
# iperf upload + download; stdout: только RESULT_*.
# BENCH_VIA=tun  — прямой iperf (маршрут через tun0 / auto_route, как Hiddify).
# BENCH_VIA=socks — socat → SOCKS5 (legacy).
set -eu

BENCH_VIA="${BENCH_VIA:-tun}"
IPERF_TARGET_HOST="${IPERF_TARGET_HOST:-163.5.180.181}"
IPERF_TARGET_PORT="${IPERF_TARGET_PORT:-5201}"
SOCKS_PORT="${SOCKS_PORT:-1080}"
RELAY_LISTEN_PORT="${RELAY_LISTEN_PORT:-15201}"
WAIT_SEC="${BENCH_WAIT_SEC:-1}"
CONNECT_TIMEOUT_MS="${IPERF_CONNECT_TIMEOUT_MS:-5000}"
PROBE_SEC="${BENCH_CONNECT_TIMEOUT_SEC:-5}"
DURATION="${IPERF_DURATION_SEC:-6}"
WALL_TIMEOUT="${BENCH_WALL_TIMEOUT_SEC:-$((DURATION + PROBE_SEC + 8))}"

sleep "$WAIT_SEC"

cleanup() {
  kill "$SOCAT_PID" 2>/dev/null || true
}
SOCAT_PID=""

if [ "$BENCH_VIA" = "socks" ]; then
  socat TCP-LISTEN:"${RELAY_LISTEN_PORT}",fork,reuseaddr \
    SOCKS5:127.0.0.1:"${IPERF_TARGET_HOST}":"${IPERF_TARGET_PORT}",socksport="${SOCKS_PORT}" &
  SOCAT_PID=$!
  sleep 1
  trap cleanup EXIT INT TERM
  IPERF_HOST="127.0.0.1"
  IPERF_PORT="${RELAY_LISTEN_PORT}"
else
  IPERF_HOST="${IPERF_TARGET_HOST}"
  IPERF_PORT="${IPERF_TARGET_PORT}"
fi

parse_mbit() {
  echo "$1" | awk '/ (sender|receiver)$/ {
    for (i=1;i<=NF;i++) if ($i ~ /bits\/sec/) { print $(i-1); exit }
  }'
}

# CONNECT-UDP probe: DNS UDP к публичному резолверу (тот же masque outbound, что и TCP iperf).
# iperf3 -u использует отдельный TCP ctrl; через MASQUE маленькие ctrl-записи сливаются — iperf-сервер обрывает сессию ("unable to read from stream socket"), хотя UDP датаплейн может быть жив.
DIG_TARGET="${BENCH_UDP_PROBE_DIG_TARGET:-$IPERF_HOST}"
DIG_PORT="${BENCH_UDP_PROBE_DIG_PORT:-$IPERF_PORT}"
DIG_NAME="${BENCH_UDP_PROBE_DIG_NAME:-cloudflare.com}"
run_udp_probe() {
  errf="$(mktemp)"
  set +e
  timeout "$WALL_TIMEOUT" dig @"${DIG_TARGET}" -p "${DIG_PORT}" "${DIG_NAME}" +time=3 +tries=1 +norecurse >"$errf" 2>&1
  rc=$?
  set -e
  out="$(cat "$errf" 2>/dev/null || true)"
  rm -f "$errf"
  out="$(printf '%s\n' "$out" | grep -v 'CONNECT_IP_OBS')"
  echo "$out"
  return "$rc"
}

run_one() {
  extra="$1"
  errf="$(mktemp)"
  set +e
  timeout "$WALL_TIMEOUT" iperf3 -c "${IPERF_HOST}" -p "${IPERF_PORT}" \
    --connect-timeout "${CONNECT_TIMEOUT_MS}" -t "${DURATION}" -P 1 ${extra} >"$errf" 2>&1
  rc=$?
  set -e
  out="$(cat "$errf" 2>/dev/null || true)"
  rm -f "$errf"
  # Sidecar logs (sing-box CONNECT_IP_OBS JSON) often interleave with iperf stderr; strip for parsing and RESULT_ERR.
  out_f="$(printf '%s\n' "$out" | grep -v 'CONNECT_IP_OBS')"
  if [ "$rc" -ne 0 ]; then
    msg=$(printf '%s\n' "$out_f" | grep -E '^iperf3:|^error |unable to receive|refused|timed out|timeout' | tail -1)
    if [ -z "$msg" ]; then
      msg=$(printf '%s\n' "$out_f" | grep -v '^[0-9][0-9][0-9][0-9]/[0-9][0-9]/[0-9]' | tail -1)
    fi
    if [ -z "$msg" ]; then msg=$(printf '%s\n' "$out_f" | tail -1); fi
    echo "RESULT_ERR=$(printf '%s' "$msg" | tr '\n\r' ' ' | head -c 180)"
    return 1
  fi
  rate="$(parse_mbit "$out_f")"
  if [ -z "$rate" ]; then
    echo "RESULT_ERR=iperf summary not found"
    return 1
  fi
  echo "$rate"
  return 0
}

if ! UP="$(run_one "")"; then
  echo "$UP"
  echo "RESULT_OK=0"
  exit 0
fi
if ! DOWN="$(run_one "-R")"; then
  echo "$DOWN"
  echo "RESULT_OK=0"
  echo "RESULT_UP_MBIT=${UP}"
  exit 0
fi

if [ "${BENCH_UDP_PROBE:-0}" = "1" ] && [ "$BENCH_VIA" = "tun" ]; then
  udp_out="$(run_udp_probe)" || true
  qtime="$(printf '%s\n' "$udp_out" | awk '/;; Query time:/ { print $4; exit }')"
  # UDP до того же хоста/порту, что TCP iperf: без DNS-сервера приходит ICMP/refused — dig сообщает "communications error ... connection refused".
  # Чистый timeout без refused — вероятный разрыв CONNECT-UDP (или фильтрация).
  if printf '%s\n' "$udp_out" | grep -qi 'connection refused'; then
    echo "RESULT_UDP_OK=1"
    if [ -n "$qtime" ]; then
      echo "RESULT_UDP_MBIT=${qtime}ms_dig"
    else
      echo "RESULT_UDP_MBIT=udp_delivered_icmp_refused"
    fi
  elif [ -n "$qtime" ]; then
    echo "RESULT_UDP_OK=1"
    echo "RESULT_UDP_MBIT=${qtime}ms_dig"
  elif printf '%s\n' "$udp_out" | grep -qiE 'timed out'; then
    echo "RESULT_UDP_OK=0"
    echo "RESULT_UDP_ERR=$(printf '%s\n' "$udp_out" | tr '\n\r' ' ' | head -c 280)"
  else
    echo "RESULT_UDP_OK=0"
    echo "RESULT_UDP_ERR=$(printf '%s\n' "$udp_out" | tr '\n\r' ' ' | head -c 280)"
  fi
else
  echo "RESULT_UDP_SKIPPED=1"
fi

echo "RESULT_OK=1"
echo "RESULT_UP_MBIT=${UP}"
echo "RESULT_DOWN_MBIT=${DOWN}"
