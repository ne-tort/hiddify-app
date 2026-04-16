#!/usr/bin/env sh
set -eu

SHARE_NAME="${SMB_SHARE_NAME:-PEER_SHARE}"
SMB_USER="${SMB_USER:-peer}"
SMB_PASS="${SMB_PASS:-peer123}"
SMB_PATH="${SMB_SHARE_PATH:-/srv/smb/share}"

mkdir -p "${SMB_PATH}" /var/log/samba
chmod 0777 "${SMB_PATH}"

if ! id "${SMB_USER}" >/dev/null 2>&1; then
  adduser -D -s /sbin/nologin "${SMB_USER}" >/dev/null 2>&1 || true
fi

mkdir -p /etc/samba
cat > /etc/samba/smb.conf <<EOF
[global]
workgroup = WORKGROUP
security = user
map to guest = Bad User
server min protocol = SMB2
disable netbios = yes
smb ports = 445

[${SHARE_NAME}]
path = ${SMB_PATH}
read only = no
guest ok = no
valid users = ${SMB_USER}
force user = ${SMB_USER}
create mask = 0660
directory mask = 0770
EOF

if ! pdbedit -L 2>/dev/null | awk -F: '{print $1}' | grep -qx "${SMB_USER}"; then
  (printf '%s\n%s\n' "${SMB_PASS}" "${SMB_PASS}") | smbpasswd -a -s "${SMB_USER}" >/dev/null
fi

smbd -D -s /etc/samba/smb.conf

exec /usr/local/bin/sing-box run -c /etc/sing-box/config.json
