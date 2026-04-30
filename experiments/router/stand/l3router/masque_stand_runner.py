#!/usr/bin/env python3
import argparse
import json
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

BYTES_10KB = 10 * 1024
BYTES_500MB = 500 * 1024 * 1024
SMOKE_DEADLINE_SEC = 5.0


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


def compose_up(docker, client_config):
    env = os.environ.copy()
    env["MASQUE_CLIENT_CONFIG"] = client_config
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
    got = wait_for_bytes(docker, SERVER_CONTAINER, sink, byte_count, 5 if byte_count == BYTES_10KB else 30)
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

    start = time.time()
    send_cmd = (
        f"timeout 20 sh -lc '"
        f"dd if=/dev/zero bs=8192 count=1 2>/dev/null | socat -u - SOCKS5:127.0.0.1:{target_host}:{port},socksport=1080 && "
        f"dd if=/dev/zero bs=2048 count=1 2>/dev/null | socat -u - SOCKS5:127.0.0.1:{target_host}:{port},socksport=1080"
        f"'"
    )
    docker_exec(docker, CLIENT_CONTAINER, send_cmd)
    elapsed = time.time() - start
    got = wait_for_bytes(docker, SERVER_CONTAINER, sink, byte_count, 5 if byte_count == BYTES_10KB else 30)
    ok = got >= byte_count and elapsed <= SMOKE_DEADLINE_SEC if byte_count == BYTES_10KB else got >= byte_count
    return {"scenario": "tcp_stream", "bytes_expected": byte_count, "bytes_received": got, "elapsed_sec": round(elapsed, 3), "ok": ok}


def run_tcp_ip(docker, byte_count):
    target_host, port = "10.200.0.2", 5602
    sink = "/tmp/tcp-connect-ip-python.bin"
    docker_exec(docker, IPERF_CONTAINER, f"rm -f {sink}", check=False)
    docker_exec(
        docker,
        IPERF_CONTAINER,
        f"nohup timeout 20 socat -u TCP-LISTEN:{port},reuseaddr OPEN:{sink},creat,append >/tmp/tcp-connect-ip-python.log 2>&1 &",
    )
    wait_tcp_listener(docker, IPERF_CONTAINER, port)
    wait_socks_ready(docker)

    start = time.time()
    chunks = max(1, byte_count // 1024)
    send_cmd = (
        f"timeout 20 sh -lc 'for i in $(seq 1 {chunks}); do "
        f"dd if=/dev/zero bs=1024 count=1 2>/dev/null | "
        f"socat -u - SOCKS5:127.0.0.1:{target_host}:{port},socksport=1080 || exit 1; "
        f"done'"
    )
    docker_exec(docker, CLIENT_CONTAINER, send_cmd)
    elapsed = time.time() - start
    got = wait_for_bytes(docker, IPERF_CONTAINER, sink, byte_count, 5 if byte_count == BYTES_10KB else 30)
    ok = got >= byte_count and elapsed <= SMOKE_DEADLINE_SEC if byte_count == BYTES_10KB else got >= byte_count
    return {"scenario": "tcp_ip", "bytes_expected": byte_count, "bytes_received": got, "elapsed_sec": round(elapsed, 3), "ok": ok}


def run_scenario(docker, scenario, byte_count):
    if scenario == "udp":
        compose_up(docker, DEFAULT_CLIENT_CONFIG)
        return run_udp(docker, byte_count)
    if scenario == "tcp_stream":
        compose_up(docker, DEFAULT_CLIENT_CONFIG)
        return run_tcp_stream(docker, byte_count)
    if scenario == "tcp_ip":
        compose_up(docker, CONNECT_IP_CLIENT_CONFIG)
        return run_tcp_ip(docker, byte_count)
    raise ValueError(f"unsupported scenario: {scenario}")


def main():
    parser = argparse.ArgumentParser(description="Single entrypoint for MASQUE stand scenarios")
    parser.add_argument("--scenario", choices=["udp", "tcp_stream", "tcp_ip", "all"], required=True)
    parser.add_argument("--stress", action="store_true", help="run 500MB transfer instead of 10KB")
    args = parser.parse_args()

    docker = docker_bin()
    byte_count = BYTES_500MB if args.stress else BYTES_10KB
    scenarios = ["udp", "tcp_stream", "tcp_ip"] if args.scenario == "all" else [args.scenario]

    RUNTIME_DIR.mkdir(parents=True, exist_ok=True)
    compile_singbox()

    results = []
    overall_ok = True
    for scenario in scenarios:
        print(f"\n=== Running scenario: {scenario} ({byte_count} bytes) ===")
        try:
            result = run_scenario(docker, scenario, byte_count)
        except Exception as exc:
            result = {"scenario": scenario, "bytes_expected": byte_count, "bytes_received": 0, "elapsed_sec": 0.0, "ok": False, "error": str(exc)}
        results.append(result)
        overall_ok = overall_ok and bool(result.get("ok"))
        print(json.dumps(result, ensure_ascii=True))

    summary = {"stress": args.stress, "bytes": byte_count, "results": results, "ok": overall_ok}
    summary_path = RUNTIME_DIR / "masque_python_runner_summary.json"
    summary_path.write_text(json.dumps(summary, indent=2), encoding="utf-8")
    print(f"\nSummary written to: {summary_path}")
    print(json.dumps(summary, ensure_ascii=True))
    sys.exit(0 if overall_ok else 1)


if __name__ == "__main__":
    main()
