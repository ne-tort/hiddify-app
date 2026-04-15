#!/usr/bin/env bash
# Локальный набор проверок стенда (без обязательного VPS).
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
cd "${SCRIPT_DIR}"

echo "== smoke_static_config =="
bash ./smoke_static_config.sh

echo "== local_transfer_100mb (локальный dd+cp+sha256, НЕ SMB и не l3router) =="
bash ./smb_transfer_100mb_static.sh
echo "== (опционально) реальный SMB через туннели: RUN_SMB_E2E=1 sudo -E bash ./smb_transfer_100mb_e2e.sh =="
if [[ "${RUN_SMB_E2E:-}" == "1" ]]; then
  sudo -E bash ./smb_transfer_100mb_e2e.sh
fi

if [[ -n "${L3ROUTER_CONTROLLER_URL:-}" ]]; then
  echo "== smoke_l3router_controller =="
  bash ./smoke_l3router_controller.sh
else
  echo "== skip smoke_l3router_controller (set L3ROUTER_CONTROLLER_URL) =="
fi

echo "[run_stand_tests] all steps completed"
