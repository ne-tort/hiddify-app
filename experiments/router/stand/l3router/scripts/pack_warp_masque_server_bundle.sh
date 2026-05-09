#!/usr/bin/env bash
# Собирает архив каталога l3router (по умолчанию без бинаря).
# Включить бинарь в архив для scp/rsync деплоя: WITH_SINGBOX=1 перед вызовом.
set -euo pipefail
STAND_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="${1:-${STAND_ROOT}/../warp-masque-stand-bundle.tgz}"
EXCLUDES=(
  --exclude='./__pycache__'
  --exclude='./.pytest_cache'
  --exclude='*.pyc'
  --exclude='./runtime/*.json'
  --exclude='./configs/*.local.json'
  --exclude='./.secrets'
  --exclude='./.git'
)
if [[ "${WITH_SINGBOX:-0}" != "1" ]]; then
  EXCLUDES+=(--exclude='./artifacts/sing-box-linux-amd64')
fi
(cd "$STAND_ROOT" && tar czvf "$OUT" "${EXCLUDES[@]}" .)
if [[ "${WITH_SINGBOX:-0}" == "1" ]]; then
  if [[ ! -f "${STAND_ROOT}/artifacts/sing-box-linux-amd64" ]]; then
    echo "WARN: WITH_SINGBOX=1 но нет файла artifacts/sing-box-linux-amd64" >&2
  fi
fi
echo "Created: $OUT"
