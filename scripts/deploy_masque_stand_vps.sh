#!/usr/bin/env bash
# Развёртывание лабораторного MASQUE-сервера (sing-box) на VPS: самоподписанный TLS, server_token.
# Обязательно: MASQUE_PUBLIC_IP — тот же хост:порт, что клиент использует в SNI / :authority (обычно публичный IP VPS или DNS).
# Опционально: SING_BOX_STAGED_PATH — локальный путь на сервере к новому бинарнику sing-box (загрузите в /tmp заранее, см. scripts/push_masque_stand.ps1).
set -euo pipefail

: "${MASQUE_PUBLIC_IP:?Задайте MASQUE_PUBLIC_IP (публичный IP или DNS клиента)}"

TOKEN="$(openssl rand -hex 16)"
MASQUE_PORT="${MASQUE_PORT:-18443}"
BASE=/etc/sing-box/masque-stand

echo "=== stop host sing-box ==="
pkill -f '/usr/local/bin/sing-box run' 2>/dev/null || true
sleep 1
if pgrep -f '/usr/local/bin/sing-box run' >/dev/null; then
  pkill -9 -f '/usr/local/bin/sing-box run' 2>/dev/null || true
fi

if [ -n "${SING_BOX_STAGED_PATH:-}" ] && [ -f "$SING_BOX_STAGED_PATH" ]; then
  echo "=== install sing-box from SING_BOX_STAGED_PATH ==="
  install -m755 "$SING_BOX_STAGED_PATH" /usr/local/bin/sing-box
elif ! command -v sing-box >/dev/null 2>&1; then
  echo "Ошибка: нет sing-box в PATH. Загрузите бинарник и задайте SING_BOX_STAGED_PATH=/path/to/sing-box" >&2
  exit 1
fi

echo "=== stop docker warp-masque stand ==="
if command -v docker >/dev/null 2>&1; then
  docker stop sing-box-warp-masque-live-server 2>/dev/null || true
fi

echo "=== stop usque if any ==="
pkill -9 usque 2>/dev/null || true

mkdir -p "$BASE"
cd "$BASE"

openssl req -x509 -newkey rsa:2048 -sha256 -days 3650 -nodes \
  -keyout key.pem -out cert.pem \
  -subj "/CN=${MASQUE_PUBLIC_IP}" \
  -addext "subjectAltName=IP:${MASQUE_PUBLIC_IP}"

umask 077
printf '%s' "$TOKEN" > server_token.txt
chmod 600 server_token.txt key.pem

cat > sing-box.json <<JSON
{
  "log": { "level": "warn", "timestamp": true },
  "endpoints": [
    {
      "type": "masque",
      "tag": "masque-in",
      "mode": "server",
      "listen": "::",
      "listen_port": ${MASQUE_PORT},
      "certificate": "${BASE}/cert.pem",
      "key": "${BASE}/key.pem",
      "server_token": "${TOKEN}",
      "template_udp": "https://${MASQUE_PUBLIC_IP}:${MASQUE_PORT}/masque/udp/{target_host}/{target_port}",
      "template_ip": "https://${MASQUE_PUBLIC_IP}:${MASQUE_PORT}/masque/ip",
      "template_tcp": "https://${MASQUE_PUBLIC_IP}:${MASQUE_PORT}/masque/tcp/{target_host}/{target_port}"
    }
  ],
  "outbounds": [
    { "type": "direct", "tag": "direct" }
  ],
  "route": {
    "auto_detect_interface": true,
    "final": "direct"
  }
}
JSON

cat > /etc/systemd/system/sing-box-masque-stand.service <<UNIT
[Unit]
Description=sing-box MASQUE server (lab stand, self-signed)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/sing-box run -c ${BASE}/sing-box.json
Restart=on-failure
RestartSec=3
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable sing-box-masque-stand.service
systemctl restart sing-box-masque-stand.service
sleep 2
systemctl is-active sing-box-masque-stand.service
ss -ulnp | grep -E ":${MASQUE_PORT}\\b" || true
ss -tlnp | grep -E ":${MASQUE_PORT}\\b" || true

echo "OK: MASQUE stand on port ${MASQUE_PORT}"
echo "SERVER_TOKEN=${TOKEN}"
