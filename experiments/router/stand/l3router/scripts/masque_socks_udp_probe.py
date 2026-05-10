"""SOCKS5 UDP ASSOCIATE -> sing-box -> MASQUE CONNECT-UDP (stand probe; not TUN)."""
import os
import socket
import struct
import time


def _read_bind_addr(sock: socket.socket) -> tuple[str, int]:
    hdr = sock.recv(4)
    if len(hdr) != 4:
        raise SystemExit("socks associate short header")
    ver, rep, _rsv, atyp = hdr[0], hdr[1], hdr[2], hdr[3]
    if ver != 5:
        raise SystemExit("socks associate bad ver")
    if rep != 0:
        raise SystemExit(f"socks associate failed rep={rep}")
    if atyp == 1:
        rest = sock.recv(6)
        if len(rest) != 6:
            raise SystemExit("socks associate ipv4 bind short")
        ip = socket.inet_ntoa(rest[:4])
        port = struct.unpack("!H", rest[4:6])[0]
        return ip, port
    if atyp == 3:
        ln = sock.recv(1)
        if not ln:
            raise SystemExit("socks associate domain eof")
        dlen = ln[0]
        rest = sock.recv(dlen + 2)
        if len(rest) != dlen + 2:
            raise SystemExit("socks associate domain bind short")
        return rest[:dlen].decode("ascii"), struct.unpack("!H", rest[dlen:])[0]
    if atyp == 4:
        rest = sock.recv(18)
        if len(rest) != 18:
            raise SystemExit("socks associate ipv6 bind short")
        return socket.inet_ntop(socket.AF_INET6, rest[:16]), struct.unpack("!H", rest[16:18])[0]
    raise SystemExit(f"socks associate unsupported atyp={atyp}")


def main() -> None:
    proxy_host = os.environ.get("MASQUE_SOCKS_HOST", "127.0.0.1")
    proxy_port = int(os.environ.get("MASQUE_SOCKS_PORT", "1080"))
    dst_host = os.environ["MASQUE_UDP_DST_HOST"]
    dst_port = int(os.environ["MASQUE_UDP_DST_PORT"])
    nbytes = int(os.environ.get("MASQUE_UDP_SEND_BYTES", "256"))

    tcp = socket.create_connection((proxy_host, proxy_port), timeout=30)
    try:
        tcp.sendall(b"\x05\x01\x00")
        ver, method = tcp.recv(2)
        if ver != 5 or method != 0:
            raise SystemExit(f"socks handshake {ver!r},{method!r}")
        # UDP ASSOCIATE, DST 0.0.0.0:0
        tcp.sendall(b"\x05\x03\x00\x01\x00\x00\x00\x00\x00\x00")
        bnd_ip, bnd_port = _read_bind_addr(tcp)
        relay_host = proxy_host if bnd_ip in ("0.0.0.0", "::") else bnd_ip

        udp = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        # Per-datagram cap: IPv4 SOCKS UDP header is 10 B before payload; stay under typical UDP max.
        # Default 8192 — ближе к bulk TUN (`MASQUE_STAND_UDP_CHUNK_BULK_DEFAULT`); SOCKS-шапка 10 B к payload.
        chunk_raw = (os.environ.get("MASQUE_SOCKS_UDP_CHUNK") or "").strip()
        try:
            chunk = int(chunk_raw) if chunk_raw else 8192
        except ValueError:
            chunk = 8192
        chunk = max(512, min(chunk, 65_400))
        rate_raw = (os.environ.get("MASQUE_SOCKS_UDP_SEND_BPS") or "").strip()
        try:
            rate_bps = max(0, int(rate_raw)) if rate_raw else 0
        except ValueError:
            rate_bps = 0
        udp_timeout = max(30.0, min(600.0, float(nbytes) / (1024 * 1024) * 8.0))
        if rate_bps > 0:
            udp_timeout = max(udp_timeout, float(nbytes) / float(rate_bps) + 120.0)
        udp.settimeout(udp_timeout)
        hdr_prefix = (
            b"\x00\x00\x00\x01"
            + socket.inet_aton(dst_host)
            + struct.pack("!H", dst_port)
        )
        t0 = time.perf_counter()
        sent_total = 0
        try:
            remaining = nbytes
            while remaining > 0:
                n = min(remaining, chunk)
                if rate_bps > 0 and sent_total > 0:
                    elapsed = time.perf_counter() - t0
                    need = sent_total / float(rate_bps)
                    if need > elapsed:
                        time.sleep(need - elapsed)
                payload = b"\x00" * n
                udp.sendto(hdr_prefix + payload, (relay_host, bnd_port))
                sent_total += n
                remaining -= n
            time.sleep(0.35)
        finally:
            udp.close()
    finally:
        tcp.close()


if __name__ == "__main__":
    main()
