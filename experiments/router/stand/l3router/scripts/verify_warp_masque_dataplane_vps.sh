#!/usr/bin/env bash
# После деплоя sing-box с авто-подбором портов MASQUE проверить на VPS (без секретов в выводе):
#   SSH_USERHOST=root@YOUR_VPS bash scripts/verify_warp_masque_dataplane_vps.sh
set -euo pipefail
USERHOST="${SSH_USERHOST:-root@163.5.180.181}"
NAME="${WARP_DOCKER_NAME:-sing-box-warp-masque-live-server}"
echo "Checking ${USERHOST} container ${NAME}..."
ssh -o BatchMode=yes "${USERHOST}" "docker logs --tail 400 '${NAME}' 2>&1" \
  | grep -E 'warp_masque (dataplane UDP candidates|dataplane try port=|runtime start failed class=|control profile endpoint)' \
  || { echo "No matching log lines (rebuild image with new core; or increase --tail)."; exit 1; }
echo "OK: found warp_masque dataplane-related log lines."
