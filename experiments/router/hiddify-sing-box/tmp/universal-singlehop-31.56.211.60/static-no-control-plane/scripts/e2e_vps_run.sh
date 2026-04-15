#!/usr/bin/env bash
# Полный E2E на VPS: поднять два клиента (REALITY), проверить ICMP L3 через l3router.
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
STAND_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
cd "${STAND_ROOT}"

COMPOSE_FILE="docker-compose.l3router-e2e-reality.yml"

echo "[e2e] stand root: ${STAND_ROOT}"

if ! docker image inspect sing-box-l3router:local >/dev/null 2>&1; then
  echo "[e2e] ERROR: missing image sing-box-l3router:local (build hub Dockerfile first)" >&2
  exit 1
fi

echo "[e2e] stopping old e2e containers if any"
docker compose -f "${COMPOSE_FILE}" down 2>/dev/null || true

echo "[e2e] starting client1 + client2 (host network)..."
docker compose -f "${COMPOSE_FILE}" up -d

echo "[e2e] waiting for tunnels (15s)..."
sleep 15

echo "[e2e] ping client2 (10.0.0.3) from client1 container..."
if docker exec l3router-e2e-client1 ping -c 5 -W 2 10.0.0.3; then
  echo "[e2e] OK: ICMP reached peer through l3router"
else
  echo "[e2e] FAIL: ping" >&2
  docker logs --tail 40 l3router-e2e-client1 2>&1 || true
  docker logs --tail 40 l3router-e2e-client2 2>&1 || true
  exit 1
fi

echo "[e2e] hub l3router metrics (from singbox-wg-hub-server)..."
docker exec singbox-wg-hub-server wget -qO- "http://127.0.0.1:9090/proxies/l3router/metrics" | head -c 600
echo ""

if command -v smbd >/dev/null 2>&1 && command -v smbclient >/dev/null 2>&1; then
  echo "[e2e] SMB 100 MiB (smbd on peer + smbclient via tun), timing + Mbit/s → runtime/smb_100mb_e2e_latest.json"
  bash "${SCRIPT_DIR}/smb_transfer_100mb_e2e.sh"
else
  echo "[e2e] SKIP smb 100 MiB: install samba + smbclient on this host, then re-run:"
  echo "       apt-get update && apt-get install -y samba smbclient"
  echo "       sudo bash ${SCRIPT_DIR}/smb_transfer_100mb_e2e.sh"
fi

echo "[e2e] done."
