#!/bin/sh
# Запускать на VPS из каталога warp-masque-stand: sh scripts/on_vps_rewrite_local.sh
set -eu
cd "$(dirname "$0")/.." || exit 1
CFG="configs"
cd "$CFG" || exit 1
PROF=$(jq -c '.endpoints[0].profile' warp-masque-live.server.local.json)
jq --argjson p "$PROF" '.endpoints[0].profile = $p' warp-masque-live.server.docker.json > warp-masque-live.server.local.json.tmp
mv warp-masque-live.server.local.json.tmp warp-masque-live.server.local.json
echo "OK: rewrote warp-masque-live.server.local.json from docker template + kept profile"
