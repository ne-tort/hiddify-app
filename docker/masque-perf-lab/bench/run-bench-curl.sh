#!/bin/sh
set -eu
# Пропускная способность через SOCKS (TCP): большой ответ Cloudflare (без iperf на целевом хосте).
SOCKS_PORT="${SOCKS_PORT:-1080}"
WAIT_SEC="${BENCH_WAIT_SOCKS_SEC:-2}"
URL="${CURL_BENCH_URL:-https://speed.cloudflare.com/__down?bytes=80000000}"

echo "[bench-curl] wait ${WAIT_SEC}s SOCKS 127.0.0.1:${SOCKS_PORT}"
sleep "$WAIT_SEC"

echo "[bench-curl] GET $URL"
curl -fsS -o /dev/null -w "curl_avg_download_bytes_per_sec=%{speed_download}\n" --socks5-hostname "127.0.0.1:${SOCKS_PORT}" "$URL"
