#!/usr/bin/env bash
set -euo pipefail

ROOT="${WARP_MASQUE_STAND:-$HOME/warp-masque-stand}"
CFG="${WARP_MASQUE_LOCAL_JSON:-$ROOT/configs/warp-masque-live.server.local.json}"

if [[ ! -f "$CFG" ]]; then
  echo "missing config: $CFG" >&2
  exit 1
fi

DIR="$(dirname "$CFG")"
BASE="$(basename "$CFG" .json)"
H3="$DIR/${BASE}.h3.json"
H2="$DIR/${BASE}.h2.json"
AUTO="$DIR/${BASE}.auto.json"

jq '.endpoints[0].http_layer="h3" | .endpoints[0].http_layer_fallback=false' "$CFG" >"$H3"
jq '.endpoints[0].http_layer="h2" | .endpoints[0].http_layer_fallback=false' "$CFG" >"$H2"
jq '.endpoints[0].http_layer="auto" | .endpoints[0].http_layer_fallback=true' "$CFG" >"$AUTO"

echo "written:"
echo "  $H3"
echo "  $H2"
echo "  $AUTO"
echo "Use with compose env: WARP_MASQUE_CONFIG=<path-to-variant>"
