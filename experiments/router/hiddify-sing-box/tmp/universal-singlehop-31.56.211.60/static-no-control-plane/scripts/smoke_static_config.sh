#!/usr/bin/env bash
# Offline проверка: конфиги на месте, static routes описаны, без вызова API.
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
CFG="${ROOT_DIR}/configs"

need() { command -v "$1" >/dev/null 2>&1 || { echo "need $1" >&2; exit 1; }; }

need jq

server="${CFG}/server.l3router.static.json"
[[ -f "$server" ]] || { echo "missing $server" >&2; exit 1; }

routes="$(jq '.endpoints[] | select(.type=="l3router") | .routes | length' "$server")"
if [[ "${routes}" -lt 2 ]]; then
  echo "expected >= 2 l3router routes, got ${routes}" >&2
  exit 1
fi

overlay="$(jq -r '.endpoints[] | select(.type=="l3router") | .overlay_destination // empty' "$server")"
if [[ -z "${overlay}" ]]; then
  echo "missing overlay_destination on l3router endpoint" >&2
  exit 1
fi

for c in client-a.static.json client-b.static.json; do
  f="${CFG}/${c}"
  [[ -f "$f" ]] || { echo "missing $f" >&2; exit 1; }
  jq -e '.outbounds[0].type' "$f" >/dev/null
done

echo "[smoke_static_config] ok: server routes=${routes}, overlay=${overlay}"
