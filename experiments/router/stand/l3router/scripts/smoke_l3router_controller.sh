#!/usr/bin/env bash
# Online smoke: метрики l3router на clash API (запускать с машины, имеющей доступ к controller).
set -euo pipefail

: "${L3ROUTER_CONTROLLER_URL:?example: export L3ROUTER_CONTROLLER_URL=http://VPS_IP:9090}"
: "${L3ROUTER_CONTROLLER_SECRET:=replace-me}"
: "${L3ROUTER_PROXY_TAG:=l3router}"

need() { command -v "$1" >/dev/null 2>&1 || { echo "need $1" >&2; exit 1; }; }
need curl
need jq

auth=(-H "Authorization: Bearer ${L3ROUTER_CONTROLLER_SECRET}")

metrics_json="$(curl -fsS "${auth[@]}" "${L3ROUTER_CONTROLLER_URL}/proxies/${L3ROUTER_PROXY_TAG}/metrics")"

upsert="$(echo "$metrics_json" | jq -r '.metrics.ControlUpsertOK // 0')"
remove="$(echo "$metrics_json" | jq -r '.metrics.ControlRemoveOK // 0')"

if [[ "${upsert}" != "0" || "${remove}" != "0" ]]; then
  echo "static-only violated: ControlUpsertOK=${upsert} ControlRemoveOK=${remove}" >&2
  exit 1
fi

echo "[smoke_l3router_controller] ok: no runtime route API mutations (upsert=${upsert} remove=${remove})"
