#!/usr/bin/env python3
"""
Single entry point for the l3router stand: build, deploy, docker clients, tests.

Usage:
  python run.py build
  python run.py cleanup
  python run.py deploy [--with-binary]
  python run.py clients [--stack static|smb|reality] [--smb-build]
  python run.py test [--smb] [--legacy-local-disk]
  python run.py all [--with-binary] [--smb-build]

Environment: copy .env.example to .env or export variables (see README).
For SMB/Reality e2e set L3ROUTER_SERVER_CONFIG=hub.vps.synced.json before deploy/all.
With --with-binary, the binary is uploaded before the config so the server image accepts new JSON (e.g. l3router peers schema).
"""

from __future__ import annotations

import argparse
import os
import sys
from pathlib import Path

# Allow `python run.py` from stand directory without installing a package
_STAND = Path(__file__).resolve().parent
if str(_STAND) not in sys.path:
    sys.path.insert(0, str(_STAND))


def _load_dotenv() -> None:
    env_path = _STAND / ".env"
    if not env_path.is_file():
        return
    for raw in env_path.read_text(encoding="utf-8").splitlines():
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        key, _, val = line.partition("=")
        key = key.strip()
        val = val.strip().strip("'").strip('"')
        if key:
            os.environ.setdefault(key, val)

from l3router_stand import build as build_mod
from l3router_stand import clients as clients_mod
from l3router_stand import deploy as deploy_mod
from l3router_stand import tests as tests_mod


def main() -> None:
    _load_dotenv()
    p = argparse.ArgumentParser(description="l3router integration stand")
    sub = p.add_subparsers(dest="cmd", required=True)

    sub.add_parser("build", help="Cross-compile sing-box linux/amd64 -> artifacts/")

    dp = sub.add_parser("deploy", help="scp server config (+ optional binary)")
    dp.add_argument(
        "--with-binary",
        action="store_true",
        help="Also upload artifacts/sing-box-linux-amd64 and restart service",
    )

    cp = sub.add_parser("clients", help="docker compose up")
    cp.add_argument(
        "--stack",
        choices=("static", "smb", "reality"),
        default="smb",
        help="static=two host-net clients; smb=SMB e2e pair; reality=Reality e2e",
    )
    cp.add_argument(
        "--smb-build",
        action="store_true",
        help="With stack=smb: run docker compose up --build",
    )

    tp = sub.add_parser("test", help="SMB 100 MiB both ways, or --legacy-local-disk")
    tp.add_argument(
        "--legacy-local-disk",
        action="store_true",
        help="Offline dd/cp/sha256 only (not through l3router)",
    )

    sub.add_parser(
        "cleanup",
        help="docker compose down for all stand stacks; optional VPS docker rm l3router* via ssh",
    )

    ap = sub.add_parser("all", help="build -> deploy -> clients(smb) -> test(smb)")
    ap.add_argument("--with-binary", action="store_true")
    ap.add_argument(
        "--smb-build",
        action="store_true",
        help="docker compose --build for SMB stack",
    )
    ap.add_argument("--skip-deploy", action="store_true")
    ap.add_argument("--skip-clients", action="store_true")
    ap.add_argument("--skip-test", action="store_true")

    args = p.parse_args()

    if args.cmd == "build":
        build_mod.build_linux_amd64()
        return

    if args.cmd == "cleanup":
        clients_mod.down_all()
        deploy_mod.cleanup_remote_l3router_docker()
        return

    if args.cmd == "deploy":
        if args.with_binary:
            bin_path = build_mod.ARTIFACTS_DIR / "sing-box-linux-amd64"
            if not bin_path.is_file():
                print("Binary missing; running build first.", file=sys.stderr)
                build_mod.build_linux_amd64()
            deploy_mod.deploy_server_binary(bin_path)
        deploy_mod.deploy_server_config()
        return

    if args.cmd == "clients":
        if args.stack == "static":
            clients_mod.up_static_clients()
        elif args.stack == "reality":
            clients_mod.build_base_image()
            clients_mod.up_e2e_reality_clients()
        else:
            clients_mod.build_base_image()
            clients_mod.up_smb_clients(build=args.smb_build)
        return

    if args.cmd == "test":
        if args.legacy_local_disk:
            tests_mod.run_legacy_local_disk_only()
        else:
            os.environ.setdefault("SMB_BUILD_IMAGE", "0")
            tests_mod.run_smb_100mb_both_ways()
        return

    if args.cmd == "all":
        build_mod.build_linux_amd64()
        if not args.skip_deploy:
            if args.with_binary:
                deploy_mod.deploy_server_binary(
                    build_mod.ARTIFACTS_DIR / "sing-box-linux-amd64"
                )
            deploy_mod.deploy_server_config()
        if not args.skip_clients:
            clients_mod.build_base_image()
            clients_mod.up_smb_clients(build=args.smb_build)
        if not args.skip_test:
            os.environ.setdefault("SMB_BUILD_IMAGE", "0")
            tests_mod.run_smb_100mb_both_ways()
        return


if __name__ == "__main__":
    main()
