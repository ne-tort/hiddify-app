#!/usr/bin/env bash
# Проверка L3 между клиентами через хаб: ICMP до peer-подсети (после поднятия обоих клиентов).
# Пример: с хоста, где крутятся оба контейнера (host network), с машины client-a к IP client-b:
#   ./e2e_ping_peer.sh 10.10.2.2
set -euo pipefail

PEER_IP="${1:?usage: e2e_ping_peer.sh <peer-tun-ip e.g. 10.10.2.2>}"

ping -c 4 -W 3 "${PEER_IP}"

echo "[e2e_ping_peer] ok: ${PEER_IP} responded"
