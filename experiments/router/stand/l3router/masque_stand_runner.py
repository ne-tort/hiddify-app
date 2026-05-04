#!/usr/bin/env python3
import argparse
import hashlib
import json
import math
import os
import shutil
import subprocess
import sys
import time
from pathlib import Path


ROOT = Path(__file__).resolve().parent
CORE_DIR = ROOT.parent.parent.parent.parent / "hiddify-core" / "hiddify-sing-box"
ARTIFACT = ROOT / "artifacts" / "sing-box-linux-amd64"
COMPOSE_FILE = ROOT / "docker-compose.masque-e2e.yml"
RUNTIME_DIR = ROOT / "runtime"
DEFAULT_CLIENT_CONFIG = "./configs/masque-client.json"
CONNECT_IP_CLIENT_CONFIG = "./configs/masque-client-connect-ip.json"

SERVER_CONTAINER = "masque-server-core"
CLIENT_CONTAINER = "masque-client-core"
IPERF_CONTAINER = "masque-iperf-server"

# SOCKS5 CONNECT (no auth) over sing-box SOCKS inbound → MASQUE connect_stream dataplane.
# Avoids/socat quirks with numeric IPv4 in SOCKS URLs on Alpine/busybox glues.
_TCP_STREAM_SEND_SOCKS5 = r"""import os, socket, struct, time
BYTE_COUNT = int(os.environ["MASQUE_TCPSEND_BYTES"])
DEST_HOST = os.environ["MASQUE_DST_HOST"]
DEST_PORT = int(os.environ["MASQUE_DST_PORT"])

def socks5_tcp_connect(proxy_host, proxy_port, ip, port):
    s = socket.create_connection((proxy_host, proxy_port), timeout=20)
    s.sendall(b"\x05\x01\x00")
    ver, method = s.recv(2)
    if ver != 5 or method != 0:
        raise SystemExit("socks handshake %r,%r" % (ver, method))
    bip = socket.inet_aton(ip)
    s.sendall(b"\x05\x01\x00\x01" + bip + struct.pack("!H", port))
    hdr = s.recv(4)
    if len(hdr) != 4:
        raise SystemExit("socks connect short header")
    ver, rep, _rsv, atyp = hdr[0], hdr[1], hdr[2], hdr[3]
    if ver != 5:
        raise SystemExit("socks connect bad ver")
    if rep != 0:
        raise SystemExit("socks connect failed rep=%s" % rep)
    if atyp == 1:
        tail = s.recv(6)
        if len(tail) != 6:
            raise SystemExit("socks ipv4 bind length")
    elif atyp == 4:
        tail = s.recv(18)
        if len(tail) != 18:
            raise SystemExit("socks ipv6 bind length")
    elif atyp == 3:
        ln = s.recv(1)
        if not ln:
            raise SystemExit("socks domain eof")
        dlen = ln[0]
        tail = s.recv(dlen + 2)
        if len(tail) != dlen + 2:
            raise SystemExit("socks domain bind length")
    else:
        raise SystemExit("socks unsupported atyp=%s" % atyp)
    return s


def main():
    s = socks5_tcp_connect("127.0.0.1", 1080, DEST_HOST, DEST_PORT)
    payload = bytes(BYTE_COUNT)
    try:
        s.sendall(memoryview(payload))
    except OSError as ex:
        raise SystemExit(str(ex))
    # Let the HTTP/3 stream / pipe drain before half-close; otherwise tail bytes
    # can be clipped (observed as exactly 8192 B on the socat sink in CI).
    time.sleep(0.6)
    s.shutdown(socket.SHUT_WR)
    s.close()


main()
"""

_TCP_IP_SEND_UDP_PACED = r"""import os, socket, time
BYTE_COUNT = int(os.environ["MASQUE_UDP_SEND_BYTES"])
DEST_HOST = os.environ["MASQUE_UDP_DEST_HOST"]
DEST_PORT = int(os.environ["MASQUE_UDP_DEST_PORT"])
CHUNK = max(1, int(os.environ.get("MASQUE_UDP_CHUNK", "1172")))
RATE_BPS = int(os.environ.get("MASQUE_UDP_RATE_BPS", "0"))
SNDBUF = int(os.environ.get("MASQUE_UDP_SNDBUF", str(16 * 1024 * 1024)))

sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
sock.setsockopt(socket.SOL_SOCKET, socket.SO_SNDBUF, SNDBUF)
payload = bytes(CHUNK)
sent = 0
start = time.monotonic()
while sent < BYTE_COUNT:
    n = min(CHUNK, BYTE_COUNT - sent)
    sock.sendto(payload[:n], (DEST_HOST, DEST_PORT))
    sent += n
    if RATE_BPS > 0:
        target_elapsed = sent / RATE_BPS
        sleep_for = target_elapsed - (time.monotonic() - start)
        if sleep_for > 0:
            time.sleep(sleep_for)
sock.close()
"""

BYTES_10KB = 10 * 1024
BYTES_500MB = 500 * 1024 * 1024
SMOKE_DEADLINE_SEC = 5.0

# bulk_single_flow: after paced `socat` finishes, QUIC/datagram + UDP sink can still
# deliver the last sub-percent of MiB on busy Docker hosts; `wait_for_bytes` used
# `strict_budget` from transfer start, so settle often got 0s. This slack extends only
# the receiver polling window (sender `timeout` stays `strict_budget`).
# Base receive tail; scaled with strict_budget so large MiB runs keep drain headroom.
BULK_SINGLE_FLOW_RECEIVE_TAIL_BASE_SEC = 12.0
BULK_SINGLE_FLOW_RECEIVE_TAIL_PER_STRICT_SEC = 0.2
BULK_SINGLE_FLOW_RECEIVE_TAIL_CAP_SEC = 60.0
# Second-phase poll budget after primary wait (still capped by phase_deadline).
BULK_SINGLE_FLOW_NEAR_COMPLETE_DRAIN_SEC = 10.0

# Default `pv -L` for bulk when MASQUE_TCP_IP_RATE_LIMIT is unset: unlimited `head|socat`
# bursts lose ~10%+ on the stand; ~1350 KiB/s balances loss vs MiB-sized wall gates
# together with receive tail (settle polls + in-flight QUIC/UDP on Docker).
TCP_IP_BULK_DEFAULT_RATE_LIMIT = "1350k"


def bytes_from_megabytes_arg(mb: int) -> int:
    """Binary MiB sizing (matches strict_timeout_sec / stand budgets)."""
    return int(mb) * 1024 * 1024


def parse_rate_limit_to_bps(rate_limit: str) -> int:
    value = (rate_limit or "").strip().lower()
    if not value:
        return 0
    suffix = value[-1]
    scale = 1
    number = value
    if suffix in {"k", "m", "g"}:
        number = value[:-1]
        if suffix == "k":
            scale = 1000
        elif suffix == "m":
            scale = 1000 * 1000
        elif suffix == "g":
            scale = 1000 * 1000 * 1000
    try:
        return max(0, int(float(number) * scale))
    except ValueError:
        return 0


