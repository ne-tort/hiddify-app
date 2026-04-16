#!/usr/bin/env bash
# Реальный SMB: smbd на peer (tun1 / PEER_IP), загрузка 100 MiB с клиента (tun0 / LOCAL_IP).
# Трафик идёт между 10.0.0.2 и 10.0.0.3 через l3router на хабе (как и ping в e2e).
#
# Требования на хосте (VPS), где уже подняты tun0/tun1 от docker-compose.l3router-e2e-reality.yml:
#   apt-get install -y samba smbclient
# Запуск от root или через sudo. По умолчанию порт SMB_PORT=1445 (не 445), чтобы не пересечься с системным smbd.
#
# Переменные:
#   PEER_IP=10.0.0.3 LOCAL_IP=10.0.0.2 FILE_SIZE_MB=100 SMB_PORT=1445
#   SMB_E2E_SKIP_VERIFY=1 — не скачивать файл обратно для sha256
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
RUNTIME_DIR="${ROOT_DIR}/runtime"
mkdir -p "${RUNTIME_DIR}"
REPORT_JSON="${RUNTIME_DIR}/smb_100mb_e2e_latest.json"

PEER_IP="${PEER_IP:-10.0.0.3}"
LOCAL_IP="${LOCAL_IP:-10.0.0.2}"
FILE_SIZE_MB="${FILE_SIZE_MB:-100}"
SMB_PORT="${SMB_PORT:-1445}"
SHARE_NAME="${SHARE_NAME:-l3data}"
REMOTE_NAME="${REMOTE_NAME:-payload100.bin}"

need_cmd() {
  local c
  for c in "$@"; do
    if ! command -v "$c" >/dev/null 2>&1; then
      echo "[smb-e2e] ERROR: need command: $c (apt-get install -y samba smbclient)" >&2
      exit 4
    fi
  done
}

need_cmd smbd smbclient

if [[ "$(id -u)" -ne 0 ]]; then
  echo "[smb-e2e] ERROR: run as root (smbd binds :445). Example: sudo $0" >&2
  exit 5
fi

iface_of_ip() {
  ip -4 -o addr show scope global 2>/dev/null | awk -v want="$1" '$4 ~ "^"want"/" {print $2; exit}'
}

LOCAL_DEV="$(iface_of_ip "${LOCAL_IP}")"
PEER_DEV="$(iface_of_ip "${PEER_IP}")"
if [[ -z "${LOCAL_DEV}" ]]; then
  echo "[smb-e2e] ERROR: no interface has ${LOCAL_IP}/xx. Is e2e compose up?" >&2
  exit 6
fi
if [[ -z "${PEER_DEV}" ]]; then
  echo "[smb-e2e] ERROR: no interface has ${PEER_IP}/xx (second client must be running)." >&2
  exit 6
fi
echo "[smb-e2e] ${LOCAL_IP} → dev ${LOCAL_DEV}, ${PEER_IP} → dev ${PEER_DEV}"

if ss -Hlnp "sport = :${SMB_PORT}" 2>/dev/null | grep -q .; then
  echo "[smb-e2e] ERROR: port ${SMB_PORT} already in use. Set SMB_PORT to a free port." >&2
  exit 7
fi

SMB_WORK="$(mktemp -d /tmp/l3router-smb-e2e.XXXXXX)"
chmod 700 "${SMB_WORK}"
SHARE_DIR="${SMB_WORK}/share"
mkdir -p "${SHARE_DIR}" "${SMB_WORK}/run" "${SMB_WORK}/state" "${SMB_WORK}/cache"
chmod 777 "${SHARE_DIR}"

SMB_CONF="${SMB_WORK}/smb.conf"
cat > "${SMB_CONF}" <<EOF
[global]
workgroup = L3TEST
security = user
map to guest = Bad User
guest account = nobody
server min protocol = SMB2
bind interfaces only = no
smb ports = ${SMB_PORT}
disable netbios = yes
pid directory = ${SMB_WORK}/run
state directory = ${SMB_WORK}/state
cache directory = ${SMB_WORK}/cache
log file = ${SMB_WORK}/state/log.%m
max log size = 0

[${SHARE_NAME}]
path = ${SHARE_DIR}
browseable = yes
read only = no
guest ok = yes
guest only = yes
force user = root
hosts allow = 10.0.0.0/24 127.0.0.1
EOF

cleanup() {
  if [[ -n "${SMBD_PID:-}" ]] && kill -0 "${SMBD_PID}" 2>/dev/null; then
    kill "${SMBD_PID}" 2>/dev/null || true
    wait "${SMBD_PID}" 2>/dev/null || true
  fi
  rm -rf "${SMB_WORK}"
}
trap cleanup EXIT

echo "[smb-e2e] starting smbd (config ${SMB_CONF})..."
smbd -s "${SMB_CONF}" -F &
SMBD_PID=$!
sleep 3
if ! kill -0 "${SMBD_PID}" 2>/dev/null; then
  echo "[smb-e2e] FAIL: smbd exited immediately. testparm:" >&2
  testparm -s "${SMB_CONF}" 2>&1 | tail -30 || true
  exit 8
