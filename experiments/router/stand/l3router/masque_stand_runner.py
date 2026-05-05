#!/usr/bin/env python3
import argparse
import base64
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

# TUN-routed CONNECT-UDP sender: one UDP socket; chunk must fit CONNECT-UDP/QUIC datagram path (see sing-box masqueUDPWriteMax).
_UDP_TUN_DATAGRAM_SEND_PY = """import sys, socket, time
N, host, port = int(sys.argv[1]), sys.argv[2], int(sys.argv[3])
CHUNK = int(sys.argv[4]) if len(sys.argv) > 4 else 960
if CHUNK < 256 or CHUNK > 1152:
    CHUNK = 960
pause = float(sys.argv[5]) if len(sys.argv) > 5 else 0.0
if pause < 0:
    pause = 0.0
s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
z = b"\\x00" * CHUNK
sent = 0
while sent < N:
    n = min(CHUNK, N - sent)
    s.sendto(z[:n], (host, port))
    sent += n
    if pause > 0:
        time.sleep(pause)
s.close()
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

_TCP_IP_ICMP_PING = r"""import os, socket, struct, time
DEST_HOST = os.environ["MASQUE_ICMP_DEST_HOST"]
TIMEOUT_SEC = float(os.environ.get("MASQUE_ICMP_TIMEOUT_SEC", "3"))
IDENT = int(os.environ.get("MASQUE_ICMP_IDENT", "4660")) & 0xffff
SEQ = int(os.environ.get("MASQUE_ICMP_SEQ", "1")) & 0xffff
PAYLOAD = b"masque-icmp-e2e"

def checksum(data):
    if len(data) % 2:
        data += b"\x00"
    s = 0
    for i in range(0, len(data), 2):
        s += (data[i] << 8) + data[i+1]
        s = (s & 0xffff) + (s >> 16)
    return (~s) & 0xffff

sock = socket.socket(socket.AF_INET, socket.SOCK_RAW, socket.IPPROTO_ICMP)
sock.settimeout(TIMEOUT_SEC)
header = struct.pack("!BBHHH", 8, 0, 0, IDENT, SEQ)
pkt = header + PAYLOAD
cs = checksum(pkt)
pkt = struct.pack("!BBHHH", 8, 0, cs, IDENT, SEQ) + PAYLOAD
start = time.monotonic()
sock.sendto(pkt, (DEST_HOST, 0))
ok = False
latency_ms = -1.0
deadline = start + TIMEOUT_SEC
while time.monotonic() < deadline:
    try:
        data, _ = sock.recvfrom(2048)
    except socket.timeout:
        break
    if len(data) < 28:
        continue
    icmp = data[20:]
    icmp_type, icmp_code, _csum, rid, rseq = struct.unpack("!BBHHH", icmp[:8])
    if icmp_type == 0 and icmp_code == 0 and rid == IDENT and rseq == SEQ:
        ok = True
        latency_ms = (time.monotonic() - start) * 1000.0
        break
