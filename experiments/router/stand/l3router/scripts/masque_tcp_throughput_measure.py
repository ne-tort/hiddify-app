"""
Measure TCP payload goodput (client send loop) through HTTP CONNECT or SOCKS5 -> masque CONNECT-stream.
Chunked send avoids allocating huge buffers. Prints one TSV line: scenario\\tbytes\\tsec\\tpayload_mbps
"""
from __future__ import annotations

import os
import socket
import struct
import sys
import time


CHUNK = 256 * 1024


def _connect_timeout_sec() -> float:
    raw = os.environ.get("MASQUE_BENCH_CONNECT_TIMEOUT_SEC", "").strip()
    if raw:
        return max(5.0, float(raw))
    return 180.0


def socks5_tcp_connect(proxy_host: str, proxy_port: int, ip: str, port: int) -> socket.socket:
    sock = socket.create_connection((proxy_host, proxy_port), timeout=_connect_timeout_sec())
    sock.setsockopt(socket.IPPROTO_TCP, socket.TCP_NODELAY, 1)
    sock.sendall(b"\x05\x01\x00")
    ver, method = sock.recv(2)
    if ver != 5 or method != 0:
        raise SystemExit(f"socks handshake {ver!r},{method!r}")
    bip = socket.inet_aton(ip)
    sock.sendall(b"\x05\x01\x00\x01" + bip + struct.pack("!H", port))
    hdr = sock.recv(4)
    if len(hdr) != 4:
        raise SystemExit("socks connect short header")
    ver, rep, _rsv, atyp = hdr[0], hdr[1], hdr[2], hdr[3]
    if ver != 5 or rep != 0:
        raise SystemExit(f"socks connect failed ver={ver} rep={rep}")
    if atyp == 1:
        tail = sock.recv(6)
        if len(tail) != 6:
            raise SystemExit("socks ipv4 bind length")
    elif atyp == 4:
        tail = sock.recv(18)
        if len(tail) != 18:
            raise SystemExit("socks ipv6 bind length")
    elif atyp == 3:
        ln = sock.recv(1)
        if not ln:
            raise SystemExit("socks domain eof")
        dlen = ln[0]
        tail = sock.recv(dlen + 2)
        if len(tail) != dlen + 2:
            raise SystemExit("socks domain bind length")
    else:
        raise SystemExit(f"socks unsupported atyp={atyp}")
    return sock


def http_connect(proxy_host: str, proxy_port: int, dest_host: str, dest_port: int) -> socket.socket:
    s = socket.create_connection((proxy_host, proxy_port), timeout=_connect_timeout_sec())
    s.setsockopt(socket.IPPROTO_TCP, socket.TCP_NODELAY, 1)
    req = (
        f"CONNECT {dest_host}:{dest_port} HTTP/1.1\r\n"
        f"Host: {dest_host}:{dest_port}\r\n\r\n"
    )
    s.sendall(req.encode())
    buf = b""
    while b"\r\n\r\n" not in buf:
        chunk = s.recv(4096)
        if not chunk:
            raise SystemExit("http connect: short response")
        buf += chunk
    line = buf.split(b"\r\n", 1)[0].decode("latin-1", errors="replace")
    if " 200 " not in line:
        raise SystemExit(f"http connect failed: {line!r}")
    return s


def send_payload_timed(s: socket.socket, byte_count: int) -> float:
    buf = bytearray(CHUNK)
    remaining = byte_count
    t0 = time.perf_counter()
    while remaining > 0:
        n = min(remaining, CHUNK)
        s.sendall(memoryview(buf)[:n])
        remaining -= n
    return time.perf_counter() - t0


def main() -> None:
    scenario = os.environ["MASQUE_BENCH_SCENARIO"]
    mode = os.environ["MASQUE_BENCH_MODE"].lower().strip()
    dest_host = os.environ["MASQUE_DST_HOST"]
    dest_port = int(os.environ["MASQUE_DST_PORT"])
    byte_count = int(os.environ.get("MASQUE_TCPSEND_BYTES", str(100 * 1024 * 1024)))

    proxy_host = os.environ.get("MASQUE_PROXY_HOST", "127.0.0.1")
    proxy_port = int(os.environ["MASQUE_PROXY_PORT"])

    if mode == "http":
        s = http_connect(proxy_host, proxy_port, dest_host, dest_port)
    elif mode == "socks":
        s = socks5_tcp_connect(proxy_host, proxy_port, dest_host, dest_port)
    else:
        raise SystemExit(f"MASQUE_BENCH_MODE must be http or socks, got {mode!r}")

    try:
        elapsed = send_payload_timed(s, byte_count)
        # Allow receiver / HTTP3 stream to drain (larger payloads need more tail time).
        if os.environ.get("MASQUE_BENCH_DRAIN_SLEEP_SEC", "").strip():
            drain_sec = float(os.environ["MASQUE_BENCH_DRAIN_SLEEP_SEC"])
        else:
            drain_sec = 2.0 if byte_count >= 100 * 1024 * 1024 else 0.6
        time.sleep(drain_sec)
        try:
            s.shutdown(socket.SHUT_WR)
        except OSError:
            pass
    finally:
        s.close()

    mbps = (byte_count * 8) / elapsed / 1e6 if elapsed > 0 else 0.0
    sys.stdout.write(f"{scenario}\t{byte_count}\t{elapsed:.6f}\t{mbps:.3f}\n")
    sys.stdout.flush()


if __name__ == "__main__":
    main()
