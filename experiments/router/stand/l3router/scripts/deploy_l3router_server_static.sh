#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
CONFIG_PATH="${ROOT_DIR}/configs/server.l3router.static.json"

: "${L3ROUTER_SERVER_HOST:?set L3ROUTER_SERVER_HOST}"
: "${L3ROUTER_SERVER_USER:=root}"
: "${L3ROUTER_SERVER_CONFIG_PATH:=/etc/sing-box/config.json}"
: "${L3ROUTER_SERVER_SERVICE:=sing-box}"

if [[ ! -f "${CONFIG_PATH}" ]]; then
  echo "missing config: ${CONFIG_PATH}" >&2
  exit 1
fi

echo "[static-only] deploying ${CONFIG_PATH} -> ${L3ROUTER_SERVER_USER}@${L3ROUTER_SERVER_HOST}:${L3ROUTER_SERVER_CONFIG_PATH}"
scp "${CONFIG_PATH}" "${L3ROUTER_SERVER_USER}@${L3ROUTER_SERVER_HOST}:${L3ROUTER_SERVER_CONFIG_PATH}"
ssh "${L3ROUTER_SERVER_USER}@${L3ROUTER_SERVER_HOST}" "systemctl restart ${L3ROUTER_SERVER_SERVICE} && systemctl --no-pager --full status ${L3ROUTER_SERVER_SERVICE} | sed -n '1,20p'"

echo "[static-only] deploy complete (no route API calls used)"