def run(cmd, cwd=None, env=None, check=True):
    print(f"$ {' '.join(cmd)}")
    return subprocess.run(cmd, cwd=cwd, env=env, check=check, text=True)


def run_capture(cmd, cwd=None, env=None):
    print(f"$ {' '.join(cmd)}")
    return subprocess.run(cmd, cwd=cwd, env=env, check=True, text=True, capture_output=True)


def docker_bin():
    return shutil.which("docker.exe") or shutil.which("docker") or "docker"


def docker_exec(docker, container, script, check=True):
    return run([docker, "exec", container, "sh", "-lc", script], cwd=ROOT, check=check)


def docker_exec_capture(docker, container, script):
    result = run_capture([docker, "exec", container, "sh", "-lc", script], cwd=ROOT)
    return result.stdout.strip()


def docker_logs_capture(docker, container):
    result = run_capture([docker, "logs", container], cwd=ROOT)
    return (result.stdout or "") + (result.stderr or "")


def compile_singbox():
    env = os.environ.copy()
    env["CGO_ENABLED"] = "0"
    env["GOOS"] = "linux"
    env["GOARCH"] = "amd64"
    run(
        [
            "go",
            "build",
            "-tags",
            "with_masque",
            "-o",
            str(ARTIFACT),
            "./cmd/sing-box",
        ],
        cwd=CORE_DIR,
        env=env,
    )
    # Docker layer cache keys on source mtimes in the build context; without bumping
    # mtime, `COPY artifacts/sing-box-linux-amd64` can stay CACHED across Go changes.
    try:
        ARTIFACT.touch(exist_ok=True)
    except OSError:
        pass


def compose_up(docker, client_config):
    env = os.environ.copy()
    env["MASQUE_CLIENT_CONFIG"] = client_config
    if ARTIFACT.is_file():
        env["SINGBOX_ARTIFACT_STAMP"] = hashlib.sha256(ARTIFACT.read_bytes()).hexdigest()
    run([docker, "compose", "-f", str(COMPOSE_FILE), "down", "-v"], cwd=ROOT, env=env, check=False)
    run([docker, "compose", "-f", str(COMPOSE_FILE), "up", "-d", "--build"], cwd=ROOT, env=env)
    for container in (SERVER_CONTAINER, CLIENT_CONTAINER):
        deadline = time.time() + 15
        while time.time() < deadline:
            rc = subprocess.run([docker, "exec", container, "sh", "-lc", "true"], cwd=ROOT, text=True).returncode
            if rc == 0:
                break
            time.sleep(0.2)
        else:
            raise RuntimeError(f"container not ready: {container}")
    time.sleep(1.0)


def wait_socks_ready(docker, timeout_sec=12):
    deadline = time.time() + timeout_sec
    while time.time() < deadline:
        rc = subprocess.run(
            [docker, "exec", CLIENT_CONTAINER, "sh", "-lc", "ss -ltn | grep -q ':1080 '"],
            cwd=ROOT,
            text=True,
        ).returncode
        if rc == 0:
            return
        time.sleep(0.2)
    raise RuntimeError("SOCKS listener on 1080 is not ready")


def wait_tcp_listener(docker, container, port, timeout_sec=5):
    deadline = time.time() + timeout_sec
    probe_cmd = "command -v ss >/dev/null 2>&1 || command -v netstat >/dev/null 2>&1"
    probe_rc = subprocess.run([docker, "exec", container, "sh", "-lc", probe_cmd], cwd=ROOT, text=True).returncode
    if probe_rc != 0:
        time.sleep(0.5)
        return
    cmd = f"(ss -ltn 2>/dev/null | grep -q ':{port} ') || (netstat -ltn 2>/dev/null | grep -q ':{port} ')"
    while time.time() < deadline:
        rc = subprocess.run([docker, "exec", container, "sh", "-lc", cmd], cwd=ROOT, text=True).returncode
        if rc == 0:
            return
        time.sleep(0.2)
    raise RuntimeError(f"TCP listener not ready on {container}:{port}")


def wait_udp_listener(docker, container, port, timeout_sec=5):
    deadline = time.time() + timeout_sec
    cmd = f"ss -lun | grep -q ':{port} '"
    while time.time() < deadline:
        rc = subprocess.run([docker, "exec", container, "sh", "-lc", cmd], cwd=ROOT, text=True).returncode
        if rc == 0:
            return
        time.sleep(0.2)
    raise RuntimeError(f"UDP listener not ready on {container}:{port}")


def bytes_on_file(docker, container, path):
    out = docker_exec_capture(docker, container, f"if [ -f {path} ]; then wc -c < {path}; else echo 0; fi")
    try:
        return int(out or "0")
    except ValueError:
        return 0


def wait_for_bytes(docker, container, path, expected, timeout_sec):
    deadline = time.time() + timeout_sec
    got = 0
    while time.time() < deadline:
        got = bytes_on_file(docker, container, path)
        if got >= expected:
            return got
        time.sleep(0.1)
    return got


def wait_for_stable_size(docker, container, path, expected, timeout_sec, checks=4, interval_sec=0.2):
    deadline = time.time() + timeout_sec
    last = -1
    stable = 0
    got = 0
    while time.time() < deadline:
        got = bytes_on_file(docker, container, path)
        if got < expected:
            stable = 0
            last = got
            time.sleep(interval_sec)
            continue
        if got == last:
            stable += 1
            if stable >= checks:
                return got, True
        else:
            stable = 0
        last = got
        time.sleep(interval_sec)
    return got, False


def zero_payload_sha256(byte_count: int) -> str:
    h = hashlib.sha256()
    chunk = b"\x00" * (1024 * 1024)
    full, rem = divmod(byte_count, len(chunk))
    for _ in range(full):
        h.update(chunk)
    if rem:
        h.update(chunk[:rem])
    return h.hexdigest()


def file_sha256(docker, container, path):
    script = (
        f"if [ ! -f {path} ]; then echo ''; exit 0; fi; "
        f"if command -v sha256sum >/dev/null 2>&1; then sha256sum {path} | awk '{{print $1}}'; "
        f"elif command -v busybox >/dev/null 2>&1; then busybox sha256sum {path} | awk '{{print $1}}'; "
        f"elif command -v openssl >/dev/null 2>&1; then openssl dgst -sha256 {path} | awk '{{print $NF}}'; "
        f"else echo ''; fi"
    )
    return docker_exec_capture(docker, container, script).strip()


def file_sha256_slice(docker, container, path, offset, length):
    script = (
        f"if [ ! -f {path} ]; then echo ''; exit 0; fi; "
        f"dd if={path} bs=1 skip={offset} count={length} 2>/dev/null | "
        "if command -v sha256sum >/dev/null 2>&1; then sha256sum | awk '{print $1}'; "
        "elif command -v busybox >/dev/null 2>&1; then busybox sha256sum | awk '{print $1}'; "
        "elif command -v openssl >/dev/null 2>&1; then openssl dgst -sha256 | awk '{print $NF}'; "
        "else echo ''; fi"
    )
    return docker_exec_capture(docker, container, script).strip()