fi

SMB_LS_OK=0
for _try in 1 2 3 4 5; do
  if smbclient "//${PEER_IP}/${SHARE_NAME}" -N -I "${LOCAL_IP}" -p "${SMB_PORT}" -t 60 -m SMB3 -c "ls" >/dev/null 2>&1; then
    SMB_LS_OK=1
    break
  fi
  sleep 2
done
if [[ "${SMB_LS_OK}" -ne 1 ]]; then
  echo "[smb-e2e] FAIL: smbclient list after retries. smbclient stderr:" >&2
  smbclient "//${PEER_IP}/${SHARE_NAME}" -N -I "${LOCAL_IP}" -p "${SMB_PORT}" -t 60 -m SMB3 -c "ls" 2>&1 || true
  ls -la "${SMB_WORK}/state/" 2>/dev/null || true
  tail -80 "${SMB_WORK}/state/log."* 2>/dev/null || true
  exit 8
fi

SRC_FILE="${SMB_WORK}/src_${FILE_SIZE_MB}mb.bin"
echo "[smb-e2e] generating ${FILE_SIZE_MB} MiB random data: ${SRC_FILE}"
dd if=/dev/urandom of="${SRC_FILE}" bs=1M count="${FILE_SIZE_MB}" status=none
BYTES="$(wc -c < "${SRC_FILE}" | tr -d ' ')"
SRC_SHA="$(sha256sum "${SRC_FILE}" | awk '{print $1}')"

echo "[smb-e2e] PUT //${PEER_IP}/${SHARE_NAME}/${REMOTE_NAME} (bind client ${LOCAL_IP})..."
START_NS="$(date +%s%N)"
smbclient "//${PEER_IP}/${SHARE_NAME}" -N -I "${LOCAL_IP}" -p "${SMB_PORT}" -t 600 -m SMB3 -c "put ${SRC_FILE} ${REMOTE_NAME}"
END_NS="$(date +%s%N)"
DUR_NS=$((END_NS - START_NS))
# bash integer division: duration in ms
DUR_MS=$((DUR_NS / 1000000))
if [[ "${DUR_MS}" -lt 1 ]]; then
  DUR_MS=1
fi
DUR_SEC_AWK="$(awk -v ms="${DUR_MS}" 'BEGIN { printf "%.3f", ms/1000 }')"

# throughput from measured wall time (bytes * 8 / s / 1e6 = Mbit/s)
THR_MBIT="$(awk -v b="${BYTES}" -v ms="${DUR_MS}" 'BEGIN { s=ms/1000; if (s<1e-9) s=1e-9; printf "%.2f", (b*8)/(s*1000000) }')"
THR_MIB="$(awk -v b="${BYTES}" -v ms="${DUR_MS}" 'BEGIN { s=ms/1000; if (s<1e-9) s=1e-9; printf "%.2f", b/s/1048576 }')"

if [[ "${SMB_E2E_SKIP_VERIFY:-0}" != "1" ]]; then
  DST_FILE="${SMB_WORK}/verify.bin"
  echo "[smb-e2e] GET verify..."
  smbclient "//${PEER_IP}/${SHARE_NAME}" -N -I "${LOCAL_IP}" -p "${SMB_PORT}" -t 600 -m SMB3 -c "get ${REMOTE_NAME} ${DST_FILE}"
  DST_SHA="$(sha256sum "${DST_FILE}" | awk '{print $1}')"
  if [[ "${SRC_SHA}" != "${DST_SHA}" ]]; then
    echo "[smb-e2e] FAIL: sha256 mismatch src=${SRC_SHA} dst=${DST_SHA}" >&2
    exit 9
  fi
fi

cat > "${REPORT_JSON}" <<EOF
{
  "mode": "smb-e2e-l3router",
  "smb_port": ${SMB_PORT},
  "peer_ip": "${PEER_IP}",
  "local_client_ip": "${LOCAL_IP}",
  "file_size_mb": ${FILE_SIZE_MB},
  "bytes": ${BYTES},
  "duration_sec": ${DUR_SEC_AWK},
  "duration_ms": ${DUR_MS},
  "throughput_mib_per_sec": ${THR_MIB},
  "throughput_mbit_per_sec": ${THR_MBIT},
  "share": "${SHARE_NAME}",
  "remote_name": "${REMOTE_NAME}",
  "sha256_src": "${SRC_SHA}",
  "verify_download": $( [[ "${SMB_E2E_SKIP_VERIFY:-0}" != "1" ]] && echo true || echo false ),
  "report_file": "${REPORT_JSON}"
}
EOF

echo "[smb-e2e] OK"
echo "[smb-e2e] duration: ${DUR_SEC_AWK} s (${DUR_MS} ms), ~${THR_MIB} MiB/s, ~${THR_MBIT} Mbit/s"
echo "[smb-e2e] report: ${REPORT_JSON}"
