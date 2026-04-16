"""Deploy server config (and optionally binary) to VPS via scp/ssh."""



from __future__ import annotations



import os

import subprocess

import sys
import tempfile
import hashlib



from .paths import CONFIGS_DIR


def _sha256_file(path: str) -> str:
    h = hashlib.sha256()
    with open(path, "rb") as f:
        while True:
            chunk = f.read(1024 * 1024)
            if not chunk:
                break
            h.update(chunk)
    return h.hexdigest()


def _ssh_output(user: str, host: str, command: str) -> str:
    proc = subprocess.run(
        ["ssh", f"{user}@{host}", command],
        check=True,
        capture_output=True,
        text=True,
    )
    return proc.stdout.strip()


def _normalized_config_for_scp(cfg_path: str) -> tuple[str, str | None]:
    """
    Return path for scp with UTF-8 BOM stripped if present.
    Returns (path_to_upload, temp_path_to_cleanup_or_none).
    """
    raw = open(cfg_path, "rb").read()
    bom = b"\xef\xbb\xbf"
    if not raw.startswith(bom):
        return cfg_path, None
    cleaned = raw[len(bom) :]
    tmp = tempfile.NamedTemporaryFile(
        prefix="l3router-config-",
        suffix=".json",
        delete=False,
    )
    try:
        tmp.write(cleaned)
        tmp.flush()
        return tmp.name, tmp.name
    finally:
        tmp.close()


def _wait_remote_container_running(
    user: str,
    host: str,
    container: str,
    *,
    timeout_sec: int | None = None,
) -> None:
    """Wait until `docker inspect` reports State.Status==running (e.g. after restart)."""
    t = timeout_sec
    if t is None:
        t = int(os.environ.get("L3ROUTER_DOCKER_WAIT_TIMEOUT", "120"))
    script = (
        f"i=0; while [ $i -lt {t} ]; do "
        f"s=$(docker inspect -f '{{{{.State.Status}}}}' {container} 2>/dev/null "
        f"|| echo unknown); "
        f"r=$(docker inspect -f '{{{{.State.Restarting}}}}' {container} 2>/dev/null || echo false); "
        f"[ \"$s\" = \"running\" ] && [ \"$r\" = \"false\" ] && exit 0; "
        f"sleep 1; i=$((i+1)); done; "
        f"echo 'timeout waiting healthy docker container {container}'; "
        f"docker inspect -f 'status={{{{.State.Status}}}} restarting={{{{.State.Restarting}}}} error={{{{.State.Error}}}}' {container} 2>/dev/null || true; "
        f"docker logs --tail 80 {container} 2>/dev/null || true; "
        f"exit 1"
    )
    print(
        f"[deploy] ssh {user}@{host} wait docker status=running ({container}, up to {t}s)",
        file=sys.stderr,
    )
    subprocess.run(["ssh", f"{user}@{host}", script], check=True)


def _ensure_remote_parent_dir(
    user: str,
    host: str,
    remote_path: str,
) -> None:

    parent = os.path.dirname(remote_path.replace("\\", "/"))

    if not parent or parent == ".":

        return

    cmd = f"mkdir -p {parent}"

    print(f"[deploy] ssh {user}@{host} {cmd!r}", file=sys.stderr)

    subprocess.run(["ssh", f"{user}@{host}", cmd], check=True)





def _restart_remote(

    user: str,

    host: str,

    *,

    service: str | None = None,

) -> None:

    skip = os.environ.get("L3ROUTER_SKIP_REMOTE_RESTART", "").strip().lower()

    if skip in ("1", "true", "yes", "on"):

        print(

            "[deploy] skip remote restart (L3ROUTER_SKIP_REMOTE_RESTART)",

            file=sys.stderr,

        )

        return

    custom = os.environ.get("L3ROUTER_REMOTE_RESTART_CMD", "").strip()

    if custom:

        print(f"[deploy] ssh {user}@{host} remote: {custom!r}", file=sys.stderr)

        subprocess.run(["ssh", f"{user}@{host}", custom], check=True)

        return

    svc = (

        service

        if service is not None

        else os.environ.get("L3ROUTER_SERVER_SERVICE", "sing-box")

    )

    if not svc:

        print(

            "[deploy] skip restart: set L3ROUTER_REMOTE_RESTART_CMD "

            "or L3ROUTER_SERVER_SERVICE",

            file=sys.stderr,

        )

        return

    ssh_cmd = (

        f"systemctl restart {svc} && "

        f"systemctl --no-pager --full status {svc} | sed -n '1,20p'"

    )

    print(f"[deploy] ssh {user}@{host} {ssh_cmd!r}", file=sys.stderr)

    subprocess.run(["ssh", f"{user}@{host}", ssh_cmd], check=True)





def cleanup_remote_l3router_docker(

    *,

    host: str | None = None,

    user: str | None = None,

) -> None:

    """Remove containers with name containing l3router on remote host (optional)."""

    host = host or os.environ.get("L3ROUTER_SERVER_HOST")

    if not host:

        print(

            "[cleanup] skip remote docker: L3ROUTER_SERVER_HOST not set",

            file=sys.stderr,

        )

        return

    user = user or os.environ.get("L3ROUTER_SERVER_USER", "root")

    remote = f"{user}@{host}"

    script = (

        "docker rm -f $(docker ps -aq --filter name=l3router) 2>/dev/null || true; "

        "docker ps -a --filter name=l3router --format '{{.Names}}' || true"

    )

    print(f"[cleanup] ssh {remote} docker cleanup l3router*", file=sys.stderr)

    subprocess.run(["ssh", remote, script], check=False)





