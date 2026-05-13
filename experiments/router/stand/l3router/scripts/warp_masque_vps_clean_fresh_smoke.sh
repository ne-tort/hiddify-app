#!/usr/bin/env bash
# «Чистый» прогон warp_masque на VPS в Docker: без персистентного state/cache вне явного scratch-тома,
# те же smoke-команды, что в корневом AGENTS.md (TUN, литерал 1.1.1.1).
#
# Использование (на VPS, из каталога стенда, например ~/warp-masque-stand):
#   bash scripts/warp_masque_vps_clean_fresh_smoke.sh h2
#   bash scripts/warp_masque_vps_clean_fresh_smoke.sh h3
# Опции:
#   WARP_STAND=...   каталог стенда (по умолчанию текущий каталог или $HOME/warp-masque-stand)
#   SMOKE_SLEEP=28   пауза после up перед curl
#   DO_BUILD=1       если задано — выполнить docker compose build перед up
#
set -euo pipefail

LAYER="${1:-}"
if [[ "$LAYER" != "h2" && "$LAYER" != "h3" ]]; then
  echo "usage: $0 h2|h3" >&2
  exit 2
fi

if [[ -n "${WARP_STAND:-}" ]]; then
  cd "$WARP_STAND"
elif [[ -f docker-compose.warp-masque-live.server.yml ]]; then
  :
elif [[ -f "$HOME/warp-masque-stand/docker-compose.warp-masque-live.server.yml" ]]; then
  cd "$HOME/warp-masque-stand"
else
  echo "cannot find stand: set WARP_STAND or run from ~/warp-masque-stand" >&2
  exit 2
fi

STAND="$(pwd -P)"
COMPOSE_MAIN="docker-compose.warp-masque-live.server.yml"
COMPOSE_CLEAN="docker-compose.warp-masque-live.server.clean-smoke.override.yml"
if [[ ! -f "$COMPOSE_MAIN" ]]; then
  echo "missing $COMPOSE_MAIN in $STAND" >&2
  exit 2
fi
if [[ ! -f "$COMPOSE_CLEAN" ]]; then
  echo "missing $COMPOSE_CLEAN (добавьте файл из репозитория l3router)" >&2
  exit 2
fi

SCRATCH="${STAND}/.clean-smoke-scratch-$(date +%s)-$$"
rm -rf "$SCRATCH"
mkdir -p "$SCRATCH"
chmod 700 "$SCRATCH"
export WARP_MASQUE_SCRATCH_DIR="$SCRATCH"

CFG_OUT="${STAND}/.clean-smoke-config-$$.${LAYER}.json"
if [[ "$LAYER" == "h2" ]]; then
  BASE="${STAND}/configs/warp-masque-live.server.docker.h2.json"
else
  BASE="${STAND}/configs/warp-masque-live.server.docker.json"
fi
if [[ ! -f "$BASE" ]]; then
  echo "missing base template: $BASE" >&2
  exit 2
fi

export BASE CFG_OUT LAYER
python3 <<'PY'
import json, pathlib, os
base = pathlib.Path(os.environ["BASE"])
out = pathlib.Path(os.environ["CFG_OUT"])
layer = os.environ["LAYER"]
cfg = json.loads(base.read_text(encoding="utf-8"))
for ep in cfg.get("endpoints", []):
    if ep.get("type") == "warp_masque":
        ep["http_layer"] = layer
        ep["http_layer_fallback"] = False
        p = ep.get("profile") or {}
        # «Как с нуля»: только consumer API, без ключей из файла (если в шаблоне пусто — ок).
        for k in ("license", "private_key", "auth_token", "id", "masque_ecdsa_private_key"):
            if k in p:
                p[k] = ""
        p["recreate"] = False
        ep["profile"] = p
        break
else:
    raise SystemExit("no warp_masque endpoint")
out.write_text(json.dumps(cfg, indent=2) + "\n", encoding="utf-8")
print("wrote", out)
PY
export WARP_MASQUE_CONFIG="$CFG_OUT"

CONTAINER="${CONTAINER:-sing-box-warp-masque-live-server}"
# После холодного enroll / «no assigned prefixes» netstack может подняться позже первого curl.
SMOKE_SLEEP="${SMOKE_SLEEP:-40}"

echo "=== stand: $STAND"
echo "=== scratch (host, empty before up): $SCRATCH"
echo "=== config: $WARP_MASQUE_CONFIG (layer=$LAYER, fallback=false)"
ls -la "$SCRATCH"

docker compose -f "$COMPOSE_MAIN" -f "$COMPOSE_CLEAN" down --remove-orphans || true
if [[ -n "${DO_BUILD:-}" ]]; then
  docker compose -f "$COMPOSE_MAIN" -f "$COMPOSE_CLEAN" build
fi
docker compose -f "$COMPOSE_MAIN" -f "$COMPOSE_CLEAN" up -d --force-recreate

echo "=== waiting ${SMOKE_SLEEP}s for bootstrap..."
sleep "$SMOKE_SLEEP"

echo "=== TUN route (AGENTS):"
docker exec "$CONTAINER" ip route get 1.1.1.1 || true

echo "=== TUN trace (1st attempt, max 45s; контейнер не перезапускается):"
set +e
TRACE="$(docker exec "$CONTAINER" curl -sS --max-time 45 "https://1.1.1.1/cdn-cgi/trace" 2>&1)"
RC=$?
set -e
echo "$TRACE" | head -n 22
if echo "$TRACE" | grep -q 'warp=on'; then
  echo "RESULT: PASS (warp=on)"
else
  echo "RESULT: 1st curl inconclusive (exit=$RC); delayed retry без restart (+12s)…"
  sleep 12
  set +e
  TRACE2="$(docker exec "$CONTAINER" curl -sS --max-time 45 "https://1.1.1.1/cdn-cgi/trace" 2>&1)"
  RC2=$?
  set -e
  echo "$TRACE2" | head -n 22
  if echo "$TRACE2" | grep -q 'warp=on'; then
    echo "RESULT: PASS on 2nd attempt (warp=on)"
  else
    echo "RESULT: FAIL after 2nd attempt (curl exit=$RC2)"
  fi
fi

echo "=== scratch after run (device state + dataplane cache only here):"
ls -la "$SCRATCH" || true

echo "=== filtered logs (tail):"
docker logs "$CONTAINER" 2>&1 | grep -E 'masque_http_layer_|CONNECT status=|runtime start failed class=|open_ip_session|warp_masque control' | tail -n 40 || true

echo "=== done; compose still UP. Down: docker compose -f $COMPOSE_MAIN -f $COMPOSE_CLEAN down"
echo "=== remove scratch: rm -rf $SCRATCH ; rm -f $CFG_OUT"
