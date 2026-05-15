#!/usr/bin/env bash
# Воспроизводимая настройка iperf3 для MASQUE perf lab (два порта — два независимых сценария).
# Запуск на целевой машине (Ubuntu/Debian) от root: bash setup_masque_iperf_benchmark_server.sh
# Или с рабочей станции: ssh 163.5.180.181 'bash -s' < scripts/remote/setup_masque_iperf_benchmark_server.sh
set -euo pipefail

if [[ "${EUID:-0}" -ne 0 ]]; then
  echo "Run as root on the server (e.g. ssh root@HOST 'bash -s' < this/script)." >&2
  exit 1
fi

export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install -y -qq iperf3

write_unit() {
  local port="$1"
  cat >"/etc/systemd/system/iperf3-masque-${port}.service" <<UNIT
[Unit]
Description=iperf3 server for MASQUE perf lab (port ${port})
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/bin/iperf3 -s -p ${port}
Restart=always
RestartSec=2

[Install]
WantedBy=multi-user.target
UNIT
}

write_unit 5201
write_unit 5202
systemctl daemon-reload
systemctl enable --now iperf3-masque-5201.service iperf3-masque-5202.service
systemctl is-active iperf3-masque-5201.service iperf3-masque-5202.service
ss -tlnp | grep -E ':5201|:5202' || true
echo "OK: iperf3 TCP listen on 5201 and 5202."
echo "Note: iperf3 -s uses UDP on the same port during tests; ss -ulnp may show no LISTEN until a UDP client runs."
echo "UDP smoke (localhost -> same ports):"
iperf3 -u -c 127.0.0.1 -p 5201 -t 1 -b 768k -l 1400 --connect-timeout 3000 >/tmp/masque-iperf-udp5201.txt 2>&1 \
  || { cat /tmp/masque-iperf-udp5201.txt; exit 1; }
grep -q '(100%).*receiver' /tmp/masque-iperf-udp5201.txt && { cat /tmp/masque-iperf-udp5201.txt; exit 1; }
iperf3 -u -c 127.0.0.1 -p 5202 -t 1 -b 768k -l 1400 --connect-timeout 3000 >/tmp/masque-iperf-udp5202.txt 2>&1 \
  || { cat /tmp/masque-iperf-udp5202.txt; exit 1; }
grep -q '(100%).*receiver' /tmp/masque-iperf-udp5202.txt && { cat /tmp/masque-iperf-udp5202.txt; exit 1; }
rm -f /tmp/masque-iperf-udp5201.txt /tmp/masque-iperf-udp5202.txt
echo "OK: UDP path on iperf3 ports verified."
echo "MASQUE bench UDP-probe: allow UDP (and TCP) to these ports on the public interface (ufw: ufw allow 5201,5202/tcp; ufw allow 5201,5202/udp)."
