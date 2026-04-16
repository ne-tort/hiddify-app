#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
STAND_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
cd "${STAND_ROOT}"

RUNTIME_DIR="${STAND_ROOT}/runtime"
mkdir -p "${RUNTIME_DIR}"
REPORT_FILE="${RUNTIME_DIR}/tcp_udp_100mb_latest.json"

FILE_SIZE_MB="${FILE_SIZE_MB:-100}"
BYTES_EXPECTED=$((FILE_SIZE_MB * 1024 * 1024))
TCP_PORT="${TCP_PORT:-29001}"
UDP_PORT="${UDP_PORT:-29002}"

SRC_A="/tmp/tcpudp-owner-a-${FILE_SIZE_MB}mb.bin"
SRC_C="/tmp/tcpudp-owner-c-${FILE_SIZE_MB}mb.bin"
DST_A_TCP="/tmp/tcp-owner-a-recv.bin"
DST_C_TCP="/tmp/tcp-owner-c-recv.bin"
DST_A_UDP="/tmp/udp-owner-a-recv.bin"
DST_C_UDP="/tmp/udp-owner-c-recv.bin"

ms_now() {
  date +%s%3N
}

bytes_in_file() {
  local container="$1"
  local path="$2"
  docker exec "${container}" sh -lc "if [ -f '${path}' ]; then wc -c < '${path}' | tr -d ' '; else echo 0; fi"
}

sha_of_file() {
  local container="$1"
  local path="$2"
  docker exec "${container}" sh -lc "sha256sum '${path}' | cut -d ' ' -f1"
}

kill_nc() {
  local container="$1"
  docker exec "${container}" sh -lc "pkill nc >/dev/null 2>&1 || true"
}

wait_for_size() {
  local container="$1"
  local path="$2"
  local expected="$3"
  local timeout_sec="${4:-90}"
  local i=0
  while [ "${i}" -lt "${timeout_sec}" ]; do
    local got
    got="$(bytes_in_file "${container}" "${path}")"
    if [ "${got}" -ge "${expected}" ]; then
      echo "${got}"
      return 0
    fi
    i=$((i + 1))
    sleep 1
  done
  bytes_in_file "${container}" "${path}"
  return 1
}

echo "[tcp-udp] preparing payloads ${FILE_SIZE_MB}MB each side"
docker exec l3router-smb-client1 sh -lc "dd if=/dev/urandom of='${SRC_A}' bs=1M count='${FILE_SIZE_MB}' status=none"
docker exec l3router-smb-client2 sh -lc "dd if=/dev/urandom of='${SRC_C}' bs=1M count='${FILE_SIZE_MB}' status=none"

sha_src_a="$(sha_of_file l3router-smb-client1 "${SRC_A}")"
sha_src_c="$(sha_of_file l3router-smb-client2 "${SRC_C}")"

echo "[tcp-udp] TCP A->C"
kill_nc l3router-smb-client2
docker exec -d l3router-smb-client2 sh -lc "rm -f '${DST_A_TCP}'; nc -l -p '${TCP_PORT}' > '${DST_A_TCP}'"
sleep 1
t0="$(ms_now)"
docker exec l3router-smb-client1 sh -lc "cat '${SRC_A}' | nc -w 20 10.0.0.4 '${TCP_PORT}'"
t1="$(ms_now)"
dur_tcp_a=$((t1 - t0))
[ "${dur_tcp_a}" -lt 1 ] && dur_tcp_a=1
bytes_tcp_a="$(wait_for_size l3router-smb-client2 "${DST_A_TCP}" "${BYTES_EXPECTED}" 90 || true)"
kill_nc l3router-smb-client2
sha_tcp_a="$(sha_of_file l3router-smb-client2 "${DST_A_TCP}")"
ok_tcp_a=false
[ "${sha_src_a}" = "${sha_tcp_a}" ] && ok_tcp_a=true
mbit_tcp_a="$(awk -v b="${BYTES_EXPECTED}" -v ms="${dur_tcp_a}" 'BEGIN { s=ms/1000; if (s<1e-9) s=1e-9; printf "%.2f", (b*8)/(s*1000000) }')"

echo "[tcp-udp] TCP C->A"
kill_nc l3router-smb-client1
docker exec -d l3router-smb-client1 sh -lc "rm -f '${DST_C_TCP}'; nc -l -p '${TCP_PORT}' > '${DST_C_TCP}'"
sleep 1
t0="$(ms_now)"
docker exec l3router-smb-client2 sh -lc "cat '${SRC_C}' | nc -w 20 10.0.0.2 '${TCP_PORT}'"
t1="$(ms_now)"
dur_tcp_c=$((t1 - t0))
[ "${dur_tcp_c}" -lt 1 ] && dur_tcp_c=1
bytes_tcp_c="$(wait_for_size l3router-smb-client1 "${DST_C_TCP}" "${BYTES_EXPECTED}" 90 || true)"
kill_nc l3router-smb-client1
sha_tcp_c="$(sha_of_file l3router-smb-client1 "${DST_C_TCP}")"
ok_tcp_c=false
[ "${sha_src_c}" = "${sha_tcp_c}" ] && ok_tcp_c=true
mbit_tcp_c="$(awk -v b="${BYTES_EXPECTED}" -v ms="${dur_tcp_c}" 'BEGIN { s=ms/1000; if (s<1e-9) s=1e-9; printf "%.2f", (b*8)/(s*1000000) }')"

