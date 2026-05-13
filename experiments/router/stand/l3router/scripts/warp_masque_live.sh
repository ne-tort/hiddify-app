#!/usr/bin/env sh
# Helper for docker-compose.warp-masque-live.yml (no secrets in this script).
set -eu
ROOT=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
COMPOSE="$ROOT/docker-compose.warp-masque-live.yml"
CFG="${WARP_MASQUE_CONFIG:-$ROOT/configs/warp-masque-live.json}"
export WARP_MASQUE_CONFIG="$CFG"

usage() {
  echo "usage: $0 up|down|logs|smoke|check" >&2
  echo "  env: WARP_MASQUE_CONFIG (default $ROOT/configs/warp-masque-live.json)" >&2
  exit 2
}

cmd="${1:-}"
case "$cmd" in
  up)
    docker compose -f "$COMPOSE" --project-directory "$ROOT" build
    docker compose -f "$COMPOSE" --project-directory "$ROOT" up -d
    ;;
  down)
    docker compose -f "$COMPOSE" --project-directory "$ROOT" down
    ;;
  logs)
    docker compose -f "$COMPOSE" --project-directory "$ROOT" logs -f sing-box-warp-masque-live
    ;;
  smoke)
    docker exec sing-box-warp-masque-live curl -sS --max-time 25 \
      https://1.1.1.1/cdn-cgi/trace
    ;;
  check)
    if command -v sing-box >/dev/null 2>&1; then
      sing-box check -c "$CFG"
    else
      echo "sing-box not in PATH; skip check (copy config to a host with sing-box)" >&2
      exit 0
    fi
    ;;
  *)
    usage
    ;;
esac
