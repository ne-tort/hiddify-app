"""Run stand verification scripts (bash: Git Bash on Windows shares Docker Desktop with run.py)."""

from __future__ import annotations

import os
import shutil
import subprocess
import sys
from pathlib import Path

from .paths import SCRIPTS_DIR, STAND_ROOT


def _bash() -> str:
    override = os.environ.get("L3ROUTER_BASH")
    if override:
        return override
    # On Windows, WSL's bash uses a different Docker engine than Docker Desktop; prefer Git Bash.
    if sys.platform == "win32":
        for base in (
            os.environ.get("ProgramFiles", r"C:\Program Files"),
            os.environ.get("ProgramFiles(x86)", r"C:\Program Files (x86)"),
        ):
            candidate = Path(base) / "Git" / "bin" / "bash.exe"
            if candidate.is_file():
                return str(candidate)
    b = shutil.which("bash")
    if not b:
        raise SystemExit(
            "bash not found: install Git for Windows, WSL, or run tests from Linux. "
            "Override with L3ROUTER_BASH. See experiments/router/stand/l3router/README.md"
        )
    return b


def run_smb_100mb_both_ways() -> None:
    """Full SMB 100 MiB client↔client via l3router (docker)."""
    script = SCRIPTS_DIR / "e2e_smb_clients_100mb.sh"
    if not script.is_file():
        raise FileNotFoundError(script)
    env = os.environ.copy()
    # Ensure base image exists; script can build smb layer if requested
    if os.environ.get("SMB_BUILD_IMAGE", "0") == "1":
        env["SMB_BUILD_IMAGE"] = "1"
    # Relative path so WSL/Git Bash resolve the script when cwd is the stand root.
    script_arg = script.relative_to(STAND_ROOT).as_posix()
    subprocess.run([_bash(), script_arg], cwd=str(STAND_ROOT), env=env, check=True)
    report = STAND_ROOT / "runtime" / "smb_clients_100mb_latest.json"
    if report.is_file():
        print(f"[test] report: {report}", file=sys.stderr)


def run_legacy_local_disk_only() -> None:
    """Offline dd/cp/sha256 (does not traverse l3router). For quick sanity only."""
    script = SCRIPTS_DIR / "legacy" / "smb_transfer_100mb_static.sh"
    if not script.is_file():
        raise FileNotFoundError(script)
    script_arg = script.relative_to(STAND_ROOT).as_posix()
    subprocess.run([_bash(), script_arg], cwd=str(STAND_ROOT), check=True)


def run_ping_peer(peer_ip: str) -> None:
    script = SCRIPTS_DIR / "e2e_ping_peer.sh"
    script_arg = script.relative_to(STAND_ROOT).as_posix()
    subprocess.run([_bash(), script_arg, peer_ip], cwd=str(STAND_ROOT), check=True)