def file_udp_payload_sha256_from_ipv4_stream(docker, container, path, expected_bytes):
    local_copy = RUNTIME_DIR / "ip_connect_ip_sink.bin"
    subprocess.run([docker, "cp", f"{container}:{path}", str(local_copy)], check=True)
    data = local_copy.read_bytes()
    pos = 0
    payload = bytearray()
    while pos + 28 <= len(data) and len(payload) < expected_bytes:
        first = data[pos]
        if (first >> 4) != 4:
            return ""
        ihl = (first & 0x0F) * 4
        if ihl < 20 or pos + ihl + 8 > len(data):
            return ""
        total_len = int.from_bytes(data[pos + 2:pos + 4], "big")
        if total_len < ihl + 8 or pos + total_len > len(data):
            return ""
        if data[pos + 9] != 17:
            return ""
        udp_len = int.from_bytes(data[pos + ihl + 4:pos + ihl + 6], "big")
        udp_payload_start = pos + ihl + 8
        udp_payload_end = pos + total_len
        if udp_len >= 8:
            udp_payload_end = min(udp_payload_end, pos + ihl + udp_len)
        if udp_payload_start > udp_payload_end:
            return ""
        payload.extend(data[udp_payload_start:udp_payload_end])
        pos += total_len
    if len(payload) < expected_bytes:
        return ""
    return hashlib.sha256(bytes(payload[:expected_bytes])).hexdigest()


