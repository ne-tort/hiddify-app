#!/usr/bin/env python3
"""
Deploy s-ui binary to a VPS over SSH (same patterns as experiments/.../l3router_stand/deploy.py).

Usage (from vendor/s-ui/deploy):
  python3 deploy_sui.py binary
  python3 deploy_sui.py restart

Config: copy deploy.env.example -> deploy.env and/or deploy.config.json.example -> deploy.config.json
Environment variables override JSON (prefix SUI_DEPLOY_*).

Requires: ssh/scp access to the host, local ../sui or path in config.
Optional YAML: install PyYAML and place deploy.yaml next to this script to merge on top of defaults.
"""

from __future__ import annotations

import hashlib
import json
import os
import subprocess
import sys
import time
from pathlib import Path

SCRIPT_DIR = Path(__file__).resolve().parent
SUI_ROOT = SCRIPT_DIR.parent


def _sha256_file(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as f:
        while True:
            chunk = f.read(1024 * 1024)
            if not chunk:
                break
            h.update(chunk)
    return h.hexdigest()


def _ssh(user: str, host: str, cmd: str) -> str:
    r = subprocess.run(
        ["ssh", f"{user}@{host}", cmd],
        check=True,
        capture_output=True,
        text=True,
    )
    return r.stdout.strip()


def _load_dotenv() -> None:
    env_path = SCRIPT_DIR / "deploy.env"
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


def _load_json_config() -> dict:
    p = SCRIPT_DIR / "deploy.config.json"
    if not p.is_file():
        return {}
    return json.loads(p.read_text(encoding="utf-8"))


def _load_yaml_optional() -> dict:
    ypath = SCRIPT_DIR / "deploy.yaml"
    if not ypath.is_file():
        return {}
    try:
        import yaml  # type: ignore
    except ImportError:
        print(
            "[deploy] deploy.yaml present but PyYAML not installed; pip install pyyaml or use deploy.config.json",
            file=sys.stderr,
        )
        return {}
    return yaml.safe_load(ypath.read_text(encoding="utf-8")) or {}


def _merge_config() -> dict:
    cfg: dict = {}
    cfg.update(_load_json_config())
    cfg.update(_load_yaml_optional())
    dock = cfg.get("docker") if isinstance(cfg.get("docker"), dict) else {}
    if dock:
        cfg.setdefault("docker_container", dock.get("container", ""))
        cfg.setdefault("docker_host_tmp", dock.get("host_tmp", "/tmp/sui.upload"))
        cfg.setdefault("docker_path_in_container", dock.get("path_in_container", "/app/sui"))
        cfg.setdefault("docker_wait_timeout", dock.get("wait_timeout_sec", 120))
    # env overrides
    def g(name: str, key: str, default=None):
        v = os.environ.get(name)
        if v is not None and str(v).strip() != "":
            cfg[key] = v.strip() if isinstance(default, str) else v
        elif key not in cfg and default is not None:
            cfg[key] = default

    g("SUI_DEPLOY_HOST", "host", None)
    g("SUI_DEPLOY_USER", "user", "root")
    g("SUI_DEPLOY_LOCAL_BINARY", "local_binary", str(SUI_ROOT / "sui"))
    g("SUI_DEPLOY_REMOTE_BINARY", "remote_binary", "/usr/local/s-ui/sui")
    g("SUI_DEPLOY_SYSTEMD_UNIT", "systemd_unit", "s-ui")
    sk = os.environ.get("SUI_DEPLOY_SKIP_RESTART", "").strip().lower()
    if sk in ("1", "true", "yes", "on"):
        cfg["skip_restart"] = True
    g("SUI_DEPLOY_RESTART_CMD", "restart_cmd", "")
    g("SUI_DEPLOY_DOCKER_CONTAINER", "docker_container", cfg.get("docker_container", ""))
    g("SUI_DEPLOY_DOCKER_BINARY_TMP", "docker_host_tmp", cfg.get("docker_host_tmp", "/tmp/sui.upload"))
    g("SUI_DEPLOY_DOCKER_BINARY_IN_CONTAINER", "docker_path_in_container", cfg.get("docker_path_in_container", "/app/sui"))
    wt = os.environ.get("SUI_DEPLOY_DOCKER_WAIT_TIMEOUT")
    if wt:
        cfg["docker_wait_timeout"] = int(wt)
    elif "docker_wait_timeout" not in cfg:
        cfg["docker_wait_timeout"] = int(cfg.get("docker_wait_timeout") or 120)
    return cfg


def _wait_docker_running(user: str, host: str, container: str, timeout_sec: int) -> None:
    script = (
        f"i=0; while [ $i -lt {timeout_sec} ]; do "
        f's=$(docker inspect -f "{{{{.State.Status}}}}" {container} 2>/dev/null || echo unknown); '
        f'r=$(docker inspect -f "{{{{.State.Restarting}}}}" {container} 2>/dev/null || echo false); '
        f'[ "$s" = "running" ] && [ "$r" = "false" ] && exit 0; '
        f"sleep 1; i=$((i+1)); done; "
        f"echo timeout; docker logs --tail 40 {container} 2>/dev/null; exit 1"
    )
    subprocess.run(["ssh", f"{user}@{host}", script], check=True)


def deploy_binary(cfg: dict) -> None:
    host = cfg.get("host")
    if not host:
        sys.exit("host is required (deploy.config.json or SUI_DEPLOY_HOST)")
    user = cfg.get("user") or "root"
    local = Path(cfg.get("local_binary") or str(SUI_ROOT / "sui")).resolve()
    if not local.is_file():
        sys.exit(f"local binary not found: {local}")

    remote = str(cfg.get("remote_binary") or "/usr/local/s-ui/sui")
    container = str(cfg.get("docker_container") or "").strip()
    unit = str(cfg.get("systemd_unit") or "s-ui")
    skip_restart = bool(cfg.get("skip_restart"))
    restart_cmd = str(cfg.get("restart_cmd") or "").strip()
    docker_tmp = str(cfg.get("docker_host_tmp") or "/tmp/sui.upload")
    path_in_c = str(cfg.get("docker_path_in_container") or "/app/sui")
    wait_t = int(cfg.get("docker_wait_timeout") or 120)

    local_sha = _sha256_file(local)

    if container:
        target = f"{user}@{host}:{docker_tmp}.incoming"
        print(f"[deploy] scp {local} -> {target}", file=sys.stderr)
        subprocess.run(["scp", str(local), target], check=True)
        stop = f"docker stop {container} 2>/dev/null || true"
        print(f"[deploy] ssh {stop!r}", file=sys.stderr)
        subprocess.run(["ssh", f"{user}@{host}", stop], check=False)
        push = (
            f"mv -f {docker_tmp}.incoming {docker_tmp} && chmod +x {docker_tmp} && "
            f"docker cp {docker_tmp} {container}:{path_in_c}"
        )
        print(f"[deploy] ssh {push!r}", file=sys.stderr)
        subprocess.run(["ssh", f"{user}@{host}", push], check=True)
        verify = (
            f"docker cp {container}:{path_in_c} {docker_tmp}.verify && "
            f"sha256sum {docker_tmp}.verify | awk '{{print $1}}'"
        )
        remote_sha = _ssh(user, host, verify)
        if remote_sha != local_sha:
            raise SystemExit(f"sha mismatch after docker cp: local={local_sha} remote={remote_sha}")
        start = f"docker start {container}"
        print(f"[deploy] ssh {start!r}", file=sys.stderr)
        subprocess.run(["ssh", f"{user}@{host}", start], check=True)
        _wait_docker_running(user, host, container, wait_t)
        print("[deploy] binary OK (docker)", file=sys.stderr)
        return

    # systemd / plain file (atomic: scp to .tmp-upload then mv)
    remote_tmp = f"{remote}.tmp-upload"
    target = f"{user}@{host}:{remote_tmp}"
    print(f"[deploy] scp {local} -> {target}", file=sys.stderr)
    subprocess.run(["scp", str(local), target], check=True)
    parent = str(Path(remote).parent)
    _ssh(user, host, f"mkdir -p {parent}")
    mv = f"mv -f {remote_tmp} {remote} && chmod +x {remote}"
    print(f"[deploy] ssh {mv!r}", file=sys.stderr)
    subprocess.run(["ssh", f"{user}@{host}", mv], check=True)
    remote_sha = _ssh(user, host, f"sha256sum {remote} | awk '{{print $1}}'")
    if remote_sha != local_sha:
        raise SystemExit(f"sha mismatch: local={local_sha} remote={remote_sha}")

    if skip_restart:
        print("[deploy] skip restart (skip_restart)", file=sys.stderr)
        return
    if restart_cmd:
        print(f"[deploy] ssh restart: {restart_cmd!r}", file=sys.stderr)
        subprocess.run(["ssh", f"{user}@{host}", restart_cmd], check=True)
        print("[deploy] binary OK", file=sys.stderr)
        return
    if not unit:
        print("[deploy] no systemd_unit / restart_cmd; done", file=sys.stderr)
        return
    rc = f"systemctl restart {unit} && systemctl --no-pager --full status {unit} | sed -n '1,25p'"
    print(f"[deploy] ssh {rc!r}", file=sys.stderr)
    subprocess.run(["ssh", f"{user}@{host}", rc], check=True)
    print("[deploy] binary OK", file=sys.stderr)


def restart_only(cfg: dict) -> None:
    host = cfg.get("host")
    if not host:
        sys.exit("host is required")
    user = cfg.get("user") or "root"
    unit = str(cfg.get("systemd_unit") or "s-ui")
    restart_cmd = str(cfg.get("restart_cmd") or "").strip()
    container = str(cfg.get("docker_container") or "").strip()
    wait_t = int(cfg.get("docker_wait_timeout") or 120)
    if restart_cmd:
        subprocess.run(["ssh", f"{user}@{host}", restart_cmd], check=True)
        return
    if container:
        subprocess.run(["ssh", f"{user}@{host}", f"docker restart {container}"], check=True)
        _wait_docker_running(user, host, container, wait_t)
        return
    subprocess.run(
        ["ssh", f"{user}@{host}", f"systemctl restart {unit} && systemctl --no-pager status {unit} | head -25"],
        check=True,
    )


def main() -> None:
    _load_dotenv()
    cfg = _merge_config()
    if len(sys.argv) < 2:
        print("Usage: deploy_sui.py binary|restart", file=sys.stderr)
        sys.exit(2)
    cmd = sys.argv[1].lower()
    if cmd == "binary":
        deploy_binary(cfg)
    elif cmd == "restart":
        restart_only(cfg)
    else:
        print("Unknown command:", cmd, file=sys.stderr)
        sys.exit(2)


if __name__ == "__main__":
    main()
