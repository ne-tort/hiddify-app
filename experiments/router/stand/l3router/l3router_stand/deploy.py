"""Deploy server config (and optionally binary) to VPS via scp/ssh."""



from __future__ import annotations



import os

import subprocess

import sys



from .paths import CONFIGS_DIR


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
        f"[ \"$s\" = \"running\" ] && exit 0; "
        f"sleep 1; i=$((i+1)); done; "
        f"echo 'timeout waiting for docker container {container}'; exit 1"
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

    print(f"[deploy] scp {cfg} -> {target}", file=sys.stderr)

    subprocess.run(["scp", str(cfg), target], check=True)

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

        subprocess.run(["scp", str(local_binary), target], check=True)

        push_cmd = (

            f"chmod +x {tmp} && "

            f"docker cp {tmp} {container}:/usr/local/bin/sing-box"

        )

        print(f"[deploy] ssh {user}@{host} {push_cmd!r}", file=sys.stderr)

        subprocess.run(["ssh", f"{user}@{host}", push_cmd], check=True)

        start_cmd = f"docker start {container}"

        print(f"[deploy] ssh {user}@{host} {start_cmd!r}", file=sys.stderr)

        subprocess.run(["ssh", f"{user}@{host}", start_cmd], check=True)

        _wait_remote_container_running(user, host, container)

        print("[deploy] binary OK", file=sys.stderr)

        return



    _ensure_remote_parent_dir(user, host, str(remote_path))

    target = f"{user}@{host}:{remote_path}"

    print(f"[deploy] scp binary -> {target}", file=sys.stderr)

    subprocess.run(["scp", str(local_binary), target], check=True)

    chmod_cmd = f"chmod +x {remote_path}"

    subprocess.run(["ssh", f"{user}@{host}", chmod_cmd], check=True)

    _restart_remote(user, host, service=service)

    print("[deploy] binary OK", file=sys.stderr)