echo "[tcp-udp] UDP A->C"
kill_nc l3router-smb-client2
docker exec -d l3router-smb-client2 sh -lc "rm -f '${DST_A_UDP}'; nc -u -l -p '${UDP_PORT}' -w 3 > '${DST_A_UDP}'"
sleep 1
t0="$(ms_now)"
docker exec l3router-smb-client1 sh -lc "cat '${SRC_A}' | nc -u -w 1 10.0.0.4 '${UDP_PORT}'"
t1="$(ms_now)"
dur_udp_a=$((t1 - t0))
[ "${dur_udp_a}" -lt 1 ] && dur_udp_a=1
sleep 4
kill_nc l3router-smb-client2
bytes_udp_a="$(bytes_in_file l3router-smb-client2 "${DST_A_UDP}")"
sha_udp_a="$(sha_of_file l3router-smb-client2 "${DST_A_UDP}")"
ok_udp_a=false
[ "${sha_src_a}" = "${sha_udp_a}" ] && ok_udp_a=true
mbit_udp_a="$(awk -v b="${bytes_udp_a}" -v ms="${dur_udp_a}" 'BEGIN { s=ms/1000; if (s<1e-9) s=1e-9; printf "%.2f", (b*8)/(s*1000000) }')"

echo "[tcp-udp] UDP C->A"
kill_nc l3router-smb-client1
docker exec -d l3router-smb-client1 sh -lc "rm -f '${DST_C_UDP}'; nc -u -l -p '${UDP_PORT}' -w 3 > '${DST_C_UDP}'"
sleep 1
t0="$(ms_now)"
docker exec l3router-smb-client2 sh -lc "cat '${SRC_C}' | nc -u -w 1 10.0.0.2 '${UDP_PORT}'"
t1="$(ms_now)"
dur_udp_c=$((t1 - t0))
[ "${dur_udp_c}" -lt 1 ] && dur_udp_c=1
sleep 4
kill_nc l3router-smb-client1
bytes_udp_c="$(bytes_in_file l3router-smb-client1 "${DST_C_UDP}")"
sha_udp_c="$(sha_of_file l3router-smb-client1 "${DST_C_UDP}")"
ok_udp_c=false
[ "${sha_src_c}" = "${sha_udp_c}" ] && ok_udp_c=true
mbit_udp_c="$(awk -v b="${bytes_udp_c}" -v ms="${dur_udp_c}" 'BEGIN { s=ms/1000; if (s<1e-9) s=1e-9; printf "%.2f", (b*8)/(s*1000000) }')"

cat > "${REPORT_FILE}" <<EOF
{
  "mode": "docker-clients-tcp-udp-over-l3router",
  "file_size_mb": ${FILE_SIZE_MB},
  "tcp": {
    "a_to_c": {
      "duration_ms": ${dur_tcp_a},
      "throughput_mbit_per_sec": ${mbit_tcp_a},
      "bytes_received": ${bytes_tcp_a},
      "sha256_match": ${ok_tcp_a}
    },
    "c_to_a": {
      "duration_ms": ${dur_tcp_c},
      "throughput_mbit_per_sec": ${mbit_tcp_c},
      "bytes_received": ${bytes_tcp_c},
      "sha256_match": ${ok_tcp_c}
    }
  },
  "udp": {
    "a_to_c": {
      "duration_ms": ${dur_udp_a},
      "throughput_mbit_per_sec": ${mbit_udp_a},
      "bytes_received": ${bytes_udp_a},
      "delivery_ratio_percent": $(awk -v got="${bytes_udp_a}" -v exp="${BYTES_EXPECTED}" 'BEGIN { if (exp==0) {print "0.00"} else {printf "%.2f", (got*100)/exp} }'),
      "sha256_match": ${ok_udp_a}
    },
    "c_to_a": {
      "duration_ms": ${dur_udp_c},
      "throughput_mbit_per_sec": ${mbit_udp_c},
      "bytes_received": ${bytes_udp_c},
      "delivery_ratio_percent": $(awk -v got="${bytes_udp_c}" -v exp="${BYTES_EXPECTED}" 'BEGIN { if (exp==0) {print "0.00"} else {printf "%.2f", (got*100)/exp} }'),
      "sha256_match": ${ok_udp_c}
    }
  }
}
EOF

echo "[tcp-udp] report: ${REPORT_FILE}"
cat "${REPORT_FILE}"
