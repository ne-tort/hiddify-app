#!/usr/bin/env python3
"""Smoke: overlay ping + smbclient между mini-hy2-wg-hub-prod-client* (WG через VLESS на хабе)."""
from __future__ import annotations

import subprocess
import sys
import time

CRED = r"wguser%wgpass"
SHARE = "//10.0.0.3/wgshare"
# Жёсткий лимит на каждую проверку (сек).
OP_TIMEOUT_SEC = 5
CONTAINERS = [
    "mini-hy2-wg-hub-prod-client1",
    "mini-hy2-wg-hub-prod-client2",
    "mini-hy2-wg-hub-prod-client3",
    "mini-hy2-wg-hub-prod-client4",
]


def run(cmd: list[str]) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        cmd,
        check=False,
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
    )


def docker_exec(
    container: str, inner: str, timeout_sec: float | None = None
) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        [
            "docker",
            "exec",
            container,
            "sh",
            "-lc",
            inner,
        ],
        check=False,
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
        timeout=timeout_sec,
    )


def wait_running(name: str, timeout_sec: float = 120.0) -> None:
    deadline = time.time() + timeout_sec
    while time.time() < deadline:
        p = run(["docker", "inspect", "-f", "{{.State.Status}}", name])
        if p.returncode == 0 and p.stdout.strip() == "running":
            return
        time.sleep(1.0)
    raise SystemExit(f"timeout: container {name} not running")


def main() -> None:
    try:
        _run()
    except subprocess.TimeoutExpired as e:
        raise SystemExit(f"FAIL: timeout waiting for command: {e}") from e


def _run() -> None:
    for c in CONTAINERS:
        wait_running(c)

    # Дождаться sing-box внутри клиентов (WG + overlay).
    for _ in range(90):
        ok = True
        for c in CONTAINERS:
            p = docker_exec(c, "pidof sing-box >/dev/null", timeout_sec=5.0)
            if p.returncode != 0:
                ok = False
                break
        if ok:
            break
        time.sleep(1.0)
    else:
        raise SystemExit("timeout: sing-box not running in all client containers")

    time.sleep(5.0)

    to = OP_TIMEOUT_SEC + 2.0
    print("[smoke] ping client1 -> 10.0.0.3")
    p = docker_exec(
        CONTAINERS[0],
        f"timeout {OP_TIMEOUT_SEC} ping -c 2 -W 2 10.0.0.3",
        timeout_sec=to,
    )
    if p.returncode != 0:
        print(p.stdout, file=sys.stderr)
        print(p.stderr, file=sys.stderr)
        raise SystemExit("FAIL: ping")
    print(p.stdout)

    print("[smoke] smbclient client1 -> //10.0.0.3/wgshare")
    p = docker_exec(
        CONTAINERS[0],
        f"timeout {OP_TIMEOUT_SEC} smbclient '{SHARE}' -U '{CRED}' -m SMB3 -c 'ls'",
        timeout_sec=to,
    )
    if p.returncode != 0:
        print(p.stdout, file=sys.stderr)
        print(p.stderr, file=sys.stderr)
        raise SystemExit("FAIL: smbclient ls (c1->c2)")
    print(p.stdout)

    print("[smoke] smbclient client2 -> //10.0.0.2/wgshare (c2->c1)")
    p = docker_exec(
        CONTAINERS[1],
        f"timeout {OP_TIMEOUT_SEC} smbclient '//10.0.0.2/wgshare' -U '{CRED}' -m SMB3 -c 'ls'",
        timeout_sec=to,
    )
    if p.returncode != 0:
        print(p.stdout, file=sys.stderr)
        print(p.stderr, file=sys.stderr)
        raise SystemExit("FAIL: smbclient ls (c2->c1)")
    print(p.stdout)

    print("[smoke] OK")


if __name__ == "__main__":
    main()
