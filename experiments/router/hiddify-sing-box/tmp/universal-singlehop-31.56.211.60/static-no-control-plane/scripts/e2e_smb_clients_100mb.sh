#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
STAND_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
cd "${STAND_ROOT}"

COMPOSE_FILE="docker-compose.l3router-e2e-reality-smb.yml"
RUNTIME_DIR="${STAND_ROOT}/runtime"
mkdir -p "${RUNTIME_DIR}"
REPORT_FILE="${RUNTIME_DIR}/smb_clients_100mb_latest.json"
FILE_SIZE_MB="${FILE_SIZE_MB:-100}"

echo "[smb-e2e] up smb clients"
if [[ "${SMB_BUILD_IMAGE:-0}" == "1" ]]; then
  docker compose -f "${COMPOSE_FILE}" up -d --build
else
  docker compose -f "${COMPOSE_FILE}" up -d --no-build
fi
sleep 20

echo "[smb-e2e] wait routes/tun"
docker exec l3router-smb-client1 ping -c 3 -W 2 10.0.0.4 >/dev/null
docker exec l3router-smb-client2 ping -c 3 -W 2 10.0.0.2 >/dev/null

SRC_IN_C1="/tmp/owner-a-${FILE_SIZE_MB}mb.bin"
DST_IN_C2="/tmp/from-owner-a-${FILE_SIZE_MB}mb.bin"
SRC_IN_C2="/tmp/owner-c-${FILE_SIZE_MB}mb.bin"
DST_IN_C1="/tmp/from-owner-c-${FILE_SIZE_MB}mb.bin"

echo "[smb-e2e] generate ${FILE_SIZE_MB}MB payloads"
docker exec l3router-smb-client1 sh -lc "dd if=/dev/urandom of='${SRC_IN_C1}' bs=1M count='${FILE_SIZE_MB}' status=none"
docker exec l3router-smb-client2 sh -lc "dd if=/dev/urandom of='${SRC_IN_C2}' bs=1M count='${FILE_SIZE_MB}' status=none"

echo "[smb-e2e] c1 -> c2 SMB put (PEER_C3_BETA)"
start_ms_a="$(date +%s%3N)"
docker exec l3router-smb-client1 sh -lc "smbclient '//10.0.0.4/PEER_C3_BETA' -U 'owner_c%owner_c_2026' -m SMB3 -c \"put ${SRC_IN_C1} payload-from-owner-a.bin\""
end_ms_a="$(date +%s%3N)"
dur_ms_a="$((end_ms_a - start_ms_a))"
[[ "${dur_ms_a}" -lt 1 ]] && dur_ms_a=1

echo "[smb-e2e] verify c1 -> c2 hash"
docker exec l3router-smb-client2 sh -lc "smbclient '//10.0.0.4/PEER_C3_BETA' -U 'owner_c%owner_c_2026' -m SMB3 -c \"get payload-from-owner-a.bin ${DST_IN_C2}\""
sha_src_a="$(docker exec l3router-smb-client1 sh -lc "sha256sum '${SRC_IN_C1}' | awk '{print \$1}'")"
sha_dst_a="$(docker exec l3router-smb-client2 sh -lc "sha256sum '${DST_IN_C2}' | awk '{print \$1}'")"

echo "[smb-e2e] c2 -> c1 SMB put (PEER_C1_ALPHA)"
start_ms_b="$(date +%s%3N)"
docker exec l3router-smb-client2 sh -lc "smbclient '//10.0.0.2/PEER_C1_ALPHA' -U 'owner_a%owner_a_2026' -m SMB3 -c \"put ${SRC_IN_C2} payload-from-owner-c.bin\""
end_ms_b="$(date +%s%3N)"
dur_ms_b="$((end_ms_b - start_ms_b))"
[[ "${dur_ms_b}" -lt 1 ]] && dur_ms_b=1

echo "[smb-e2e] verify c2 -> c1 hash"
docker exec l3router-smb-client1 sh -lc "smbclient '//10.0.0.2/PEER_C1_ALPHA' -U 'owner_a%owner_a_2026' -m SMB3 -c \"get payload-from-owner-c.bin ${DST_IN_C1}\""
sha_src_b="$(docker exec l3router-smb-client2 sh -lc "sha256sum '${SRC_IN_C2}' | awk '{print \$1}'")"
sha_dst_b="$(docker exec l3router-smb-client1 sh -lc "sha256sum '${DST_IN_C1}' | awk '{print \$1}'")"

bytes="$((FILE_SIZE_MB * 1024 * 1024))"
mbit_a="$(awk -v b="${bytes}" -v ms="${dur_ms_a}" 'BEGIN { s=ms/1000; if (s<1e-9) s=1e-9; printf "%.2f", (b*8)/(s*1000000) }')"
mbit_b="$(awk -v b="${bytes}" -v ms="${dur_ms_b}" 'BEGIN { s=ms/1000; if (s<1e-9) s=1e-9; printf "%.2f", (b*8)/(s*1000000) }')"

ok_a=false
ok_b=false
[[ "${sha_src_a}" == "${sha_dst_a}" ]] && ok_a=true
[[ "${sha_src_b}" == "${sha_dst_b}" ]] && ok_b=true

cat > "${REPORT_FILE}" <<EOF
{
  "mode": "docker-clients-smb-over-l3router",
  "file_size_mb": ${FILE_SIZE_MB},
  "transfer_a": {
    "from": "l3router-smb-client1",
    "to": "l3router-smb-client2",
    "share": "PEER_C3_BETA",
    "duration_ms": ${dur_ms_a},
    "throughput_mbit_per_sec": ${mbit_a},
    "sha256_src": "${sha_src_a}",
    "sha256_dst": "${sha_dst_a}",
    "sha256_match": ${ok_a}
  },
  "transfer_b": {
    "from": "l3router-smb-client2",
    "to": "l3router-smb-client1",
    "share": "PEER_C1_ALPHA",
    "duration_ms": ${dur_ms_b},
    "throughput_mbit_per_sec": ${mbit_b},
    "sha256_src": "${sha_src_b}",
    "sha256_dst": "${sha_dst_b}",
    "sha256_match": ${ok_b}
  }
}
EOF

echo "[smb-e2e] report: ${REPORT_FILE}"
cat "${REPORT_FILE}"

if [[ "${ok_a}" != "true" || "${ok_b}" != "true" ]]; then
  echo "[smb-e2e] FAIL: hash mismatch" >&2
  exit 1
fi

echo "[smb-e2e] OK"
