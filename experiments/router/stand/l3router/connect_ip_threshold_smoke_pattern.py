#!/usr/bin/env python3
import hashlib
import json
import subprocess
import time
from pathlib import Path

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
    block = b"\x00" * (1024 * 1024)
    q, r = divmod(byte_count, len(block))
    for _ in range(q):
        h.update(block)
    if r:
        h.update(block[:r])
    return h.hexdigest()


def run_size(docker: str, byte_count: int) -> dict:
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
    # Keep smoke-like 1KB chunk pattern, but scale timeout for larger payloads.
    transfer_timeout_sec = max(180, int(byte_count / 180_000) + 120)
    send_cmd = (
        f"timeout {transfer_timeout_sec} sh -lc '"
        f"for i in $(seq 1 {chunks}); do "
        f"dd if=/dev/zero bs=1024 count=1 2>/dev/null | "
        f"socat -u - SOCKS5:127.0.0.1:10.200.0.2:{port},socksport=1080 || exit 1; "
        "done'"
    )
    start = time.time()
    err = None
    try:
        docker_exec(docker, CLIENT_CONTAINER, send_cmd)
    except subprocess.CalledProcessError as exc:
        err = str(exc)
    elapsed = time.time() - start
    wait_timeout_sec = max(20, transfer_timeout_sec + 60)
    got = wait_for_bytes(docker, IPERF_CONTAINER, sink, byte_count, wait_timeout_sec)
    actual_hash = file_sha256(docker, IPERF_CONTAINER, sink)
    expected_hash = expected_zero_sha(byte_count)
    hash_ok = actual_hash != "" and actual_hash == expected_hash
    loss_bytes = max(0, byte_count - got)
    return {
        "size_kb": byte_count // 1024,
        "bytes_expected": byte_count,
        "bytes_received": got,
        "elapsed_sec": round(elapsed, 3),
        "hash_ok": hash_ok,
        "hash_expected_sha256": expected_hash,
        "hash_actual_sha256": actual_hash,
        "loss_bytes": loss_bytes,
        "loss_pct": round((loss_bytes / byte_count) * 100, 4),
        "throughput_mbps": round((got * 8 / elapsed / 1_000_000) if elapsed > 0 else 0.0, 3),
        "ok": bool(got >= byte_count and hash_ok and err is None),
        "error": err,
    }


def main() -> None:
    docker = docker_bin()
    compile_singbox()
    compose_up(docker, CONNECT_IP_CLIENT_CONFIG)

    sweep1 = [kb * 1024 for kb in range(10, 101, 10)]
    sweep2 = [kb * 1024 for kb in range(200, 1001, 100)]
    results = []

    for size in sweep1:
        results.append(run_size(docker, size))

    if results and results[-1]["size_kb"] == 100 and results[-1]["ok"]:
        for size in sweep2:
            results.append(run_size(docker, size))

    summary = {
        "pattern": "smoke_like_1kb_chunks",
        "results": results,
    }
    out_path = Path(__file__).resolve().parent / "runtime" / "connect_ip_threshold_smoke_pattern.json"
    out_path.write_text(json.dumps(summary, indent=2), encoding="utf-8")
    print(json.dumps(summary, ensure_ascii=True))
    print(f"written: {out_path}")


if __name__ == "__main__":
    main()

