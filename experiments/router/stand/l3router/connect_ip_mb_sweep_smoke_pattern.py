#!/usr/bin/env python3
import json
from pathlib import Path

from connect_ip_threshold_smoke_pattern import run_size
from masque_stand_runner import CONNECT_IP_CLIENT_CONFIG, compile_singbox, compose_up, docker_bin


def main() -> None:
    docker = docker_bin()
    compile_singbox()
    compose_up(docker, CONNECT_IP_CLIENT_CONFIG)

    sweep_1_10_mb = [mb * 1024 * 1024 for mb in range(1, 11)]
    sweep_10_100_mb = [mb * 1024 * 1024 for mb in range(20, 101, 10)]

    results = []
    stop_reason = None
    for size in sweep_1_10_mb:
        result = run_size(docker, size)
        result["size_mb"] = size // (1024 * 1024)
        result["phase"] = "1_10_mb_step_1"
        results.append(result)
        if not result.get("ok"):
            stop_reason = f"first failure at {result['size_mb']}MB in phase 1_10_mb_step_1"
            break

    if stop_reason is None:
        for size in sweep_10_100_mb:
            result = run_size(docker, size)
            result["size_mb"] = size // (1024 * 1024)
            result["phase"] = "20_100_mb_step_10"
            results.append(result)
            if not result.get("ok"):
                stop_reason = f"first failure at {result['size_mb']}MB in phase 20_100_mb_step_10"
                break
    summary = {
        "pattern": "smoke_like_1kb_chunks_connect_ip",
        "connect_ip_config": CONNECT_IP_CLIENT_CONFIG,
        "sweeps": {
            "1_10_mb_step_1": [i for i in range(1, 11)],
            "20_100_mb_step_10": [i for i in range(20, 101, 10)],
        },
        "stop_reason": stop_reason,
        "results": results,
    }
    out_path = Path(__file__).resolve().parent / "runtime" / "connect_ip_mb_sweep_smoke_pattern.json"
    out_path.write_text(json.dumps(summary, indent=2), encoding="utf-8")
    print(json.dumps(summary, ensure_ascii=True))
    print(f"written: {out_path}")


if __name__ == "__main__":
    main()

