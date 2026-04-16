#!/usr/bin/env bash
# ВНИМАНИЕ: это НЕ протокол SMB — только dd + cp + sha256 на одной машине (имя файла историческое).
# Реальный SMB 100 MiB через l3router: scripts/smb_transfer_100mb_e2e.sh (после e2e compose / на VPS).
# Не проверяет прохождение трафика через l3router/VPS — для этого см. e2e_vps_run.sh и smb_transfer_100mb_e2e.sh.
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
RUNTIME_DIR="${ROOT_DIR}/runtime"
mkdir -p "${RUNTIME_DIR}"

: "${SRC_DIR:=/tmp}"
: "${DST_DIR:=/tmp}"
: "${FILE_SIZE_MB:=100}"

SRC_FILE="${SRC_DIR}/l3router-static-${FILE_SIZE_MB}mb.bin"
DST_FILE="${DST_DIR}/l3router-static-${FILE_SIZE_MB}mb.bin"
REPORT_FILE="${RUNTIME_DIR}/latest_100mb_metrics.json"

if [[ "${SRC_DIR}" == "${DST_DIR}" ]]; then
  echo "SRC_DIR and DST_DIR must be different for a meaningful transfer test" >&2
  exit 3
fi

echo "[static-only] generating ${FILE_SIZE_MB}MB source file: ${SRC_FILE}"
dd if=/dev/urandom of="${SRC_FILE}" bs=1M count="${FILE_SIZE_MB}" status=none

echo "[static-only] copying to destination: ${DST_FILE}"
start_epoch="$(date +%s)"
cp "${SRC_FILE}" "${DST_FILE}"
end_epoch="$(date +%s)"

duration_sec="$((end_epoch - start_epoch))"
if [[ "${duration_sec}" -le 0 ]]; then
  duration_sec=1
fi

src_sha="$(sha256sum "${SRC_FILE}" | awk '{print $1}')"
dst_sha="$(sha256sum "${DST_FILE}" | awk '{print $1}')"
bytes="$(wc -c < "${SRC_FILE}" | tr -d ' ')"
throughput_bps="$((bytes / duration_sec))"
throughput_mib_per_sec="$(awk -v b="${throughput_bps}" 'BEGIN {printf "%.2f", b/1048576}')"
throughput_mbit_per_sec="$(awk -v b="${throughput_bps}" 'BEGIN {printf "%.2f", (b*8)/1000000}')"

if [[ "${src_sha}" != "${dst_sha}" ]]; then
  echo "sha256 mismatch: src=${src_sha}, dst=${dst_sha}" >&2
  exit 2
fi

cat > "${REPORT_FILE}" <<EOF
{
  "mode": "static-only",
  "file_size_mb": ${FILE_SIZE_MB},
  "bytes": ${bytes},
  "duration_sec": ${duration_sec},
  "throughput_bytes_per_sec": ${throughput_bps},
  "throughput_mib_per_sec": ${throughput_mib_per_sec},
  "throughput_mbit_per_sec": ${throughput_mbit_per_sec},
  "src_file": "${SRC_FILE}",
  "dst_file": "${DST_FILE}",
  "sha256_src": "${src_sha}",
  "sha256_dst": "${dst_sha}",
  "sha256_match": true,
  "runtime_route_api_used": false
}
EOF

echo "[static-only] transfer verification complete"
echo "[static-only] time: ${duration_sec}s  (~${throughput_mib_per_sec} MiB/s, ~${throughput_mbit_per_sec} Mbit/s) — локальный диск, не VPN/SMB"
echo "[static-only] report: ${REPORT_FILE}"
