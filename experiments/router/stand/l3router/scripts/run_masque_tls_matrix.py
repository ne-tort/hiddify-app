#!/usr/bin/env python3
"""
Run MASQUE Docker smokes across client configs (TLS / http_layer / uTLS variations).

Requires: Docker, pre-built sing-box image/artifact (masque_stand_runner.compile_singbox).
The default `compile_singbox` uses Go tags `with_masque,with_utls` so H2 + uTLS client rows work.

Usage (from stand/l3router):
  python scripts/run_masque_tls_matrix.py
  python scripts/run_masque_tls_matrix.py --scenario tcp_stream --megabytes 1
  python scripts/run_masque_tls_matrix.py --with-quic-udp

Env:
  MASQUE_STAND_CLIENT_CONFIG — forwarded to masque_stand_runner (per-process override of default client JSON).
"""
from __future__ import annotations

import argparse
import os
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
RUNNER = ROOT / "masque_stand_runner.py"

# (label, client_config_relpath, scenario_override_or_None)
# tcp_stream exercises SOCKS → MASQUE → CONNECT-stream (TLS depends on http_layer).
MATRIX: list[tuple[str, str, str | None]] = [
    ("default-h3", "./configs/masque-client.json", None),
    ("h3-explicit-alpn", "./configs/masque-client-h3-explicit-alpn.json", None),
    ("h2-plain", "./configs/masque-client-h2.json", None),
    ("h2-utls-chrome", "./configs/masque-client-h2-utls-chrome.json", None),
    ("h2-utls-firefox", "./configs/masque-client-h2-utls-firefox.json", None),
    ("connect-ip-h2", "./configs/masque-client-connect-ip-h2.json", None),
    ("connect-ip", "./configs/masque-client-connect-ip.json", None),
    ("http-layer-auto-fb", "./configs/masque-client-http-layer-auto-cache-fallback.json", None),
    ("auto", "./configs/masque-client-auto.json", None),
]


def main() -> int:
    ap = argparse.ArgumentParser(description="MASQUE TLS / uTLS Docker matrix (masque_stand_runner).")
    ap.add_argument("--scenario", default="tcp_stream", help="default scenario when row has no override")
    ap.add_argument("--megabytes", type=int, default=1, help="payload size per scenario (>=1)")
    ap.add_argument(
        "--with-quic-udp",
        action="store_true",
        help="also run scenario udp (TUN+CONNECT-IP, QUIC TLS) for masque-client-connect-ip.json",
    )
    args = ap.parse_args()

    if not RUNNER.is_file():
        print("missing", RUNNER, file=sys.stderr)
        return 1

    extra_udp: list[tuple[str, str]] = []
    if args.with_quic_udp:
        extra_udp.append(("connect-ip-quic-udp", "./configs/masque-client-connect-ip.json"))

    ok_all = True
    for name, rel, scen in MATRIX:
        cfg = (ROOT / rel).resolve()
        if not cfg.is_file():
            print("skip missing file", rel, file=sys.stderr)
            ok_all = False
            continue
        scenario = scen or args.scenario
        env = os.environ.copy()
        env["MASQUE_STAND_CLIENT_CONFIG"] = rel
        print(f"\n=== TLS matrix row: {name} ({rel}) scenario={scenario} ===", flush=True)
        cmd = [
            sys.executable,
            str(RUNNER),
            "--scenario",
            scenario,
            "--megabytes",
            str(args.megabytes),
        ]
        r = subprocess.run(cmd, cwd=str(ROOT), env=env)
        if r.returncode != 0:
            print(f"FAIL {name} exit={r.returncode}", file=sys.stderr)
            ok_all = False

    for name, rel in extra_udp:
        cfg = (ROOT / rel).resolve()
        if not cfg.is_file():
            print("skip missing file", rel, file=sys.stderr)
            ok_all = False
            continue
        env = os.environ.copy()
        env["MASQUE_STAND_CLIENT_CONFIG"] = rel
        print(f"\n=== TLS matrix row: {name} ({rel}) scenario=udp ===", flush=True)
        cmd = [
            sys.executable,
            str(RUNNER),
            "--scenario",
            "udp",
            "--megabytes",
            str(args.megabytes),
        ]
        r = subprocess.run(cmd, cwd=str(ROOT), env=env)
        if r.returncode != 0:
            print(f"FAIL {name} exit={r.returncode}", file=sys.stderr)
            ok_all = False

    return 0 if ok_all else 1


if __name__ == "__main__":
    raise SystemExit(main())
