#!/usr/bin/env bash
# Toggle warp_masque http_layer on VPS stand (no secrets in script).
set -eu
LAYER="${1:?usage: stand_toggle_http_layer.sh h3|h2}"
CFG="${WARP_MASQUE_LOCAL_JSON:-/root/warp-masque-stand/configs/warp-masque-live.server.local.json}"
ROOT="${WARP_MASQUE_STAND:-/root/warp-masque-stand}"
COMPOSE="$ROOT/docker-compose.warp-masque-live.server.yml"
case "$LAYER" in
  h2|h3) ;;
  *) echo "layer must be h2 or h3" >&2; exit 2 ;;
esac
if [[ ! -f "$CFG" ]]; then echo "missing $CFG" >&2; exit 1; fi
tmp="$(mktemp)"
jq --arg x "$LAYER" '.endpoints[0].http_layer = $x' "$CFG" >"$tmp"
mv "$tmp" "$CFG"
cd "$ROOT"
docker compose -f "$COMPOSE" up -d --force-recreate