def file_mixed_payload_sha256_from_ipv4_stream(docker, container, path, expected_bytes):
    local_copy = RUNTIME_DIR / "ip_connect_ip_sink.bin"
    subprocess.run([docker, "cp", f"{container}:{path}", str(local_copy)], check=True)
    data = local_copy.read_bytes()
    payload = bytearray()
    pos = 0
    parsed_packets = 0
    scan_limit = max(64, len(data) // 4)
    scans = 0
    while pos < len(data) and len(payload) < expected_bytes:
        if pos + 28 <= len(data):
            first = data[pos]
            if (first >> 4) == 4:
                ihl = (first & 0x0F) * 4
                if ihl >= 20 and pos + ihl + 8 <= len(data):
                    total_len = int.from_bytes(data[pos + 2:pos + 4], "big")
                    proto = data[pos + 9]
                    if total_len >= ihl + 8 and pos + total_len <= len(data) and proto == 17:
                        udp_len = int.from_bytes(data[pos + ihl + 4:pos + ihl + 6], "big")
                        udp_payload_start = pos + ihl + 8
                        udp_payload_end = pos + total_len
                        if udp_len >= 8:
                            udp_payload_end = min(udp_payload_end, pos + ihl + udp_len)
                        if udp_payload_start <= udp_payload_end:
                            payload.extend(data[udp_payload_start:udp_payload_end])
                            parsed_packets += 1
                            pos += total_len
                            continue
        payload.append(data[pos])
        pos += 1
        scans += 1
        if scans > scan_limit and parsed_packets == 0:
            return ""
    if len(payload) < expected_bytes or parsed_packets == 0:
        return ""
    return hashlib.sha256(bytes(payload[:expected_bytes])).hexdigest()


def file_chunk_payload_sha256(docker, container, path, chunk_size, prefix_len, expected_bytes):
    if chunk_size <= prefix_len or prefix_len <= 0:
        return ""
    local_copy = RUNTIME_DIR / "ip_connect_ip_sink.bin"
    subprocess.run([docker, "cp", f"{container}:{path}", str(local_copy)], check=True)
    data = local_copy.read_bytes()
    payload = bytearray()
    for pos in range(0, len(data), chunk_size):
        chunk = data[pos:pos + chunk_size]
        if len(chunk) <= prefix_len:
            continue
        payload.extend(chunk[prefix_len:])
        if len(payload) >= expected_bytes:
            break
    if len(payload) < expected_bytes:
        return ""
    return hashlib.sha256(bytes(payload[:expected_bytes])).hexdigest()


def transfer_metrics(byte_count, got, elapsed_sec):
    loss_bytes = max(0, byte_count - got)
    loss_pct = (loss_bytes / byte_count * 100.0) if byte_count > 0 else 0.0
    throughput_mbps = (got * 8.0 / elapsed_sec / 1_000_000.0) if elapsed_sec > 0 else 0.0
    return {
        "loss_bytes": loss_bytes,
        "loss_pct": round(loss_pct, 4),
        "throughput_mbps": round(throughput_mbps, 3),
    }


def classify_error(err):
    if not err:
        return "none"
    low = str(err).lower()
    if "timeout" in low or "exit status 143" in low or "exit status 124" in low:
        return "timeout"
    if "connection refused" in low:
        return "dial_refused"
    if "closed network connection" in low:
        return "lifecycle_closed"
    return "unknown"


def strict_timeout_sec(byte_count, floor_sec=1):
    mb = byte_count / (1024 * 1024)
    return max(floor_sec, int(math.ceil(mb)))


def _zero_observability_snapshot():
    return {
        "connect_ip_obs_contract_version": "v1",
        "connect_ip_session_id": "",
        "connect_ip_emit_seq": 0,
        "connect_ip_ptb_rx_total": 0,
        "connect_ip_packet_write_fail_total": 0,
        "connect_ip_packet_write_fail_reason_total": {},
        "connect_ip_packet_read_exit_total": 0,
        "connect_ip_packet_read_drop_reason_total": {},
        "connect_ip_packet_tx_total": 0,
        "connect_ip_packet_rx_total": 0,
        "connect_ip_bytes_tx_total": 0,
        "connect_ip_bytes_rx_total": 0,
        "connect_ip_netstack_read_inject_total": 0,
        "connect_ip_netstack_read_drop_invalid_total": 0,
        "connect_ip_netstack_write_dequeued_total": 0,
        "connect_ip_netstack_write_attempt_total": 0,
        "connect_ip_netstack_write_success_total": 0,
        "connect_ip_bypass_listenpacket_total": 0,
        "connect_ip_open_session_total": 0,
        "connect_ip_engine_ingress_total": 0,
        "connect_ip_engine_classified_total": 0,
        "connect_ip_engine_drop_total": 0,
        "connect_ip_engine_drop_reason_total": {},
        "connect_ip_engine_icmp_feedback_total": 0,
        "connect_ip_session_reset_total": {},
    }


def _normalize_observability_snapshot(snapshot):
    base = _zero_observability_snapshot()
    if not isinstance(snapshot, dict):
        return base
    contract_version = snapshot.get("connect_ip_obs_contract_version")
    if isinstance(contract_version, str) and contract_version.strip():
        base["connect_ip_obs_contract_version"] = contract_version.strip()
    session_id = snapshot.get("connect_ip_session_id")
    if isinstance(session_id, str):
        base["connect_ip_session_id"] = session_id.strip()
    emit_seq = snapshot.get("connect_ip_emit_seq", 0)
    base["connect_ip_emit_seq"] = int(emit_seq) if isinstance(emit_seq, (int, float)) else 0
    for key in base:
        if key in {
            "connect_ip_obs_contract_version",
            "connect_ip_session_id",
            "connect_ip_emit_seq",
            "connect_ip_session_reset_total",
            "connect_ip_packet_write_fail_reason_total",
            "connect_ip_packet_read_drop_reason_total",
            "connect_ip_engine_drop_reason_total",
        }:
            continue
        value = snapshot.get(key, 0)
        base[key] = int(value) if isinstance(value, (int, float)) else 0
    reset_map = snapshot.get("connect_ip_session_reset_total", {})
    if isinstance(reset_map, dict):
        normalized = {}
        for key, value in reset_map.items():
            if isinstance(value, (int, float)):
                normalized[str(key)] = int(value)
        base["connect_ip_session_reset_total"] = normalized
    write_reason_map = snapshot.get("connect_ip_packet_write_fail_reason_total", {})
    if isinstance(write_reason_map, dict):
        normalized = {}
        for key, value in write_reason_map.items():
            if isinstance(value, (int, float)):
                normalized[str(key)] = int(value)
        base["connect_ip_packet_write_fail_reason_total"] = normalized
    read_reason_map = snapshot.get("connect_ip_packet_read_drop_reason_total", {})
    if isinstance(read_reason_map, dict):
        normalized = {}
        for key, value in read_reason_map.items():
            if isinstance(value, (int, float)):
                normalized[str(key)] = int(value)
        base["connect_ip_packet_read_drop_reason_total"] = normalized
    engine_drop_map = snapshot.get("connect_ip_engine_drop_reason_total", {})
    if isinstance(engine_drop_map, dict):
        normalized = {}
        for key, value in engine_drop_map.items():
            if isinstance(value, (int, float)):
                normalized[str(key)] = int(value)
        base["connect_ip_engine_drop_reason_total"] = normalized
    return base


def _parse_connect_ip_snapshot_from_logs(log_text):
    marker = "CONNECT_IP_OBS "
    latest = None
    for line in (log_text or "").splitlines():
        idx = line.find(marker)
        if idx < 0:
            continue
        payload = line[idx + len(marker):].strip()
        try:
            latest = json.loads(payload)
        except json.JSONDecodeError:
            continue
    return _normalize_observability_snapshot(latest)


def _parse_connect_ip_snapshots_from_logs(log_text):
    marker = "CONNECT_IP_OBS "
    snapshots = []
    for line in (log_text or "").splitlines():
        idx = line.find(marker)
        if idx < 0:
            continue
        payload = line[idx + len(marker):].strip()
        try:
            snapshots.append(_normalize_observability_snapshot(json.loads(payload)))
        except json.JSONDecodeError:
            continue
    return snapshots


def _snapshot_window(before_logs, after_logs):
    before_snapshots = _parse_connect_ip_snapshots_from_logs(before_logs)
    after_snapshots = _parse_connect_ip_snapshots_from_logs(after_logs)
    before_snapshot = before_snapshots[-1] if before_snapshots else _zero_observability_snapshot()
    after_index = len(before_snapshots)
    if len(after_snapshots) > after_index:
        after_snapshot = after_snapshots[-1]
        return before_snapshot, after_snapshot, True
    after_snapshot = after_snapshots[-1] if after_snapshots else _zero_observability_snapshot()
    return before_snapshot, after_snapshot, False


def _marker_seen(log_text):
    return "CONNECT_IP_OBS " in (log_text or "")


def _merge_observability_snapshot(primary, secondary):
    merged = _normalize_observability_snapshot(primary)
    alt = _normalize_observability_snapshot(secondary)
    for key in merged:
        if key in {
            "connect_ip_obs_contract_version",
            "connect_ip_session_id",
            "connect_ip_emit_seq",
            "connect_ip_session_reset_total",
            "connect_ip_packet_write_fail_reason_total",
            "connect_ip_packet_read_drop_reason_total",
            "connect_ip_engine_drop_reason_total",
        }:
            continue
        merged[key] = max(int(merged.get(key, 0)), int(alt.get(key, 0)))
    merged["connect_ip_obs_contract_version"] = str(
        merged.get("connect_ip_obs_contract_version", "v1")
        or alt.get("connect_ip_obs_contract_version", "v1")
    )
    if not merged.get("connect_ip_session_id"):
        merged["connect_ip_session_id"] = str(alt.get("connect_ip_session_id", "") or "")
    merged["connect_ip_emit_seq"] = max(
        int(merged.get("connect_ip_emit_seq", 0)),
        int(alt.get("connect_ip_emit_seq", 0)),
    )
    for map_key in (
        "connect_ip_session_reset_total",
        "connect_ip_packet_write_fail_reason_total",
        "connect_ip_packet_read_drop_reason_total",
        "connect_ip_engine_drop_reason_total",
    ):
        result = dict(merged.get(map_key, {}))
        for reason, value in dict(alt.get(map_key, {})).items():
            result[str(reason)] = max(int(result.get(reason, 0)), int(value))
        merged[map_key] = result
    return merged


def _fallback_observability_from_logs(client_logs, server_logs):
    session_reset = {}
    route_closed = server_logs.count("masque connect-ip route closed")
    if route_closed > 0:
        session_reset["route_closed"] = route_closed
    return {
        "connect_ip_ptb_rx_total": client_logs.count("DATAGRAM frame too large"),
        "connect_ip_packet_write_fail_total": server_logs.count("proxying send side to"),
        "connect_ip_packet_read_exit_total": server_logs.count("reading from request stream failed"),
        "connect_ip_packet_tx_total": 0,
        "connect_ip_packet_rx_total": 0,
        "connect_ip_bytes_tx_total": 0,
        "connect_ip_bytes_rx_total": 0,
        "connect_ip_session_reset_total": session_reset,
    }


def _diff_observability(before, after):
    delta = _zero_observability_snapshot()
    delta["connect_ip_obs_contract_version"] = after.get("connect_ip_obs_contract_version", "v1")
    delta["connect_ip_session_id"] = str(after.get("connect_ip_session_id", "") or "")
    delta["connect_ip_emit_seq"] = max(0, int(after.get("connect_ip_emit_seq", 0)) - int(before.get("connect_ip_emit_seq", 0)))
    for key in delta:
        if key in {
            "connect_ip_obs_contract_version",
            "connect_ip_session_id",
            "connect_ip_emit_seq",
            "connect_ip_session_reset_total",
            "connect_ip_packet_write_fail_reason_total",
            "connect_ip_packet_read_drop_reason_total",
            "connect_ip_engine_drop_reason_total",
        }:
            continue
        delta[key] = max(0, int(after.get(key, 0)) - int(before.get(key, 0)))
    before_reset = before.get("connect_ip_session_reset_total", {})
    after_reset = after.get("connect_ip_session_reset_total", {})
    reasons = {}
    for reason, value in after_reset.items():
        diff = int(value) - int(before_reset.get(reason, 0))
        if diff > 0:
            reasons[reason] = diff
    delta["connect_ip_session_reset_total"] = reasons
    before_write = before.get("connect_ip_packet_write_fail_reason_total", {})
    after_write = after.get("connect_ip_packet_write_fail_reason_total", {})
    write_reasons = {}
    for reason, value in after_write.items():
        diff = int(value) - int(before_write.get(reason, 0))
        if diff > 0:
            write_reasons[reason] = diff
    delta["connect_ip_packet_write_fail_reason_total"] = write_reasons
    before_read = before.get("connect_ip_packet_read_drop_reason_total", {})
    after_read = after.get("connect_ip_packet_read_drop_reason_total", {})
    read_reasons = {}
    for reason, value in after_read.items():
        diff = int(value) - int(before_read.get(reason, 0))
        if diff > 0:
            read_reasons[reason] = diff
    delta["connect_ip_packet_read_drop_reason_total"] = read_reasons
    before_engine_drop = before.get("connect_ip_engine_drop_reason_total", {})
    after_engine_drop = after.get("connect_ip_engine_drop_reason_total", {})
    engine_drop_reasons = {}
    for reason, value in after_engine_drop.items():
        diff = int(value) - int(before_engine_drop.get(reason, 0))
        if diff > 0:
            engine_drop_reasons[reason] = diff
    delta["connect_ip_engine_drop_reason_total"] = engine_drop_reasons
    return delta


def connect_ip_observability(docker, before_client_logs, before_server_logs):
    after_client_logs = docker_logs_capture(docker, CLIENT_CONTAINER)
    after_server_logs = docker_logs_capture(docker, SERVER_CONTAINER)
    before_client_snapshot, after_client_snapshot, client_window_seen = _snapshot_window(
        before_client_logs,
        after_client_logs,
    )
    before_server_snapshot, after_server_snapshot, server_window_seen = _snapshot_window(
        before_server_logs,
        after_server_logs,
    )
    before = _merge_observability_snapshot(
        before_client_snapshot,
        before_server_snapshot,
    )
    after = _merge_observability_snapshot(
        after_client_snapshot,
        after_server_snapshot,
    )
    source = "runtime_snapshot_log_marker"
    runtime_marker_seen = (
        _marker_seen(after_client_logs)
        or _marker_seen(after_server_logs)
        or client_window_seen
        or server_window_seen
    )
    if before == _zero_observability_snapshot() and after == _zero_observability_snapshot():
        source = "runtime_marker_missing_fallback"
        before = _normalize_observability_snapshot(
            _fallback_observability_from_logs(before_client_logs, before_server_logs)
        )
        after = _normalize_observability_snapshot(
            _fallback_observability_from_logs(after_client_logs, after_server_logs)
        )
    delta = _diff_observability(before, after)
    numeric_delta_nonzero = any(
        int(delta.get(key, 0)) > 0
        for key in delta
        if key not in {
            "connect_ip_obs_contract_version",
            "connect_ip_session_id",
            "connect_ip_emit_seq",
            "connect_ip_session_reset_total",
            "connect_ip_packet_write_fail_reason_total",
            "connect_ip_packet_read_drop_reason_total",
        }
    )
    map_delta_nonzero = any(delta.get("connect_ip_session_reset_total", {}).values()) or any(
        delta.get("connect_ip_packet_write_fail_reason_total", {}).values()
    ) or any(delta.get("connect_ip_packet_read_drop_reason_total", {}).values())
    observability_gap = runtime_marker_seen and not (numeric_delta_nonzero or map_delta_nonzero)
    return {
        "source": source,
        "runtime_marker_seen": runtime_marker_seen,
        "observability_gap": observability_gap,
        "before": before,
        "after": after,
        "delta": delta,
    }


def classify_tcp_ip_stop_reason(send_err, got, expected, hash_ok, settled, budget_exceeded, obs):
    obs_delta = obs.get("delta", {}) if isinstance(obs, dict) else {}
    obs_source = obs.get("source", "") if isinstance(obs, dict) else ""
    obs_gap = bool(obs.get("observability_gap", False)) if isinstance(obs, dict) else False
    reset_delta = obs_delta.get("connect_ip_session_reset_total", {}) if isinstance(obs_delta, dict) else {}
    if send_err:
        return classify_error(send_err)
    if obs_source == "runtime_snapshot_log_marker":
        if (
            obs_delta.get("connect_ip_bypass_listenpacket_total", 0) > 0
            and obs_delta.get("connect_ip_packet_tx_total", 0) == 0
            and obs_delta.get("connect_ip_packet_rx_total", 0) == 0
        ):
            return "bypass_path_connect_udp"
        if reset_delta.get("write_fail_retry_exhausted", 0) > 0:
            return "session_reset_write_fail_retry_exhausted"
        if reset_delta.get("write_fail_fatal", 0) > 0:
            return "session_reset_write_fail_fatal"
        if reset_delta.get("read_exit", 0) > 0:
            return "session_reset_read_exit"
        if reset_delta.get("read_retry_exhausted", 0) > 0:
            return "session_reset_read_retry_exhausted"
        if reset_delta.get("lifecycle_close", 0) > 0:
            return "session_reset_lifecycle_close"
        if reset_delta.get("hop_advance", 0) > 0:
            return "session_reset_hop_advance"
        if obs_delta.get("connect_ip_packet_write_fail_total", 0) > 0:
            return "write_fail"
        if obs_delta.get("connect_ip_packet_read_exit_total", 0) > 0:
            return "read_exit"
        if obs_delta.get("connect_ip_ptb_rx_total", 0) > 0:
            return "ptb"
    if budget_exceeded:
        return "budget_exceeded"
    if got < expected:
        return "receiver_incomplete"
    if not settled:
        return "receiver_not_settled"
    if not hash_ok:
        return "hash_mismatch"
    if obs_gap:
        return "observability_gap"
    if obs_delta.get("connect_ip_ptb_rx_total", 0) > 0:
        return "ptb"
    if obs_delta.get("connect_ip_packet_write_fail_total", 0) > 0:
        return "write_fail"
    if obs_delta.get("connect_ip_packet_read_exit_total", 0) > 0:
        return "read_exit"
    if reset_delta.get("write_fail_retry_exhausted", 0) > 0:
        return "session_reset_write_fail_retry_exhausted"
    if reset_delta.get("write_fail_fatal", 0) > 0:
        return "session_reset_write_fail_fatal"
    if reset_delta.get("read_exit", 0) > 0:
        return "session_reset_read_exit"
    if reset_delta.get("read_retry_exhausted", 0) > 0:
        return "session_reset_read_retry_exhausted"
    if reset_delta.get("lifecycle_close", 0) > 0:
        return "session_reset_lifecycle_close"
    if reset_delta.get("hop_advance", 0) > 0:
        return "session_reset_hop_advance"
    return "none"


def _connect_ip_accounting_confirmed(obs):
    if not isinstance(obs, dict):
        return False
    if obs.get("source") != "runtime_snapshot_log_marker":
        return False
    delta = obs.get("delta", {})
    if not isinstance(delta, dict):
        return False
    if int(delta.get("connect_ip_bypass_listenpacket_total", 0)) > 0:
        return False
    packet_tx = int(delta.get("connect_ip_packet_tx_total", 0))
    bytes_tx = int(delta.get("connect_ip_bytes_tx_total", 0))
    return packet_tx > 0 and bytes_tx > 0


def run_udp(docker, byte_count):
    target_host, port = "10.200.0.3", 5601
    sink = "/tmp/udp-python.bin"
    docker_exec(docker, SERVER_CONTAINER, f"rm -f {sink}", check=False)
    docker_exec(
        docker,
        SERVER_CONTAINER,
        f"nohup timeout 15 socat -u -T1 UDP-RECVFROM:{port},reuseaddr,fork OPEN:{sink},creat,append >/tmp/udp-python.log 2>&1 &",
    )
    wait_udp_listener(docker, SERVER_CONTAINER, port)

    start = time.time()
    chunk = 1024
    count = byte_count // chunk
    send_cmd = (
        f"timeout 20 sh -lc 'ip route add 10.200.0.0/24 dev tun0 2>/dev/null || true; "
        f"for i in $(seq 1 {count}); do dd if=/dev/zero bs={chunk} count=1 2>/dev/null | socat -u - UDP:{target_host}:{port}; done'"
    )
    docker_exec(docker, CLIENT_CONTAINER, send_cmd)
    elapsed = time.time() - start
    got = wait_for_bytes(docker, SERVER_CONTAINER, sink, byte_count, 10 if byte_count == BYTES_10KB else 30)
    ok = got >= byte_count and elapsed <= SMOKE_DEADLINE_SEC if byte_count == BYTES_10KB else got >= byte_count
    return {"scenario": "udp", "bytes_expected": byte_count, "bytes_received": got, "elapsed_sec": round(elapsed, 3), "ok": ok}


def run_tcp_stream(docker, byte_count):
    target_host, port = "10.200.0.3", 5602
    sink = "/tmp/tcp-stream-python.bin"
    docker_exec(docker, SERVER_CONTAINER, f"rm -f {sink}", check=False)
    docker_exec(
        docker,
        SERVER_CONTAINER,
        f"nohup timeout 20 socat -u TCP-LISTEN:{port},reuseaddr,fork OPEN:{sink},creat,append >/tmp/tcp-stream-python.log 2>&1 &",
    )
    wait_tcp_listener(docker, SERVER_CONTAINER, port)
    wait_socks_ready(docker)
    time.sleep(0.35)

    start = time.time()
    try:
        run(
            [
                docker,
                "exec",
                "-e",
                f"MASQUE_TCPSEND_BYTES={byte_count}",
                "-e",
                f"MASQUE_DST_HOST={target_host}",
                "-e",
                f"MASQUE_DST_PORT={port}",
                CLIENT_CONTAINER,
                "python3",
                "-c",
                _TCP_STREAM_SEND_SOCKS5,
            ],
            cwd=ROOT,
        )
    except subprocess.CalledProcessError:
        time.sleep(0.35)
        run(
            [
                docker,
                "exec",
                "-e",
                f"MASQUE_TCPSEND_BYTES={byte_count}",
                "-e",
                f"MASQUE_DST_HOST={target_host}",
                "-e",
                f"MASQUE_DST_PORT={port}",
                CLIENT_CONTAINER,
                "python3",
                "-c",
                _TCP_STREAM_SEND_SOCKS5,
            ],
            cwd=ROOT,
        )
    elapsed = time.time() - start
    got = wait_for_bytes(docker, SERVER_CONTAINER, sink, byte_count, 20 if byte_count == BYTES_10KB else 30)
    ok = got >= byte_count and elapsed <= SMOKE_DEADLINE_SEC if byte_count == BYTES_10KB else got >= byte_count
    return {"scenario": "tcp_stream", "bytes_expected": byte_count, "bytes_received": got, "elapsed_sec": round(elapsed, 3), "ok": ok}


def run_tcp_ip(
    docker,
    byte_count,
    mode="churn_many_flows",
    send_timeout_sec=None,
    wait_timeout_sec=None,
    tcp_ip_deadline_sec=None,
):
    # TUN-only hard switch: validate CONNECT-IP packet-plane directly.
    target_host, port = "10.200.0.2", 5601
    sink = "/tmp/ip-connect-ip-python.bin"
    raw_datagram_size = int(os.environ.get("MASQUE_TCP_IP_DATAGRAM", "1172"))
    payload_cap = int(os.environ.get("MASQUE_TCP_IP_UDP_PAYLOAD_CAP", "1172"))
    if payload_cap < 512:
        payload_cap = 512
    datagram_size = min(raw_datagram_size, payload_cap)
    if datagram_size < 1172:
        datagram_size = 1172
    docker_exec(docker, IPERF_CONTAINER, f"rm -f {sink}", check=False)
    listener_timeout = 20 if byte_count == BYTES_10KB else 1800
    recv_timeout_opt = "-T1 " if byte_count == BYTES_10KB else ""
    docker_exec(
        docker,
        IPERF_CONTAINER,
        (
            f"nohup timeout {listener_timeout} "
            f"socat -u {recv_timeout_opt}UDP-RECV:{port},reuseaddr,rcvbuf=16777216 OPEN:{sink},creat,trunc,append "
            f">/tmp/ip-connect-ip-python.log 2>&1 &"
        ),
    )
    wait_udp_listener(docker, IPERF_CONTAINER, port)

    start = time.time()
    strict_budget = 3 if byte_count == BYTES_10KB else strict_timeout_sec(byte_count, floor_sec=1)
    if byte_count > BYTES_10KB and tcp_ip_deadline_sec is not None:
        strict_budget = max(1, int(tcp_ip_deadline_sec))
    if byte_count > BYTES_10KB and mode == "bulk_single_flow":
        phase_slack = min(
            BULK_SINGLE_FLOW_RECEIVE_TAIL_CAP_SEC,
            BULK_SINGLE_FLOW_RECEIVE_TAIL_BASE_SEC + strict_budget * BULK_SINGLE_FLOW_RECEIVE_TAIL_PER_STRICT_SEC,
        )
    else:
        phase_slack = 0.0
    phase_deadline = start + strict_budget + phase_slack
    send_timeout = send_timeout_sec if send_timeout_sec is not None else strict_budget
    if mode == "churn_many_flows":
        chunk = 1024
        count = max(1, byte_count // chunk)
        send_cmd = (
            f"timeout {send_timeout} sh -lc 'ip route add 10.200.0.0/24 dev tun0 2>/dev/null || true; "
            f"for i in $(seq 1 {count}); do dd if=/dev/zero bs={chunk} count=1 2>/dev/null | "
            f"socat -u - UDP:{target_host}:{port} || exit 1; done'"
        )
    else:
        send_rate_limit = os.environ.get("MASQUE_TCP_IP_RATE_LIMIT", "").strip()
        if not send_rate_limit and byte_count > BYTES_10KB:
            send_rate_limit = TCP_IP_BULK_DEFAULT_RATE_LIMIT
        # pv interprets "m" as decimal MB/s. Strict budget uses MiB sizing.
        # Normalize the common "1m" knob to a higher decimal rate so
        # strict 1 MiB/s budgets remain reachable despite UDP framing
        # and userspace pipeline overhead in the stand.
        if send_rate_limit == "1m":
            send_rate_limit = "1300k"
        rate_bps = parse_rate_limit_to_bps(send_rate_limit)
        send_cmd = None
    before_client_logs = docker_logs_capture(docker, CLIENT_CONTAINER)
    before_server_logs = docker_logs_capture(docker, SERVER_CONTAINER)
    send_err = None
    try:
        if mode == "churn_many_flows":
            docker_exec(docker, CLIENT_CONTAINER, send_cmd)
        else:
            docker_exec(docker, CLIENT_CONTAINER, "ip route add 10.200.0.0/24 dev tun0 2>/dev/null || true")
            run(
                [
                    docker,
                    "exec",
                    "-e",
                    f"MASQUE_UDP_SEND_BYTES={byte_count}",
                    "-e",
                    f"MASQUE_UDP_DEST_HOST={target_host}",
                    "-e",
                    f"MASQUE_UDP_DEST_PORT={port}",
                    "-e",
                    f"MASQUE_UDP_CHUNK={datagram_size}",
                    "-e",
                    f"MASQUE_UDP_RATE_BPS={rate_bps}",
                    "-e",
                    "MASQUE_UDP_SNDBUF=16777216",
                    CLIENT_CONTAINER,
                    "timeout",
                    str(send_timeout),
                    "python3",
                    "-c",
                    _TCP_IP_SEND_UDP_PACED,
                ],
                cwd=ROOT,
            )
    except subprocess.CalledProcessError as exc:
        send_err = str(exc)
    send_elapsed = time.time() - start
    if wait_timeout_sec is not None:
        wait_timeout = wait_timeout_sec
    else:
        wait_timeout = max(0.0, phase_deadline - time.time())
    got = wait_for_bytes(docker, IPERF_CONTAINER, sink, byte_count, wait_timeout)
    bytes_at_deadline = got
    if byte_count > BYTES_10KB and mode == "bulk_single_flow" and got < byte_count:
        for _ in range(8):
            rescue_budget = min(
                BULK_SINGLE_FLOW_NEAR_COMPLETE_DRAIN_SEC,
                max(0.0, phase_deadline - time.time()),
            )
            if rescue_budget <= 0.05 or got >= byte_count:
                break
            got = max(
                got,
                wait_for_bytes(docker, IPERF_CONTAINER, sink, byte_count, rescue_budget),
            )
    settle_timeout = max(0.0, phase_deadline - time.time())
    if byte_count > BYTES_10KB and mode == "bulk_single_flow" and got >= byte_count:
        settle_timeout = max(settle_timeout, 2.0)
    settled_got, settled = wait_for_stable_size(
        docker,
        IPERF_CONTAINER,
        sink,
        byte_count,
        settle_timeout,
        checks=3 if byte_count == BYTES_10KB else 5,
        interval_sec=0.2,
    )
    got = max(got, settled_got)
    late_growth_bytes = max(0, got - bytes_at_deadline)
    expected_hash = zero_payload_sha256(byte_count)
    actual_hash = ""
    hash_normalized = False
    if settled and got == byte_count:
        actual_hash = file_sha256(docker, IPERF_CONTAINER, sink)
    elif settled and got > byte_count:
        # UDP append sink can exceed expected on duplicate datagrams; validate prefix only.
        actual_hash = file_sha256_slice(docker, IPERF_CONTAINER, sink, 0, byte_count)
        if actual_hash != "" and actual_hash == expected_hash:
            hash_normalized = True
    hash_ok = actual_hash != "" and actual_hash == expected_hash
    if not hash_ok and settled and got == byte_count + 28:
        sliced_hash = file_sha256_slice(docker, IPERF_CONTAINER, sink, 28, byte_count)
        if sliced_hash != "" and sliced_hash == expected_hash:
            actual_hash = sliced_hash
            hash_ok = True
            hash_normalized = True
    if not hash_ok and settled and got >= byte_count + 28:
        stream_payload_hash = file_udp_payload_sha256_from_ipv4_stream(docker, IPERF_CONTAINER, sink, byte_count)
        if stream_payload_hash != "" and stream_payload_hash == expected_hash:
            actual_hash = stream_payload_hash
            hash_ok = True
            hash_normalized = True
    if not hash_ok and settled and got >= byte_count + 28:
        chunk_payload_hash = file_chunk_payload_sha256(
            docker,
            IPERF_CONTAINER,
            sink,
            datagram_size,
            28,
            byte_count,
        )
        if chunk_payload_hash != "" and chunk_payload_hash == expected_hash:
            actual_hash = chunk_payload_hash
            hash_ok = True
            hash_normalized = True
    if not hash_ok and settled and got >= byte_count + 28:
        mixed_payload_hash = file_mixed_payload_sha256_from_ipv4_stream(
            docker,
            IPERF_CONTAINER,
            sink,
            byte_count,
        )
        if mixed_payload_hash != "" and mixed_payload_hash == expected_hash:
            actual_hash = mixed_payload_hash
            hash_ok = True
            hash_normalized = True
    elapsed = time.time() - start
    metrics = transfer_metrics(byte_count, got, elapsed)
    error_class = classify_error(send_err)
    observability = connect_ip_observability(docker, before_client_logs, before_server_logs)
    accounting_confirmed = _connect_ip_accounting_confirmed(observability)
    # Sender-side gate stays strict_budget (`timeout` on client). Receiver drain/settle
    # can extend wall clock; do not fold total elapsed into throughput_ok.
    throughput_ok = send_err is None and send_elapsed <= strict_budget and (
        got == byte_count or (got > byte_count and hash_ok)
    )
    budget_exceeded = not throughput_ok
    stop_reason = classify_tcp_ip_stop_reason(send_err, got, byte_count, hash_ok, settled, budget_exceeded, observability)
    if send_err:
        stop_reason_source = "runner_guard"
    elif stop_reason in {"budget_exceeded", "receiver_incomplete", "receiver_not_settled", "hash_mismatch"}:
        stop_reason_source = "runner_integrity"
    elif stop_reason == "none":
        stop_reason_source = "none"
    else:
        stop_reason_source = "runtime_observability"
    obs_delta = observability.get("delta", {}) if isinstance(observability, dict) else {}
    reset_delta = obs_delta.get("connect_ip_session_reset_total", {}) if isinstance(obs_delta, dict) else {}
    runtime_health_ok = (
        int(obs_delta.get("connect_ip_packet_write_fail_total", 0)) == 0
        and int(obs_delta.get("connect_ip_packet_read_exit_total", 0)) == 0
        and sum(int(v) for v in reset_delta.values()) == 0
    )
    observability_ok = (
        accounting_confirmed
        and int(obs_delta.get("connect_ip_bypass_listenpacket_total", 0)) == 0
        and runtime_health_ok
    )
    integrity_ok = hash_ok and settled
    stop_reason_evidence = {
        "budget_exceeded": budget_exceeded,
        "bytes_expected": byte_count,
        "bytes_received": got,
        "receiver_settled": settled,
        "hash_ok": hash_ok,
        "observability_delta": observability.get("delta", {}),
        "accounting_confirmed": accounting_confirmed,
        "throughput_ok": throughput_ok,
        "integrity_ok": integrity_ok,
        "observability_ok": observability_ok,
        "runtime_health_ok": runtime_health_ok,
        "transfer_elapsed_sec": round(send_elapsed, 3),
    }
    budget_margin_sec = round(strict_budget + phase_slack - elapsed, 3)
    if byte_count == BYTES_10KB:
        throughput_ok = throughput_ok and send_elapsed <= SMOKE_DEADLINE_SEC
    ok = throughput_ok and integrity_ok and observability_ok
    return {
        "scenario": "tcp_ip",
        "mode": mode,
        "bytes_expected": byte_count,
        "bytes_received": got,
        "elapsed_sec": round(elapsed, 3),
        "transfer_elapsed_sec": round(send_elapsed, 3),
        "hash_expected_sha256": expected_hash,
        "hash_actual_sha256": actual_hash,
        "hash_ok": hash_ok,
        "hash_normalized": hash_normalized,
        "metrics": metrics,
        "timeout_budget_sec": strict_budget,
        "receive_phase_slack_sec": phase_slack,
        "datagram_size_raw": raw_datagram_size,
        "datagram_size_effective": datagram_size,
        "send_guard_timeout_sec": send_timeout,
        "wait_guard_timeout_sec": wait_timeout,
        "send_elapsed_sec": round(send_elapsed, 3),
        "receiver_settled": settled,
        "bytes_at_deadline": bytes_at_deadline,
        "late_growth_bytes": late_growth_bytes,
        "budget_margin_sec": budget_margin_sec,
        "stop_reason": stop_reason,
        "stop_reason_source": stop_reason_source,
        "stop_reason_evidence": stop_reason_evidence,
        "observability": observability,
        "error": send_err,
        "error_class": error_class,
        "ok": ok,
    }


def run_scenario(docker, scenario, byte_count, tcp_ip_mode, tcp_ip_deadline_sec=None):
    if scenario == "udp":
        compose_up(docker, DEFAULT_CLIENT_CONFIG)
        return run_udp(docker, byte_count)
    if scenario == "tcp_stream":
        compose_up(docker, DEFAULT_CLIENT_CONFIG)
        return run_tcp_stream(docker, byte_count)
    if scenario == "tcp_ip":
        compose_up(docker, CONNECT_IP_CLIENT_CONFIG)
        return run_tcp_ip(docker, byte_count, mode=tcp_ip_mode, tcp_ip_deadline_sec=tcp_ip_deadline_sec)
    raise ValueError(f"unsupported scenario: {scenario}")


def main():
    parser = argparse.ArgumentParser(description="Single entrypoint for MASQUE stand scenarios")
    parser.add_argument("--scenario", choices=["udp", "tcp_stream", "tcp_ip", "all"], required=True)
    parser.add_argument("--stress", action="store_true", help="run 500MB transfer instead of 10KB")
    parser.add_argument(
        "--megabytes",
        type=int,
        default=None,
        metavar="N",
        help="transfer size in MiB (binary: N*1024*1024). For strict bulk gates (e.g. 10/20/100). "
        "Incompatible with --stress (stress wins). With --scenario all, only tcp_ip runs at this size.",
    )
    parser.add_argument(
        "--tcp-ip-mode",
        choices=["churn_many_flows", "bulk_single_flow"],
        default="bulk_single_flow",
        help="tcp_ip transfer pattern (default is strict bulk single flow)",
    )
    parser.add_argument(
        "--tcp-ip-deadline-sec",
        type=int,
        default=None,
        metavar="S",
        help="override tcp_ip wall-clock budget (default: ceil(MiB) for bulk; use on slow hosts to verify hash/integrity)",
    )
    args = parser.parse_args()

    if args.stress and args.megabytes is not None:
        print("Note: --stress forces 500MB; ignoring --megabytes", flush=True)
    if args.megabytes is not None and args.megabytes < 1:
        parser.error("--megabytes must be >= 1")

    docker = docker_bin()
    if args.stress:
        byte_count = BYTES_500MB
    elif args.megabytes is not None:
        byte_count = bytes_from_megabytes_arg(args.megabytes)
    else:
        byte_count = BYTES_10KB

    if args.scenario == "all":
        scenarios = ["udp", "tcp_stream", "tcp_ip"]
        if byte_count > BYTES_10KB:
            scenarios = ["tcp_ip"]
            print(
                "Note: bulk size >10KB with --scenario all runs tcp_ip only "
                "(udp/tcp_stream harness is smoke-sized).",
                flush=True,
            )
    else:
        scenarios = [args.scenario]

    RUNTIME_DIR.mkdir(parents=True, exist_ok=True)
    compile_singbox()

    results = []
    overall_ok = True
    for scenario in scenarios:
        print(f"\n=== Running scenario: {scenario} ({byte_count} bytes) ===")
        try:
            result = run_scenario(
                docker,
                scenario,
                byte_count,
                args.tcp_ip_mode,
                tcp_ip_deadline_sec=args.tcp_ip_deadline_sec,
            )
        except Exception as exc:
            result = {"scenario": scenario, "bytes_expected": byte_count, "bytes_received": 0, "elapsed_sec": 0.0, "ok": False, "error": str(exc)}
        results.append(result)
        overall_ok = overall_ok and bool(result.get("ok"))
        print(json.dumps(result, ensure_ascii=True))

    summary = {
        "stress": args.stress,
        "megabytes_arg": args.megabytes,
        "bytes": byte_count,
        "results": results,
        "ok": overall_ok,
    }
    summary_path = RUNTIME_DIR / "masque_python_runner_summary.json"
    summary_path.write_text(json.dumps(summary, indent=2), encoding="utf-8")
    print(f"\nSummary written to: {summary_path}")
    print(json.dumps(summary, ensure_ascii=True))
    sys.exit(0 if overall_ok else 1)


if __name__ == "__main__":
    main()
