#!/bin/sh
set -e

# Host network: до появления wghub — снизить rp_filter у будущих интерфейсов (иначе peer↔peer на хабе может не форвардиться).
sysctl -w net.ipv4.conf.default.rp_filter=0 2>/dev/null || true
# Иначе при форвардинге между пирами на wghub ядро шлёт ICMP redirects и клиенты теряют путь к overlay peer (SMB/ping).
sysctl -w net.ipv4.conf.all.send_redirects=0 2>/dev/null || true
sysctl -w net.ipv4.conf.default.send_redirects=0 2>/dev/null || true

# Сервер: ядро может слушать UDP 51820 на всех интерфейсах; Hy2 заворачивает трафик на 127.0.0.1.
# DROP_WG_WAN_UDP=1: INPUT UDP :51820 не с lo — DROP. Внимание: ломает kernel WG (remote_wg_split_stack), т.к. peer шлёт UDP на :51820 с WAN.
if command -v iptables >/dev/null 2>&1; then
  if [ "${DROP_WG_WAN_UDP:-0}" = "1" ]; then
    iptables -C INPUT -p udp --dport 51820 ! -i lo -j DROP 2>/dev/null \
      || iptables -I INPUT -p udp --dport 51820 ! -i lo -j DROP 2>/dev/null \
      || true
    if command -v ip6tables >/dev/null 2>&1; then
      ip6tables -C INPUT -p udp --dport 51820 ! -i lo -j DROP 2>/dev/null \
        || ip6tables -I INPUT -p udp --dport 51820 ! -i lo -j DROP 2>/dev/null \
        || true
    fi
  else
    while iptables -D INPUT -p udp --dport 51820 ! -i lo -j DROP 2>/dev/null; do :; done
    if command -v ip6tables >/dev/null 2>&1; then
      while ip6tables -D INPUT -p udp --dport 51820 ! -i lo -j DROP 2>/dev/null; do :; done
    fi
  fi
fi

if [ "${START_SMB_SERVER:-0}" = "1" ]; then
  SMB_SHARE_DIR="${SMB_SHARE_DIR:-/smbshare}"
  SMB_USER="${SMB_USER:-wguser}"
  SMB_PASS="${SMB_PASS:-wgpass}"
  SMB_PORTS="${SMB_PORTS:-445}"
  mkdir -p "${SMB_SHARE_DIR}" /var/run/samba /var/log/samba
  cat >/etc/samba/smb.conf <<EOF
[global]
  server role = standalone server
  map to guest = Never
  security = user
  smb ports = ${SMB_PORTS}
  disable netbios = yes
  load printers = no
  printing = bsd
  log file = /var/log/samba/log.%m
  max log size = 1000
  server min protocol = SMB2

[wgshare]
  path = ${SMB_SHARE_DIR}
  browseable = yes
  writable = yes
  read only = no
  guest ok = no
  valid users = ${SMB_USER}
EOF
  adduser -D "${SMB_USER}" 2>/dev/null || true
  printf '%s\n%s\n' "${SMB_PASS}" "${SMB_PASS}" | smbpasswd -a -s "${SMB_USER}" >/dev/null 2>&1 || true
  chown -R "${SMB_USER}:${SMB_USER}" "${SMB_SHARE_DIR}" 2>/dev/null || true
  smbd -D
fi

# Маршруты overlay→LAN: next-hop по именам сервисов Compose.
# Клиенты часто стартуют после singbox-server (depends_on/healthcheck) — добавляем маршруты в фоне, когда имена появятся в DNS.
if [ "${ADD_OVERLAY_ROUTES:-0}" = "1" ]; then
  (
    _gw() { python3 -c "import socket,sys; print(socket.gethostbyname(sys.argv[1]))" "$1" 2>/dev/null || true; }
    _i=0
    while [ "$_i" -lt 120 ]; do
      _c1=$(_gw singbox-client1)
      _c2=$(_gw singbox-client2)
      _c3=$(_gw singbox-client3)
      _c4=$(_gw singbox-client4)
      if [ -n "$_c1" ] && [ -n "$_c2" ] && [ -n "$_c3" ] && [ -n "$_c4" ]; then
        ip route replace 10.0.0.2/32 via "$_c1" dev eth0 2>/dev/null || ip route add 10.0.0.2/32 via "$_c1" dev eth0 2>/dev/null || true
        ip route replace 10.0.0.3/32 via "$_c2" dev eth0 2>/dev/null || ip route add 10.0.0.3/32 via "$_c2" dev eth0 2>/dev/null || true
        ip route replace 10.0.0.4/32 via "$_c3" dev eth0 2>/dev/null || ip route add 10.0.0.4/32 via "$_c3" dev eth0 2>/dev/null || true
        ip route replace 10.0.0.5/32 via "$_c4" dev eth0 2>/dev/null || ip route add 10.0.0.5/32 via "$_c4" dev eth0 2>/dev/null || true
        exit 0
      fi
      _i=$((_i + 1))
      sleep 1
    done
  ) &
fi

HIDDIFY_CORE_BIN=""
for _d in /hiddify/hiddify-core-linux-*; do
  if [ -x "${_d}/hiddify-core" ]; then
    HIDDIFY_CORE_BIN="${_d}/hiddify-core"
    break
  fi
done
if [ -z "${HIDDIFY_CORE_BIN}" ]; then
  echo "hiddify-core: не найден бинарник в /hiddify/hiddify-core-linux-*/" >&2
  exit 1
fi
exec "${HIDDIFY_CORE_BIN}" "$@"
