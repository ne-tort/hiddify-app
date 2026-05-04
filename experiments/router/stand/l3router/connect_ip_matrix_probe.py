#!/usr/bin/env python3
import json
import subprocess
from datetime import datetime, timezone
from pathlib import Path

from masque_stand_runner import (
    CLIENT_CONTAINER,
    CONNECT_IP_CLIENT_CONFIG,
    RUNTIME_DIR,
    compile_singbox,
    compose_up,
    docker_bin,
    docker_exec,
    run_tcp_ip,
    strict_timeout_sec,
)


def apply_shaping(docker: str, rate_mbps: int | None) -> None:
    docker_exec(docker, CLIENT_CONTAINER, "tc qdisc del dev eth0 root 2>/dev/null || true", check=False)
    if rate_mbps is None:
        return
    cmd = (
        "tc qdisc replace dev eth0 root tbf "
        f"rate {rate_mbps}mbit burst 64kbit latency 400ms"
    )
    docker_exec(docker, CLIENT_CONTAINER, cmd)


def run_matrix() -> dict:
    docker = docker_bin()
    compile_singbox()
    compose_up(docker, CONNECT_IP_CLIENT_CONFIG)

    size_sweep_mb = [1, 10, 20, 30, 40, 50]
    shaping_sweep_mbps = [1, 10, 20, 30, 40, 50]
    shaping_probe_size_mb = 10

    size_results = []
    shaping_results = []

    apply_shaping(docker, None)
    for size_mb in size_sweep_mb:
        # Strict timeout policy: N MB must finish within N seconds.
        send_timeout_sec = strict_timeout_sec(size_mb * 1024 * 1024, floor_sec=1)
        wait_timeout_sec = send_timeout_sec
        try:
            result = run_tcp_ip(
                docker,
                size_mb * 1024 * 1024,
                mode="bulk_single_flow",
                send_timeout_sec=send_timeout_sec,
                wait_timeout_sec=wait_timeout_sec,
            )
        except subprocess.CalledProcessError as exc:
            result = {
                "scenario": "tcp_ip",
                "bytes_expected": size_mb * 1024 * 1024,
                "bytes_received": 0,
                "elapsed_sec": 0.0,
                "hash_ok": False,
                "metrics": {"loss_bytes": size_mb * 1024 * 1024, "loss_pct": 100.0, "throughput_mbps": 0.0},
                "ok": False,
                "error": str(exc),
            }
        result["test_group"] = "size_sweep_unshaped"
        result["size_mb"] = size_mb
        result["shape_mbps"] = None
        result["timeouts"] = {"send_timeout_sec": send_timeout_sec, "wait_timeout_sec": wait_timeout_sec}
        size_results.append(result)
        if not result.get("ok"):
            # We only need the failure boundary, no need to run larger sizes.
            break

    for rate in shaping_sweep_mbps:
        apply_shaping(docker, rate)
        send_timeout_sec = strict_timeout_sec(shaping_probe_size_mb * 1024 * 1024, floor_sec=1)
        wait_timeout_sec = send_timeout_sec
        try:
            result = run_tcp_ip(
                docker,
                shaping_probe_size_mb * 1024 * 1024,
                mode="bulk_single_flow",
                send_timeout_sec=send_timeout_sec,
                wait_timeout_sec=wait_timeout_sec,
            )
        except subprocess.CalledProcessError as exc:
            result = {
                "scenario": "tcp_ip",
                "bytes_expected": shaping_probe_size_mb * 1024 * 1024,
                "bytes_received": 0,
                "elapsed_sec": 0.0,
                "hash_ok": False,
                "metrics": {"loss_bytes": shaping_probe_size_mb * 1024 * 1024, "loss_pct": 100.0, "throughput_mbps": 0.0},
                "ok": False,
                "error": str(exc),
            }
        result["test_group"] = "shape_sweep_fixed_size"
        result["size_mb"] = shaping_probe_size_mb
        result["shape_mbps"] = rate
        result["timeouts"] = {"send_timeout_sec": send_timeout_sec, "wait_timeout_sec": wait_timeout_sec}
        shaping_results.append(result)

    apply_shaping(docker, None)

    max_pass_mb = 0
    first_fail_mb = None
    for item in size_results:
        if item.get("ok"):
            max_pass_mb = max(max_pass_mb, item["size_mb"])
        elif first_fail_mb is None:
            first_fail_mb = item["size_mb"]

    summary = {
        "timestamp_utc": datetime.now(timezone.utc).isoformat(),
        "matrix": {
            "size_sweep_unshaped_mb": size_sweep_mb,
            "shape_sweep_mbps": shaping_sweep_mbps,
            "shape_probe_size_mb": shaping_probe_size_mb,
        },
        "size_sweep_results": size_results,
        "shape_sweep_results": shaping_results,
        "analysis": {
            "max_pass_size_mb_unshaped": max_pass_mb,
            "first_fail_size_mb_unshaped": first_fail_mb,
        },
    }
    return summary


def main() -> None:
    summary = run_matrix()
    RUNTIME_DIR.mkdir(parents=True, exist_ok=True)
    out_path = Path(RUNTIME_DIR) / "connect_ip_matrix_probe_latest.json"
    out_path.write_text(json.dumps(summary, indent=2), encoding="utf-8")
    print(json.dumps(summary, ensure_ascii=True))
    print(f"Matrix summary written to: {out_path}")


if __name__ == "__main__":
    main()

