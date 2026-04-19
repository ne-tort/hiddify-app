#!/bin/sh
# Опционально: перед certonly проверить, что сертификат для домена уже есть в томе.
# Использование: из каталога vendor/s-ui с поднятым томом letsencrypt_data:
#   docker compose -f docker-compose.stand.yml run --rm certbot certificates | grep -q work.ai-qwerty.ru && exit 0
# Либо локально на хосте, если том смонтирован.
set -e
DOMAIN="${1:-work.ai-qwerty.ru}"
LE_DIR="${2:-/etc/letsencrypt/live/${DOMAIN}}"
if [ -f "${LE_DIR}/fullchain.pem" ] && [ -f "${LE_DIR}/privkey.pem" ]; then
  echo "certbot-init: certs already present under ${LE_DIR}, skip certonly"
  exit 0
fi
echo "certbot-init: no valid cert pair at ${LE_DIR}; run certonly (see deploy/TLS.md)"
exit 1