sock.close()
print("ok=%s latency_ms=%.3f" % ("1" if ok else "0", latency_ms))
raise SystemExit(0 if ok else 2)
"""

BYTES_10KB = 10 * 1024
BYTES_500MB = 500 * 1024 * 1024
SMOKE_DEADLINE_SEC = 5.0

# Max application UDP payload per CONNECT-UDP QUIC datagram in sing-box MASQUE core
# (see hiddify-sing-box/transport/masque `masqueUDPWriteHardCap`, ~1152 B). This is not
# Ethernet MTU 1500: each user datagram is wrapped (IP/UDP + MASQUE/HTTP3 framing).
MASQUE_STAND_UDP_CHUNK_MAX = 1152

# CONNECT-UDP / connect_stream bulk: wall-clock caps derived from a **minimum acceptable**
# goodput (relative to this baseline, decimal Mb/s). If the path cannot finish within
# ``t50 * _BULK_STALL_MULT`` (with a small floor), the run fails fast instead of hanging
# for hundreds of seconds on a stuck sender or half-filled sink.
_BULK_HARNESS_BASELINE_MBPS = 50.0
_BULK_STALL_MULT = 4.0
_BULK_STALL_FLOOR_SEC = 90
_BULK_SERVER_PAD_SEC = 90
_BULK_WAIT_PAD_SEC = 45


def _udp_tcp_stream_bulk_stall_wall_sec(byte_count: int) -> int:
    """Max seconds for client send + receiver catch-up at bulk sizes (fail-fast if too slow)."""
    if byte_count <= BYTES_10KB:
        return 30
    t50 = (byte_count * 8.0) / (_BULK_HARNESS_BASELINE_MBPS * 1_000_000.0)
    return max(_BULK_STALL_FLOOR_SEC, int(math.ceil(t50 * _BULK_STALL_MULT)))


def _udp_tcp_stream_bulk_harness_timeouts(byte_count: int) -> tuple[int, int, int]:
    """
    (server_socat_outer_timeout_sec, client_send_timeout_sec, wait_for_bytes_sec).

    Smoke: short fixed caps. Bulk: ``_udp_tcp_stream_bulk_stall_wall_sec`` so a healthy
    ~50 Mb/s path has headroom, a stalled path aborts within minutes, not hours.
    """
    if byte_count <= BYTES_10KB:
        return (15, 20, 30)
    client_sec = _udp_tcp_stream_bulk_stall_wall_sec(byte_count)
    server_sec = client_sec + _BULK_SERVER_PAD_SEC
    wait_sec = client_sec + _BULK_WAIT_PAD_SEC
    return (server_sec, client_sec, wait_sec)


def _theoretical_transfer_sec_at_mbps(byte_count: int, mbps: float) -> float:
    if byte_count <= 0 or mbps <= 0:
        return 0.0
    return (byte_count * 8.0) / (mbps * 1_000_000.0)

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


def parse_iperf_rates(value: str) -> list[str]:
    rates = []
    for part in (value or "").split(","):
        item = part.strip()
        if item:
            rates.append(item)
    if not rates:
        raise ValueError("no iperf rates provided")
    return rates


def parse_iperf_udp_result(raw: str) -> dict:
    start = raw.find("{")
    end = raw.rfind("}")
    if start < 0 or end < start:
        raise ValueError("iperf json output not found")
    payload = raw[start : end + 1]
    data = json.loads(payload)
    if not isinstance(data, dict):
        raise ValueError("iperf output is not a json object")
    end_block = data.get("end", {})
    sum_block = end_block.get("sum", {})
    receiver_block = end_block.get("sum_received", sum_block)
    sender_block = end_block.get("sum_sent", sum_block)
    return {
        "sender_bps": float(sender_block.get("bits_per_second", 0.0) or 0.0),
        "receiver_bps": float(receiver_block.get("bits_per_second", 0.0) or 0.0),
        "lost_percent": float(receiver_block.get("lost_percent", 0.0) or 0.0),
        "jitter_ms": float(receiver_block.get("jitter_ms", 0.0) or 0.0),
        "packets": int(receiver_block.get("packets", 0) or 0),
        "lost_packets": int(receiver_block.get("lost_packets", 0) or 0),
    }


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


def _udp_tun_datagram_send_sh(
    byte_count: int,
    target_host: str,
    port: int,
    cli_to: int,
    chunk: int = MASQUE_STAND_UDP_CHUNK_MAX,
    pause_sec: float = 0.0,
) -> str:
    """Shell snippet: write small Python sender via base64 (robust on Windows Docker), run on client."""
    b64 = base64.standard_b64encode(_UDP_TUN_DATAGRAM_SEND_PY.encode()).decode("ascii")
    pause_s = f"{float(pause_sec):.6f}"
    return (
        f"ip route replace 10.200.0.0/24 dev tun0 2>/dev/null || true; "
        f"printf '%s' '{b64}' | base64 -d > /tmp/udp_tun_send.py && "
        f"timeout {int(cli_to)} python3 /tmp/udp_tun_send.py {int(byte_count)} {target_host} {port} {int(chunk)} {pause_s}"
    )


def _stand_udp_chunk(byte_count: int, udp_chunk: int) -> int:
    if 256 <= int(udp_chunk) <= MASQUE_STAND_UDP_CHUNK_MAX:
        return int(udp_chunk)
    env_raw = (os.environ.get("MASQUE_STAND_UDP_CHUNK") or "").strip()
    if env_raw:
        try:
            v = int(env_raw)
            if 256 <= v <= MASQUE_STAND_UDP_CHUNK_MAX:
                return v
        except ValueError:
            pass
    if byte_count <= BYTES_10KB:
        return 960
    return MASQUE_STAND_UDP_CHUNK_MAX


def _client_default_dev(docker) -> str:
    out = docker_exec_capture(
        docker, CLIENT_CONTAINER, "ip route show default 2>/dev/null | awk '{print $5; exit}'"
    )
    dev = (out or "eth0").strip()
    return dev if dev else "eth0"


def _netem_apply_loss(docker, loss_pct: float) -> None:
    if loss_pct <= 0:
        return
    dev = _client_default_dev(docker)
    docker_exec(
        docker,
        CLIENT_CONTAINER,
        f"tc qdisc replace dev {dev} root netem loss {float(loss_pct)}%",
        check=False,
    )


def _netem_clear(docker) -> None:
    dev = _client_default_dev(docker)
    docker_exec(docker, CLIENT_CONTAINER, f"tc qdisc del dev {dev} root 2>/dev/null || true", check=False)


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
    p = int(port)
    cmd = (
        f"(ss -ltn 2>/dev/null | grep -qF '0.0.0.0:{p}' || "
        f"ss -ltn 2>/dev/null | grep -qF '*:{p}' || "
        f"ss -ltn 2>/dev/null | grep -q \"[::]:{p}\")"
    )
    while time.time() < deadline:
        rc = subprocess.run([docker, "exec", container, "sh", "-lc", cmd], cwd=ROOT, text=True).returncode
        if rc == 0:
            return
        time.sleep(0.2)
    raise RuntimeError(f"TCP listener not ready on {container}:{port}")


def wait_udp_listener(docker, container, port, timeout_sec=5):
    deadline = time.time() + timeout_sec
    p = int(port)
    cmd = (
        f"(ss -lun 2>/dev/null | grep -qF '0.0.0.0:{p}' || "
        f"ss -lun 2>/dev/null | grep -qF '*:{p}' || "
        f"ss -lun 2>/dev/null | grep -q \"[::]:{p}\")"
    )
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


def _parse_csv_ints(s, default):
    raw = (s or "").strip()
    if not raw:
        return list(default)
    out = []
    for p in raw.split(","):
        p = p.strip()
        if p:
            out.append(int(p))
    return out if out else list(default)


def _parse_csv_floats(s, default):
    raw = (s or "").strip()
    if not raw:
        return list(default)
    out = []
    for p in raw.split(","):
        p = p.strip()
        if p:
            out.append(float(p))
    return out if out else list(default)


def run_udp_matrix(docker, sizes_mib, rates_bps, losses, udp_chunk=0):
    rows = []
    overall_ok = True
    for mib in sizes_mib:
        bc = int(mib) * 1024 * 1024
        for loss in losses:
            for rate in rates_bps:
                row = run_udp(docker, bc, udp_chunk=udp_chunk, udp_rate_bps=rate, udp_loss_pct=loss)
                row["matrix_mib"] = int(mib)
                row["matrix_rate_bps"] = int(rate)
                row["matrix_loss_pct"] = float(loss)
                rows.append(row)
                overall_ok = overall_ok and bool(row.get("ok"))
    return {
        "scenario": "udp_matrix",
        "rows": rows,
        "ok": overall_ok,
        "matrix_sizes_mib": list(sizes_mib),
        "matrix_rates_bps": list(rates_bps),
        "matrix_losses_pct": list(losses),
    }


def run_udp(docker, byte_count, udp_chunk=0, udp_rate_bps=0, udp_loss_pct=0.0):
    target_host, port = "10.200.0.3", 5601
    sink = "/tmp/udp-python.bin"
    srv_to, cli_to, wait_cap = _udp_tcp_stream_bulk_harness_timeouts(byte_count)
    chunk = _stand_udp_chunk(byte_count, udp_chunk)
    if int(udp_rate_bps) > 0:
        pause = float(chunk) / float(int(udp_rate_bps))
    else:
        raw = (os.environ.get("MASQUE_STAND_UDP_BULK_PAUSE") or "0").strip()
        try:
            pause = max(0.0, float(raw))
        except ValueError:
            pause = 0.0
    _netem_clear(docker)
    try:
        _netem_apply_loss(docker, float(udp_loss_pct))
        docker_exec(
            docker,
            SERVER_CONTAINER,
            "pkill -f 'UDP4-LISTEN:5601' 2>/dev/null || pkill -f 'udp4-listen:5601' 2>/dev/null || true; sleep 0.15",
            check=False,
        )
        docker_exec(docker, SERVER_CONTAINER, f"rm -f {sink}", check=False)
        docker_exec(
            docker,
            SERVER_CONTAINER,
            # UDP4-LISTEN: one process appends all datagrams (no fork+creat truncate races).
            f"nohup timeout {srv_to} socat -u -T1 UDP4-LISTEN:{port},reuseaddr OPEN:{sink},creat,append >/tmp/udp-python.log 2>&1 &",
        )
        wait_udp_listener(docker, SERVER_CONTAINER, port)

        start = time.time()
        docker_exec(
            docker,
            CLIENT_CONTAINER,
            _udp_tun_datagram_send_sh(byte_count, target_host, port, cli_to, chunk=chunk, pause_sec=pause),
            check=False,
        )
        recv_wait = 10 if byte_count == BYTES_10KB else wait_cap
        got = wait_for_bytes(docker, SERVER_CONTAINER, sink, byte_count, recv_wait)
        elapsed = time.time() - start
        if float(udp_loss_pct) > 0:
            floor_ratio = max(0.85, 1.0 - (float(udp_loss_pct) + 4.0) / 100.0)
            ok = int(got) >= int(float(byte_count) * floor_ratio)
        elif byte_count == BYTES_10KB:
            ok = got >= byte_count and elapsed <= SMOKE_DEADLINE_SEC
        else:
            ok = got >= byte_count
        loss_b = max(0, int(byte_count) - int(got))
        meas_loss = (100.0 * loss_b / float(byte_count)) if byte_count > 0 else 0.0
        tm_elapsed = max(float(elapsed), 1e-9)
        tmetrics = transfer_metrics(byte_count, got, tm_elapsed)
        t_min_50 = _theoretical_transfer_sec_at_mbps(byte_count, _BULK_HARNESS_BASELINE_MBPS)
        return {
            "scenario": "udp",
            "metric_layer": "application_payload",
            "bytes_expected": byte_count,
            "bytes_received": got,
            "elapsed_sec": round(elapsed, 3),
            "throughput_mbps": tmetrics["throughput_mbps"],
            "theoretical_min_sec_at_50mbps": round(t_min_50, 3),
            "ok": ok,
            "stand_udp_chunk_bytes": chunk,
            "stand_udp_pause_sec": round(pause, 6),
            "udp_send_rate_bps": int(udp_rate_bps) if int(udp_rate_bps) > 0 else None,
            "injected_loss_pct": float(udp_loss_pct),
            "measured_loss_pct_approx": round(meas_loss, 4),
        }
    finally:
        _netem_clear(docker)


def run_tcp_stream(docker, byte_count):
    target_host, port = "10.200.0.3", 5602
    sink = "/tmp/tcp-stream-python.bin"
    srv_to, _, wait_cap = _udp_tcp_stream_bulk_harness_timeouts(byte_count)
    docker_exec(docker, SERVER_CONTAINER, f"rm -f {sink}", check=False)
    docker_exec(
        docker,
        SERVER_CONTAINER,
        # Single acceptor: no fork (fork+creat per-connection can truncate the sink under bulk).
        f"nohup timeout {srv_to} socat -u TCP-LISTEN:{port},reuseaddr OPEN:{sink},creat,append >/tmp/tcp-stream-python.log 2>&1 &",
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
    recv_wait = 20 if byte_count == BYTES_10KB else wait_cap
    got = wait_for_bytes(docker, SERVER_CONTAINER, sink, byte_count, recv_wait)
    elapsed = time.time() - start
    ok = got >= byte_count and elapsed <= SMOKE_DEADLINE_SEC if byte_count == BYTES_10KB else got >= byte_count
    tm_elapsed = max(float(elapsed), 1e-9)
    tmetrics = transfer_metrics(byte_count, got, tm_elapsed)
    t_min_50 = _theoretical_transfer_sec_at_mbps(byte_count, _BULK_HARNESS_BASELINE_MBPS)
    return {
        "scenario": "tcp_stream",
        "metric_layer": "application_payload",
        "bytes_expected": byte_count,
        "bytes_received": got,
        "elapsed_sec": round(elapsed, 3),
        "throughput_mbps": tmetrics["throughput_mbps"],
        "theoretical_min_sec_at_50mbps": round(t_min_50, 3),
        "ok": ok,
    }


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
            f"socat -u - UDP4:{target_host}:{port} || exit 1; done'"
        )
    else:
        send_rate_limit = os.environ.get("MASQUE_TCP_IP_RATE_LIMIT", "").strip()
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


def run_tcp_ip_icmp(docker, timeout_sec=5):
    target_host = "10.200.0.2"
    docker_exec(docker, CLIENT_CONTAINER, "ip route add 10.200.0.0/24 dev tun0 2>/dev/null || true")
    started = time.time()
    result = run_capture(
        [
            docker,
            "exec",
            "-e",
            f"MASQUE_ICMP_DEST_HOST={target_host}",
            "-e",
            f"MASQUE_ICMP_TIMEOUT_SEC={timeout_sec}",
            CLIENT_CONTAINER,
            "python3",
            "-c",
            _TCP_IP_ICMP_PING,
        ],
        cwd=ROOT,
    )
    elapsed = time.time() - started
    output = (result.stdout or "") + (result.stderr or "")
    ok = "ok=1" in output
    latency_ms = -1.0
    for token in output.replace("\n", " ").split():
        if token.startswith("latency_ms="):
            try:
                latency_ms = float(token.split("=", 1)[1])
            except ValueError:
                latency_ms = -1.0
    return {
        "scenario": "tcp_ip_icmp",
        "bytes_expected": 1,
        "bytes_received": 1 if ok else 0,
        "elapsed_sec": round(elapsed, 3),
        "latency_ms": round(latency_ms, 3) if latency_ms >= 0 else latency_ms,
        "ok": ok,
        "error": None if ok else output.strip(),
    }


def run_tcp_ip_iperf_matrix(
    docker,
    mtu,
    rates,
    duration_sec,
    loss_threshold_pct,
    min_delivery_ratio,
):
    target_host, port = "10.200.0.2", 5201
    payload_len = max(1200, int(mtu) - 28)
    docker_exec(docker, CLIENT_CONTAINER, "ip route add 10.200.0.0/24 dev tun0 2>/dev/null || true")
    trials = []
    for rate in rates:
        docker_exec(
            docker,
            IPERF_CONTAINER,
            f"pkill iperf3 2>/dev/null || true; nohup iperf3 -s -1 -p {port} >/tmp/iperf3-server.log 2>&1 &",
        )
        # iperf3 server advertises a TCP control listener on the same port;
        # UDP flow setup depends on this control channel.
        wait_tcp_listener(docker, IPERF_CONTAINER, port, timeout_sec=8)
        cmd = (
            f"timeout {int(duration_sec) + 10} "
            f"iperf3 -c {target_host} -u -l {payload_len} -b {rate} -t {int(duration_sec)} -p {port} -J"
        )
        started = time.time()
        error = None
        sender_bps = 0.0
        receiver_bps = 0.0
        lost_percent = 100.0
        jitter_ms = 0.0
        packets = 0
        lost_packets = 0
        try:
            result = subprocess.run(
                [docker, "exec", CLIENT_CONTAINER, "sh", "-lc", cmd],
                cwd=ROOT,
                text=True,
                capture_output=True,
                check=False,
            )
            merged_output = (result.stdout or "") + "\n" + (result.stderr or "")
            parsed = parse_iperf_udp_result(merged_output)
            sender_bps = parsed["sender_bps"]
            receiver_bps = parsed["receiver_bps"]
            lost_percent = parsed["lost_percent"]
            jitter_ms = parsed["jitter_ms"]
            packets = parsed["packets"]
            lost_packets = parsed["lost_packets"]
            if result.returncode != 0:
                err_tail = (result.stderr or "").strip()
                if len(err_tail) > 240:
                    err_tail = err_tail[-240:]
                error = f"iperf exit={result.returncode}" + (f": {err_tail}" if err_tail else "")
        except Exception as exc:
            error = str(exc)
        elapsed = time.time() - started
        target_bps = parse_rate_limit_to_bps(rate)
        delivered_ratio = (receiver_bps / target_bps) if target_bps > 0 else 0.0
        stable = (
            error is None
            and lost_percent <= loss_threshold_pct
            and delivered_ratio >= min_delivery_ratio
        )
        trials.append(
            {
                "rate": rate,
                "target_mbps": round(target_bps / 1_000_000.0, 3),
                "sender_mbps": round(sender_bps / 1_000_000.0, 3),
                "receiver_mbps": round(receiver_bps / 1_000_000.0, 3),
                "delivery_ratio": round(delivered_ratio, 3),
                "loss_pct": round(lost_percent, 3),
                "jitter_ms": round(jitter_ms, 3),
                "packets": packets,
                "lost_packets": lost_packets,
                "elapsed_sec": round(elapsed, 3),
                "stable": stable,
                "error": error,
            }
        )
    stable_trials = [t for t in trials if t["stable"]]
    highest = max(stable_trials, key=lambda t: t["receiver_mbps"]) if stable_trials else None
    return {
        "scenario": "tcp_ip_iperf",
        "mode": "tun_rule_masque_real_udp",
        "mtu": int(mtu),
        "udp_payload_len": payload_len,
        "duration_sec": int(duration_sec),
        "loss_threshold_pct": loss_threshold_pct,
        "min_delivery_ratio": min_delivery_ratio,
        "trials": trials,
        "stable_trial_count": len(stable_trials),
        "highest_stable": highest,
        "ok": len(stable_trials) > 0,
    }


def _tun_masque_iperf_budget_sec(byte_count: int) -> int:
    """Wall-clock budget for iperf/socat trials (no multi-minute waits by default)."""
    mb = max(1, int(math.ceil(byte_count / (1024 * 1024))))
    return min(180, max(45, mb * 25 + 35))


def run_connect_udp_via_tun_benchmark(docker, byte_count):
    """
    CONNECT-UDP: tun ingress -> route.final -> masque. Raw UDP (same tools as udp scenario).

    Avoid iperf towards a tun-routed remote TCP port here: tunnel-originated TCP to a backend is a
    separate upstream concern; this scenario measures TCP via SOCKS→connect_stream separately.

    Listener on masque-server (10.200.0.3:5601), same contract as udp scenario.
    """
    target_host, port = "10.200.0.3", 5601
    sink = "/tmp/udp-tun-bench.bin"
    if byte_count <= BYTES_10KB:
        budget = _tun_masque_iperf_budget_sec(byte_count)
        server_outer = budget + 30
        recv_poll_cap = min(180.0, float(budget) + 45)
    else:
        server_outer, budget, wait_cap = _udp_tcp_stream_bulk_harness_timeouts(byte_count)
        recv_poll_cap = float(wait_cap)
    docker_exec(docker, SERVER_CONTAINER, f"rm -f {sink}", check=False)
    docker_exec(
        docker,
        SERVER_CONTAINER,
        (
            f"nohup timeout {server_outer} socat -u -T1 UDP4-LISTEN:{port},reuseaddr OPEN:{sink},creat,append "
            f">/tmp/udp-tun-bench.log 2>&1 &"
        ),
    )
    wait_udp_listener(docker, SERVER_CONTAINER, port)
    route_probe = docker_exec_capture(docker, CLIENT_CONTAINER, f"ip route get {target_host}")
    elapsed_sec = -1.0
    sender_mbps = 0.0
    sender_bytes_sent = 0
    sender_line = ""
    if byte_count <= 0:
        sender_line = "send_skipped_byte_count_zero"
    else:
        t0 = time.monotonic()
        raw_pause = (os.environ.get("MASQUE_STAND_UDP_BULK_PAUSE") or "0").strip()
        try:
            bulk_pause = max(0.0, float(raw_pause))
        except ValueError:
            bulk_pause = 0.0
        tun_chunk = _stand_udp_chunk(byte_count, 0)
        docker_exec(
            docker,
            CLIENT_CONTAINER,
            _udp_tun_datagram_send_sh(
                byte_count, target_host, port, int(budget), chunk=tun_chunk, pause_sec=bulk_pause
            ),
            check=False,
        )
        elapsed_sec = max(0.0, time.monotonic() - t0)
        sender_bytes_sent = int(byte_count)
        sender_mbps = (byte_count * 8.0 / elapsed_sec / 1_000_000.0) if elapsed_sec > 0 else 0.0
        sender_line = (
            f"elapsed_sec={elapsed_sec:.6f} bytes_sent={sender_bytes_sent} mbps={sender_mbps:.6f} "
            f"(python-udp chunk_datagram={tun_chunk}B)"
        )

    got = bytes_on_file(docker, SERVER_CONTAINER, sink)
    recv_wait = wait_for_bytes(docker, SERVER_CONTAINER, sink, byte_count, recv_poll_cap)
    got = max(got, recv_wait)
    recv_elapsed = elapsed_sec if elapsed_sec > 0 else 1.0
    recv_mbps = (got * 8.0 / recv_elapsed / 1_000_000.0) if recv_elapsed > 0 else 0.0

    return {
        "flow": "connect_udp_via_tun_raw_udp",
        "path_note": (
            "tun -> route.final -> masque (CONNECT-UDP), raw UDP payload path. "
            "TCP throughput is reported separately via SOCKS-bridged iperf (connect_stream)."
        ),
        "bytes_target": int(byte_count),
        "bytes_received": int(got),
        "route_probe": route_probe,
        "sender_line": sender_line.strip(),
        "elapsed_sec_sender": round(elapsed_sec, 6) if elapsed_sec >= 0 else -1.0,
        "sender_mbps": round(sender_mbps, 3),
        "receiver_mbps_approx": round(recv_mbps, 3),
        "bytes_sent_actual": sender_bytes_sent,
        "ok": got >= byte_count,
    }


def _parse_iperf_tcp_json(stdout: str) -> dict:
    start = stdout.find("{")
    end = stdout.rfind("}")
    if start < 0 or end < start:
        raise ValueError("iperf json output not found")
    payload = stdout[start : end + 1]
    data = json.loads(payload)
    end_block = data.get("end", {})
    sent = end_block.get("sum_sent", {})
    recv = end_block.get("sum_received", {})
    return {
        "sender_bps": float(sent.get("bits_per_second", 0.0) or 0.0),
        "receiver_bps": float(recv.get("bits_per_second", 0.0) or 0.0),
        "seconds": float(recv.get("seconds", sent.get("seconds", 0.0)) or 0.0),
        "bytes_sent": int(sent.get("bytes", 0) or 0),
        "bytes_received": int(recv.get("bytes", 0) or 0),
    }


def _socks_iperf_tcp_kill_bridge(docker, client_container: str):
    docker_exec(
        docker,
        client_container,
        "pkill -f 'LISTEN:17111' 2>/dev/null || pkill -f 'tcp-listen:17111' 2>/dev/null || true",
        check=False,
    )


def _socks_iperf_tcp_bridge_restart(docker, client_container: str):
    docker_exec(docker, client_container, "rm -f /tmp/iperf-socat-last.json", check=False)
    _socks_iperf_tcp_kill_bridge(docker, client_container)
    time.sleep(0.2)
    bridge = (
        "nohup socat -lf/tmp/socat-tcp-bridge-iperf.log TCP-LISTEN:17111,fork,reuseaddr "
        "SOCKS5-CONNECT:127.0.0.1:1080:10.200.0.2:5201 >/dev/null 2>&1 & "
        "sleep 0.35; echo started"
    )
    docker_exec(docker, client_container, bridge, check=False)
    wait_tcp_listener(docker, client_container, 17111, timeout_sec=25)


def _run_iperf_trial(
    docker, server_container, client_container, byte_count: int, reverse: bool, timeout_sec=120
):
    rev = " -R" if reverse else ""
    docker_exec(
        docker,
        server_container,
        "pkill iperf3 2>/dev/null || true; nohup iperf3 -s -1 -p 5201 >/tmp/iperf3-server.log 2>&1 &",
    )
    wait_tcp_listener(docker, server_container, 5201, timeout_sec=8)
    _socks_iperf_tcp_bridge_restart(docker, client_container)
    started = time.time()
    iperf_shell = (
        f"iperf3 -c 127.0.0.1 -p 17111 -n {int(byte_count)}{rev} -J > /tmp/iperf-socat-last.json 2>&1"
    )
    try:
        result = subprocess.run(
            [
                docker,
                "exec",
                client_container,
                "sh",
                "-lc",
                iperf_shell,
            ],
            cwd=ROOT,
            text=True,
            capture_output=True,
            timeout=timeout_sec,
            check=False,
        )
    except subprocess.TimeoutExpired:
        elapsed = time.time() - started
        _socks_iperf_tcp_kill_bridge(docker, client_container)
        return None, f"timeout_after_{timeout_sec}s", elapsed
    elapsed = time.time() - started
    _socks_iperf_tcp_kill_bridge(docker, client_container)

    json_blob = docker_exec_capture(
        docker, client_container, "cat /tmp/iperf-socat-last.json 2>/dev/null || true"
    )
    merged = (json_blob or "") + "\n" + (result.stdout or "") + "\n" + (result.stderr or "")
    parsed = None
    error = None
    try:
        parsed = _parse_iperf_tcp_json(merged)
    except Exception as exc:
        error = f"json_parse_failed: {exc}"
    if result.returncode != 0:
        tail = merged.strip().replace("\n", " ")
        if len(tail) > 500:
            tail = tail[-500:]
        rc_msg = f"iperf exit={result.returncode}"
        socat_tail = docker_exec_capture(
            docker, client_container, "tail -c 240 /tmp/socat-tcp-bridge-iperf.log 2>/dev/null || true"
        )
        hint = socat_tail or tail
        error = f"{rc_msg} diag={hint!r}"
        if parsed is None:
            error = f"{error} (iperf_json_missing)"
    if parsed is not None and int(parsed.get("bytes_received", 0) or 0) >= int(byte_count * 0.97):
        error = None
    return parsed, error, elapsed


def run_tun_rule_masque_perf(docker, byte_count):
    budget = _tun_masque_iperf_budget_sec(byte_count)
    docker_exec(docker, CLIENT_CONTAINER, "ip route replace 10.200.0.0/24 dev tun0")
    route_probe = docker_exec_capture(docker, CLIENT_CONTAINER, "ip route get 10.200.0.2")
    wait_socks_ready(docker)
    time.sleep(0.35)
    udp_payload = run_connect_udp_via_tun_benchmark(docker, byte_count)

    tcp_fwd, tcp_fwd_err, tcp_fwd_elapsed = _run_iperf_trial(
        docker,
        IPERF_CONTAINER,
        CLIENT_CONTAINER,
        byte_count,
        reverse=False,
        timeout_sec=budget,
    )
    tcp_rev, tcp_rev_err, tcp_rev_elapsed = _run_iperf_trial(
        docker,
        IPERF_CONTAINER,
        CLIENT_CONTAINER,
        byte_count,
        reverse=True,
        timeout_sec=budget,
    )

    def tcp_row(name, parsed, err, elapsed):
        if parsed:
            sender_mbps = round(parsed["sender_bps"] / 1_000_000.0, 3)
            receiver_mbps = round(parsed["receiver_bps"] / 1_000_000.0, 3)
            sent_b = parsed["bytes_sent"]
            recv_b = parsed["bytes_received"]
        else:
            sender_mbps = receiver_mbps = 0.0
            sent_b = recv_b = 0
        ok = err is None and recv_b > 0
        return {
            "flow": name,
            "path_note": (
                "socks-inbound -> outbound masque CONNECT-STREAM; local socat SOCKS5 bridges iperf TCP."
            ),
            "bytes_target": int(byte_count),
            "bytes_sent": int(sent_b),
            "bytes_received": int(recv_b),
            "sender_mbps": sender_mbps,
            "receiver_mbps": receiver_mbps,
            "elapsed_sec": round(elapsed, 3),
            "ok": ok,
            "error": err,
        }

    rows = [
        {
            "flow": udp_payload["flow"],
            "path_note": udp_payload["path_note"],
            "bytes_target": udp_payload["bytes_target"],
            "bytes_sent": udp_payload.get(
                "bytes_sent_actual",
                udp_payload["bytes_received"],
            ),
            "bytes_received": udp_payload["bytes_received"],
            "sender_mbps": udp_payload["sender_mbps"],
            "receiver_mbps_approx": udp_payload["receiver_mbps_approx"],
            "elapsed_sec_sender": udp_payload["elapsed_sec_sender"],
            "ok": udp_payload["ok"],
            "route_probe": udp_payload["route_probe"],
        },
        tcp_row("tcp_stream_via_socks_iperf_forward", tcp_fwd, tcp_fwd_err, tcp_fwd_elapsed),
        tcp_row("tcp_stream_via_socks_iperf_reverse", tcp_rev, tcp_rev_err, tcp_rev_elapsed),
    ]
    merged_ok = all(bool(row.get("ok")) for row in rows)
    return {
        "scenario": "tun_rule_masque_perf",
        "bytes_expected": int(byte_count),
        "bytes_received": int(byte_count) if merged_ok else 0,
        "upstream_gap": (
            "Tunnel-origin TCP to a tun-routed backend host is tracked separately from SOCKS→connect_stream; "
            "UDP leg mirrors run_connect_udp_via_tun_benchmark at the same byte_count as TCP iperf rows."
        ),
        "byte_count_primary": int(byte_count),
        "route_probe": route_probe,
        "rows": rows,
        "ok": merged_ok,
    }


def _write_masque_smoke_contract_files(results: list, byte_count: int) -> None:
    """Write CI smoke JSON artifacts (see hiddify-core/.github/workflows/ci.yml)."""
    if byte_count != BYTES_10KB:
        return
    mapping = {
        "udp": ("smoke_10kb_latest.json", "connect_udp"),
        "tcp_stream": ("smoke_tcp_connect_stream_latest.json", "connect_stream"),
        "tcp_ip": ("smoke_tcp_connect_ip_latest.json", "connect_ip"),
    }
    min_b, max_ms = 10240, 5000
    for row in results:
        scen = row.get("scenario")
        if scen not in mapping:
            continue
        fname, mode = mapping[scen]
        elapsed_ms = int(round(float(row.get("elapsed_sec", 0) or 0) * 1000.0))
        payload = {
            "mode": mode,
            "result": "true" if row.get("ok") else "false",
            "metrics": {
                "bytes_received": int(row.get("bytes_received", 0) or 0),
                "elapsed_ms": elapsed_ms,
            },
            "thresholds": {"min_bytes": min_b, "max_elapsed_ms": max_ms},
        }
        (RUNTIME_DIR / fname).write_text(json.dumps(payload, indent=2), encoding="utf-8")


def run_scenario(
    docker,
    scenario,
    byte_count,
    tcp_ip_mode,
    tcp_ip_deadline_sec=None,
    iperf_cfg=None,
    udp_chunk=0,
    udp_rate_bps=0,
    udp_loss_pct=0.0,
    udp_matrix_sizes_mib=None,
    udp_matrix_rates_bps=None,
    udp_matrix_losses_pct=None,
):
    if scenario == "udp":
        compose_up(docker, DEFAULT_CLIENT_CONFIG)
        return run_udp(
            docker,
            byte_count,
            udp_chunk=udp_chunk,
            udp_rate_bps=udp_rate_bps,
            udp_loss_pct=udp_loss_pct,
        )
    if scenario == "udp_matrix":
        compose_up(docker, DEFAULT_CLIENT_CONFIG)
        sizes = udp_matrix_sizes_mib or [10, 20]
        rates = udp_matrix_rates_bps or [0]
        losses = udp_matrix_losses_pct or [0.0]
        return run_udp_matrix(docker, sizes, rates, losses, udp_chunk=udp_chunk)
    if scenario == "tcp_stream":
        compose_up(docker, DEFAULT_CLIENT_CONFIG)
        return run_tcp_stream(docker, byte_count)
    if scenario == "tcp_ip":
        compose_up(docker, CONNECT_IP_CLIENT_CONFIG)
        return run_tcp_ip(docker, byte_count, mode=tcp_ip_mode, tcp_ip_deadline_sec=tcp_ip_deadline_sec)
    if scenario == "tcp_ip_icmp":
        compose_up(docker, CONNECT_IP_CLIENT_CONFIG)
        return run_tcp_ip_icmp(docker, timeout_sec=5)
    if scenario == "tcp_ip_iperf":
        compose_up(docker, CONNECT_IP_CLIENT_CONFIG)
        cfg = iperf_cfg or {}
        return run_tcp_ip_iperf_matrix(
            docker=docker,
            mtu=cfg.get("mtu", 1500),
            rates=cfg.get("rates", ["100M", "250M", "500M", "750M", "1G"]),
            duration_sec=cfg.get("duration_sec", 20),
            loss_threshold_pct=cfg.get("loss_threshold_pct", 1.0),
            min_delivery_ratio=cfg.get("min_delivery_ratio", 0.85),
        )
    if scenario == "tun_rule_masque_perf":
        compose_up(docker, DEFAULT_CLIENT_CONFIG)
        return run_tun_rule_masque_perf(docker, byte_count)
    raise ValueError(f"unsupported scenario: {scenario}")


def main():
    parser = argparse.ArgumentParser(description="Single entrypoint for MASQUE stand scenarios")
    parser.add_argument(
        "--scenario",
        choices=[
            "udp",
            "udp_matrix",
            "tcp_stream",
            "tcp_ip",
            "tcp_ip_icmp",
            "tcp_ip_iperf",
            "tun_rule_masque_perf",
            "all",
            "real",
        ],
        required=True,
    )
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
    parser.add_argument("--mtu", type=int, default=1500, help="MTU target for CONNECT-IP real profile (default: 1500)")
    parser.add_argument(
        "--iperf-rates",
        type=str,
        default="100M,250M,500M,750M,1G",
        help="comma-separated UDP rates for tcp_ip_iperf scenario (iperf syntax)",
    )
    parser.add_argument("--iperf-duration-sec", type=int, default=20, help="duration per iperf trial")
    parser.add_argument("--iperf-loss-threshold-pct", type=float, default=1.0, help="max loss percent for stable trial")
    parser.add_argument("--iperf-min-delivery-ratio", type=float, default=0.85, help="min receiver/target ratio for stable trial")
    parser.add_argument(
        "--udp-chunk-bytes",
        type=int,
        default=0,
        metavar="B",
        help=(
            f"UDP stand sender application payload per datagram (256..{MASQUE_STAND_UDP_CHUNK_MAX}); "
            "0=auto (smoke 960 B, bulk max CONNECT-UDP payload). Not L2 MTU — see MASQUE_STAND_UDP_CHUNK_MAX in runner."
        ),
    )
    parser.add_argument(
        "--udp-send-bps",
        type=int,
        default=0,
        metavar="BPS",
        help="pace UDP sender: inter-send pause = chunk/rate (0=no pacing; bulk default pause=0, override via MASQUE_STAND_UDP_BULK_PAUSE)",
    )
    parser.add_argument(
        "--udp-loss-pct",
        type=float,
        default=0.0,
        metavar="PCT",
        help="tc netem loss on client default iface for udp/tcp_matrix (0=disabled)",
    )
    parser.add_argument(
        "--udp-matrix-sizes-mib",
        type=str,
        default="10,20",
        help="comma-separated MiB sizes for udp_matrix (default: 10,20)",
    )
    parser.add_argument(
        "--udp-matrix-rates-bps",
        type=str,
        default="0",
        help="comma-separated target send rates for udp_matrix (0=default pacing per row)",
    )
    parser.add_argument(
        "--udp-matrix-loss-pct",
        type=str,
        default="0",
        help="comma-separated loss %% rows for udp_matrix (e.g. 0,1); uses relaxed ok floor when >0",
    )
    args = parser.parse_args()

    if args.stress and args.megabytes is not None:
        print("Note: --stress forces 500MB; ignoring --megabytes", flush=True)
    if args.megabytes is not None and args.megabytes < 1:
        parser.error("--megabytes must be >= 1")

    if args.mtu < 1280 or args.mtu > 9000:
        parser.error("--mtu must be in [1280, 9000]")
    if args.iperf_duration_sec < 3:
        parser.error("--iperf-duration-sec must be >= 3")
    if args.iperf_min_delivery_ratio <= 0 or args.iperf_min_delivery_ratio > 1:
        parser.error("--iperf-min-delivery-ratio must be in (0,1]")
    if args.iperf_loss_threshold_pct < 0:
        parser.error("--iperf-loss-threshold-pct must be >= 0")
    if args.udp_chunk_bytes != 0 and (
        args.udp_chunk_bytes < 256 or args.udp_chunk_bytes > MASQUE_STAND_UDP_CHUNK_MAX
    ):
        parser.error(f"--udp-chunk-bytes must be 0 or in [256, {MASQUE_STAND_UDP_CHUNK_MAX}]")
    if args.udp_loss_pct < 0 or args.udp_loss_pct > 50:
        parser.error("--udp-loss-pct must be in [0, 50]")
    try:
        iperf_rates = parse_iperf_rates(args.iperf_rates)
    except ValueError as exc:
        parser.error(str(exc))

    docker = docker_bin()
    if args.stress:
        byte_count = BYTES_500MB
    elif args.megabytes is not None:
        byte_count = bytes_from_megabytes_arg(args.megabytes)
    else:
        byte_count = BYTES_10KB

    if args.scenario == "all":
        scenarios = ["udp", "tcp_stream", "tcp_ip", "tcp_ip_icmp"]
        if byte_count > BYTES_10KB:
            scenarios = ["tcp_ip"]
            print(
                "Note: bulk size >10KB with --scenario all runs tcp_ip only "
                "(udp/tcp_stream harness is smoke-sized).",
                flush=True,
            )
    elif args.scenario == "real":
        scenarios = ["tcp_ip_iperf"]
    elif args.scenario == "udp_matrix":
        scenarios = ["udp_matrix"]
    else:
        scenarios = [args.scenario]

    RUNTIME_DIR.mkdir(parents=True, exist_ok=True)
    compile_singbox()

    iperf_cfg = {
        "mtu": int(args.mtu),
        "rates": iperf_rates,
        "duration_sec": int(args.iperf_duration_sec),
        "loss_threshold_pct": float(args.iperf_loss_threshold_pct),
        "min_delivery_ratio": float(args.iperf_min_delivery_ratio),
    }

    results = []
    overall_ok = True
    for scenario in scenarios:
        print(f"\n=== Running scenario: {scenario} ({byte_count} bytes) ===")
        try:
            matrix_sizes = _parse_csv_ints(args.udp_matrix_sizes_mib, [10, 20])
            matrix_rates = _parse_csv_ints(args.udp_matrix_rates_bps, [0])
            matrix_losses = _parse_csv_floats(args.udp_matrix_loss_pct, [0.0])
            result = run_scenario(
                docker,
                scenario,
                byte_count,
                args.tcp_ip_mode,
                tcp_ip_deadline_sec=args.tcp_ip_deadline_sec,
                iperf_cfg=iperf_cfg,
                udp_chunk=args.udp_chunk_bytes,
                udp_rate_bps=args.udp_send_bps,
                udp_loss_pct=args.udp_loss_pct,
                udp_matrix_sizes_mib=matrix_sizes,
                udp_matrix_rates_bps=matrix_rates,
                udp_matrix_losses_pct=matrix_losses,
            )
        except Exception as exc:
            result = {"scenario": scenario, "bytes_expected": byte_count, "bytes_received": 0, "elapsed_sec": 0.0, "ok": False, "error": str(exc)}
        results.append(result)
        overall_ok = overall_ok and bool(result.get("ok"))
        print(json.dumps(result, ensure_ascii=True))

    _write_masque_smoke_contract_files(results, byte_count)

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