def deploy_server_config(

    *,

    host: str | None = None,

    user: str | None = None,

    remote_config: str | None = None,

    service: str | None = None,

    config_local: str | None = None,

) -> None:

    host = host or os.environ.get("L3ROUTER_SERVER_HOST")

    if not host:

        raise SystemExit("L3ROUTER_SERVER_HOST is required for deploy")

    user = user or os.environ.get("L3ROUTER_SERVER_USER", "root")

    remote_config = remote_config or os.environ.get(

        "L3ROUTER_SERVER_CONFIG_PATH", "/etc/sing-box/config.json"

    )

    default_name = os.environ.get(

        "L3ROUTER_SERVER_CONFIG", "server.l3router.static.json"

    )

    cfg = CONFIGS_DIR / (config_local or default_name)

    if not cfg.is_file():

        raise FileNotFoundError(cfg)

    _ensure_remote_parent_dir(user, host, remote_config)

    target = f"{user}@{host}:{remote_config}"
    remote_tmp = f"{remote_config}.tmp-upload"

    upload_path, cleanup_tmp = _normalized_config_for_scp(str(cfg))
    local_cfg_sha = _sha256_file(upload_path)
    print(f"[deploy] scp {upload_path} -> {target}", file=sys.stderr)
    try:
        subprocess.run(["scp", upload_path, f"{user}@{host}:{remote_tmp}"], check=True)
        subprocess.run(
            ["ssh", f"{user}@{host}", f"mv {remote_tmp} {remote_config}"],
            check=True,
        )
    finally:
        if cleanup_tmp:
            try:
                os.remove(cleanup_tmp)
            except OSError:
                pass

    remote_cfg_sha = _ssh_output(
        user,
        host,
        f"sha256sum {remote_config} | awk '{{print $1}}'",
    )
    if local_cfg_sha != remote_cfg_sha:
        raise RuntimeError(
            f"deployed config sha mismatch: local={local_cfg_sha} remote={remote_cfg_sha}"
        )

    _restart_remote(user, host, service=service)

    print("[deploy] config OK", file=sys.stderr)





def deploy_server_binary(

    local_binary: "os.PathLike[str]",

    *,

    host: str | None = None,

    user: str | None = None,

    remote_path: str | None = None,

    service: str | None = None,

) -> None:

    host = host or os.environ.get("L3ROUTER_SERVER_HOST")

    if not host:

        raise SystemExit("L3ROUTER_SERVER_HOST is required")

    user = user or os.environ.get("L3ROUTER_SERVER_USER", "root")

    remote_path = remote_path or os.environ.get(

        "L3ROUTER_SERVER_BINARY_PATH", "/usr/local/bin/sing-box"

    )

    container = os.environ.get("L3ROUTER_SERVER_DOCKER_CONTAINER", "").strip()

    if container:

        tmp = os.environ.get(

            "L3ROUTER_SERVER_DOCKER_BINARY_TMP", "/tmp/sing-box-linux-amd64"

        )

        _ensure_remote_parent_dir(user, host, tmp)

        target = f"{user}@{host}:{tmp}"

        print(

            f"[deploy] scp binary -> {target} then docker cp -> {container}",

            file=sys.stderr,

        )

        # Stop first so we are not racing a crash loop, and docker cp works on a stopped container.

        stop_cmd = f"docker stop {container} 2>/dev/null || true"

        print(f"[deploy] ssh {user}@{host} {stop_cmd!r}", file=sys.stderr)

        subprocess.run(["ssh", f"{user}@{host}", stop_cmd], check=False)

        local_bin = str(local_binary)
        local_bin_sha = _sha256_file(local_bin)
        subprocess.run(["scp", local_bin, target], check=True)

        push_cmd = (

            f"chmod +x {tmp} && "

            f"docker cp {tmp} {container}:/usr/local/bin/sing-box"

        )

        print(f"[deploy] ssh {user}@{host} {push_cmd!r}", file=sys.stderr)

        subprocess.run(["ssh", f"{user}@{host}", push_cmd], check=True)

        verify_tmp = os.environ.get(
            "L3ROUTER_SERVER_DOCKER_BINARY_VERIFY_TMP", "/tmp/sing-box.verify"
        )
        remote_bin_sha = _ssh_output(
            user,
            host,
            (
                f"docker cp {container}:/usr/local/bin/sing-box {verify_tmp} && "
                f"sha256sum {verify_tmp} | awk '{{print $1}}'"
            ),
        )
        if remote_bin_sha != local_bin_sha:
            raise RuntimeError(
                f"deployed binary sha mismatch: local={local_bin_sha} remote={remote_bin_sha}"
            )

        start_cmd = f"docker start {container}"

        print(f"[deploy] ssh {user}@{host} {start_cmd!r}", file=sys.stderr)

        subprocess.run(["ssh", f"{user}@{host}", start_cmd], check=True)

        _wait_remote_container_running(user, host, container)

        print("[deploy] binary OK", file=sys.stderr)

        return



    _ensure_remote_parent_dir(user, host, str(remote_path))

    target = f"{user}@{host}:{remote_path}"

    print(f"[deploy] scp binary -> {target}", file=sys.stderr)

    local_bin = str(local_binary)
    local_bin_sha = _sha256_file(local_bin)
    subprocess.run(["scp", local_bin, target], check=True)

    chmod_cmd = f"chmod +x {remote_path}"

    subprocess.run(["ssh", f"{user}@{host}", chmod_cmd], check=True)

    remote_bin_sha = _ssh_output(
        user,
        host,
        f"sha256sum {remote_path} | awk '{{print $1}}'",
    )
    if remote_bin_sha != local_bin_sha:
        raise RuntimeError(
            f"deployed binary sha mismatch: local={local_bin_sha} remote={remote_bin_sha}"
        )

    _restart_remote(user, host, service=service)

    print("[deploy] binary OK", file=sys.stderr)


