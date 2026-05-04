#!/usr/bin/env python3
import argparse
import hashlib
import json
import subprocess
import time

from masque_stand_runner import (
    CLIENT_CONTAINER,
    CONNECT_IP_CLIENT_CONFIG,
    IPERF_CONTAINER,
    compile_singbox,
    compose_up,
    docker_bin,
    docker_exec,
    file_sha256,
    wait_for_bytes,
    wait_socks_ready,
    wait_tcp_listener,
)


def expected_zero_sha(byte_count: int) -> str:
    h = hashlib.sha256()
    chunk = b"\x00" * (1024 * 1024)
    q, r = divmod(byte_count, len(chunk))
    for _ in range(q):
        h.update(chunk)
    if r:
        h.update(chunk[:r])
    return h.hexdigest()


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--size-mb", type=int, required=True)
    parser.add_argument("--send-timeout-sec", type=int, default=40)
    parser.add_argument("--wait-timeout-sec", type=int, default=50)
    args = parser.parse_args()

    docker = docker_bin()
    compile_singbox()
    compose_up(docker, CONNECT_IP_CLIENT_CONFIG)

    byte_count = args.size_mb * 1024 * 1024
    port = 5602
    sink = "/tmp/tcp-connect-ip-python.bin"
    docker_exec(docker, IPERF_CONTAINER, f"rm -f {sink}", check=False)
    docker_exec(
        docker,
        IPERF_CONTAINER,
        f"nohup timeout 120 socat -u TCP-LISTEN:{port},reuseaddr,fork OPEN:{sink},creat,append >/tmp/tcp-connect-ip-python.log 2>&1 &",
    )
    wait_tcp_listener(docker, IPERF_CONTAINER, port)
    wait_socks_ready(docker)

    chunks = max(1, byte_count // 1024)
    send_cmd = (
        f"timeout {args.send_timeout_sec} sh -lc '"
        f"for i in $(seq 1 {chunks}); do "
        f"dd if=/dev/zero bs=1024 count=1 2>/dev/null | "
        f"socat -u - SOCKS5:127.0.0.1:10.200.0.2:{port},socksport=1080 || exit 1; "
        f"done'"
    )
    start = time.time()
    err = None
    try:
        docker_exec(docker, CLIENT_CONTAINER, send_cmd)
    except subprocess.CalledProcessError as exc:
        err = str(exc)
    elapsed = time.time() - start

    got = wait_for_bytes(docker, IPERF_CONTAINER, sink, byte_count, args.wait_timeout_sec)
    actual_hash = file_sha256(docker, IPERF_CONTAINER, sink)
    expected_hash = expected_zero_sha(byte_count)
    loss_bytes = max(0, byte_count - got)
    result = {
        "size_mb": args.size_mb,
        "send_timeout_sec": args.send_timeout_sec,
        "wait_timeout_sec": args.wait_timeout_sec,
        "bytes_expected": byte_count,
        "bytes_received": got,
        "elapsed_sec": round(elapsed, 3),
        "hash_ok": actual_hash != "" and actual_hash == expected_hash,
        "hash_expected_sha256": expected_hash,
        "hash_actual_sha256": actual_hash,
        "loss_bytes": loss_bytes,
        "loss_pct": round((loss_bytes / byte_count) * 100, 4),
        "throughput_mbps": round((got * 8 / elapsed / 1_000_000) if elapsed > 0 else 0.0, 3),
        "ok": bool(got >= byte_count and actual_hash == expected_hash and err is None),
        "error": err,
    }
    print(json.dumps(result, ensure_ascii=True))


if __name__ == "__main__":
    main()

