#!/usr/bin/env bash
set -euo pipefail

ROOT="${WARP_MASQUE_STAND:-$HOME/warp-masque-stand}"
CFG="${WARP_MASQUE_LOCAL_JSON:-$ROOT/configs/warp-masque-live.server.local.json}"
COMPOSE="$ROOT/docker-compose.warp-masque-live.server.yml"
CONTAINER="${WARP_CONTAINER:-sing-box-warp-masque-live-server}"

if [[ ! -f "$CFG" ]]; then
  echo "missing config: $CFG" >&2
  exit 1
fi

SESSION_BAK="$CFG.bak.matrix_session"
cp -f "$CFG" "$SESSION_BAK"

run_case() {
  local name="$1"
  local layer="$2"
  local fallback="$3"
  local tmp
  tmp="$(mktemp)"
  jq --arg l "$layer" --argjson f "$fallback" '.endpoints[0].http_layer=$l | .endpoints[0].http_layer_fallback=$f' "$SESSION_BAK" >"$tmp"
  mv "$tmp" "$CFG"

  (cd "$ROOT" && docker compose -f "$COMPOSE" up -d --force-recreate >/dev/null)
  sleep 28

  local route s1 s2 logs
  route="$(docker exec "$CONTAINER" ip route get 1.1.1.1 2>/dev/null | head -1 || true)"
  s1="$(docker exec "$CONTAINER" curl -sS --max-time 45 https://1.1.1.1/cdn-cgi/trace 2>/dev/null | grep -E 'warp=|http=' | tr '\n' ' ' || true)"
  s2="$(docker exec "$CONTAINER" curl -sS --max-time 45 https://1.1.1.1/cdn-cgi/trace 2>/dev/null | grep -E 'warp=|http=' | tr '\n' ' ' || true)"
  logs="$(docker logs "$CONTAINER" 2>&1 | grep -E 'runtime start failed class=|peer TLS public key|cloudflare api unauthorized|CONNECT status=|masque_http_layer_chosen|masque_http_layer_fallback' | tail -n 8 | tr '\n' '|' || true)"

  echo "CASE=$name layer=$layer fallback=$fallback"
  echo "  route=$route"
  echo "  smoke1=${s1:-<none>}"
  echo "  smoke2=${s2:-<none>}"
  echo "  logs=${logs:-<none>}"
}

run_case "h3_only" "h3" "false"
run_case "h2_only" "h2" "false"
run_case "auto_h3h2" "auto" "true"
run_case "auto_h3h2_repeat" "auto" "true"

cp -f "$SESSION_BAK" "$CFG"
(cd "$ROOT" && docker compose -f "$COMPOSE" up -d --force-recreate >/dev/null)
echo "DONE_RESTORED"
