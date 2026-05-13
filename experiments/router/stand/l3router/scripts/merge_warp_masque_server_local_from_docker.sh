#!/usr/bin/env bash
# Обновляет в warp-masque-live.server.local.json всё из warp-masque-live.server.docker.json,
# кроме endpoints[0].profile (и необязательных полей профиля), чтобы подтянуть listen SOCKS, http_layer и т.д.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SRC="$ROOT/configs/warp-masque-live.server.docker.json"
DST="$ROOT/configs/warp-masque-live.server.local.json"
if [[ ! -f "$SRC" ]]; then
  echo "missing $SRC" >&2
  exit 1
fi
if [[ ! -f "$DST" ]]; then
  echo "missing $DST — run scripts/init_warp_masque_server_local.sh" >&2
  exit 1
fi
if ! command -v jq >/dev/null 2>&1; then
  echo "jq required" >&2
  exit 1
fi
prof="$(jq -c '.endpoints[0].profile' "$DST")"
tmp="$(mktemp)"
jq --argjson p "$prof" '.endpoints[0].profile = $p' "$SRC" >"$tmp"
mv "$tmp" "$DST"
echo "OK: merged docker template into $DST (profile preserved from previous local)"
