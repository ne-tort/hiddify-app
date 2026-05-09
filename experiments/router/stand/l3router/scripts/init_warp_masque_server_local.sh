#!/usr/bin/env bash
# Создаёт configs/warp-masque-live.server.local.json из шаблона, если ещё нет.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SRC="$ROOT/configs/warp-masque-live.server.docker.json"
DST="$ROOT/configs/warp-masque-live.server.local.json"
if [[ -f "$DST" ]]; then
  echo "already exists: $DST"
  exit 0
fi
cp "$SRC" "$DST"
echo "created $DST — заполните profile.* и при необходимости WARP_MASQUE_WARP_CACHE"
