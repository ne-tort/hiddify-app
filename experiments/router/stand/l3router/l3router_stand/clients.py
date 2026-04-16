"""Docker Compose: build image and/or start client stacks."""

from __future__ import annotations

import os
import subprocess
import sys
from pathlib import Path

from . import build as build_mod
from .paths import STAND_ROOT


def _compose_prefix() -> list[str]:
    # Default: Docker Compose v2 plugin (`docker compose`). Override with e.g. `docker-compose` v1 binary.
    return os.environ.get("DOCKER_COMPOSE", "docker compose").split()


def _compose(
    compose_file: str,
    args: list[str],
    *,
    cwd: Path | None = None,
) -> None:
    cwd = cwd or STAND_ROOT
    cmd = _compose_prefix() + ["-f", compose_file, *args]
    print(f"[clients] cd {cwd} && {' '.join(cmd)}", file=sys.stderr)
    subprocess.run(cmd, cwd=str(cwd), check=True)


def build_base_image() -> None:
    """Build sing-box-l3router:local (needed for SMB client image)."""
    artifact = build_mod.ARTIFACTS_DIR / "sing-box-linux-amd64"
    if not artifact.is_file():
        build_mod.build_linux_amd64()
    _compose(
        "docker-compose.l3router-static-clients.yml",
        ["build"],
    )


def up_static_clients() -> None:
    """Two static TUN clients (host network)."""
    _compose(
        "docker-compose.l3router-static-clients.yml",
        ["up", "-d", "--build"],
    )


def up_e2e_reality_clients() -> None:
    """Reality test clients (requires image sing-box-l3router:local)."""
    _compose("docker-compose.l3router-e2e-reality.yml", ["up", "-d"])


def up_smb_clients(*, build: bool) -> None:
    """SMB e2e client pair (builds smb layer; needs base image)."""
    # Always recreate containers so fresh sing-box image/code is actually applied.
    args = ["up", "-d", "--force-recreate", "--remove-orphans"]
    # Default to --build to guarantee client image picks up latest base binary.
    force_build = os.environ.get("L3ROUTER_SMB_FORCE_BUILD", "1").strip().lower()
    force_build_enabled = force_build not in ("0", "false", "no", "off")
    if build or force_build_enabled:
        args.append("--build")
    else:
        args.append("--no-build")
    _compose("docker-compose.l3router-e2e-reality-smb.yml", args)


def down_all() -> None:
    for name in (
        "docker-compose.l3router-e2e-reality-smb.yml",
        "docker-compose.l3router-e2e-reality.yml",
        "docker-compose.l3router-static-clients.yml",
    ):
        try:
            _compose(name, ["down"])
        except subprocess.CalledProcessError:
            pass
    # compose down can leave fixed container_name instances if project labels differ
    for cname in (
        "l3router-smb-client1",
        "l3router-smb-client2",
        "l3router-e2e-client1",
        "l3router-e2e-client2",
        "l3router-client-a",
        "l3router-client-b",
    ):
        subprocess.run(
            ["docker", "rm", "-f", cname],
            cwd=str(STAND_ROOT),
            check=False,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )
