"""Cross-compile sing-box (linux/amd64) from the fork source tree."""

from __future__ import annotations

import os
import subprocess
import sys

from .paths import ARTIFACTS_DIR, SING_BOX_ROOT

__all__ = ["build_linux_amd64", "ARTIFACTS_DIR", "DEFAULT_TAGS", "SING_BOX_ROOT"]


DEFAULT_TAGS = "with_gvisor,with_clash_api,with_utls,with_l3router"


def build_linux_amd64(
    *,
    tags: str | None = None,
    out_name: str = "sing-box-linux-amd64",
) -> Path:
    """Run `go build` with CGO_ENABLED=0. Returns path to the binary."""
    if not SING_BOX_ROOT.is_dir():
        raise FileNotFoundError(f"sing-box source not found: {SING_BOX_ROOT}")
    tags = tags or os.environ.get("L3ROUTER_GO_TAGS", DEFAULT_TAGS)
    ARTIFACTS_DIR.mkdir(parents=True, exist_ok=True)
    out_path = ARTIFACTS_DIR / out_name
    env = os.environ.copy()
    env["CGO_ENABLED"] = "0"
    env["GOOS"] = "linux"
    env["GOARCH"] = "amd64"
    # Avoid workspace mode picking wrong modules when building from the fork tree.
    env.setdefault("GOWORK", "off")
    cmd = [
        "go",
        "build",
        "-trimpath",
        "-ldflags=-s -w",
        "-tags",
        tags,
        "-o",
        str(out_path),
        "./cmd/sing-box",
    ]
    print(f"[build] cd {SING_BOX_ROOT} && {' '.join(cmd)}", file=sys.stderr)
    subprocess.run(cmd, cwd=str(SING_BOX_ROOT), env=env, check=True)
    print(f"[build] OK -> {out_path}", file=sys.stderr)
    return out_path
