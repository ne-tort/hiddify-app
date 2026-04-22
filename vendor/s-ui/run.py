#!/usr/bin/env python3
"""
Единая точка входа для стенда s-ui: сборка образа на **этой** машине (кэш Docker/Go из
Dockerfile), выгрузка готового образа на VPS, подъём compose **без** сборки на сервере.

Почему так: сборка на VPS (`ssh … docker compose build`) каждый раз тянет контекст, холодный
go mod/build и выглядит как «зависание» — это долгий полный билд. Локальный `docker compose build`
использует BuildKit и кэши слоёв; передача `docker save | ssh docker load` не дублирует компиляцию.

  python run.py deploy        # build -> (опц.) compose down на VPS -> save|load -> scp compose -> up --no-build -> (опц.) image prune
  python run.py build           # только локальная сборка образа s-ui-local
  python run.py ship-image      # только docker save | ssh docker load
  python run.py push-compose    # только scp docker-compose на VPS + каталоги db/cert
  python run.py up              # на VPS: docker compose up -d --no-build (образ уже загружен)

  python run.py remote-build    # редко: полная синхронизация исходников + build на VPS (медленно)
  python run.py sync            # только синхронизация исходников (без образа)

  python run.py certbot | verify | all | binary | redeploy-binary | restart — как раньше.

Конфиг: run.env / deploy/deploy.env (SUI_RUN_HOST, SUI_DEPLOY_*).
Деплой: SUI_RUN_DEPLOY_DOWN_FIRST=1 (по умолчанию) — перед загрузкой образа выполнить на VPS
`docker compose down --remove-orphans`, чтобы контейнеры корректно пересоздались с новым образом.
SUI_RUN_DEPLOY_IMAGE_PRUNE=1 — после `up` выполнить `docker image prune -f` (убирает «висячие»
слои после смены тега на тот же :latest).

Опционально: SUI_RUN_COMPOSE_PROFILES=le (через запятую) — передать в `docker compose` для up/down
(например профиль `le`: фоновый certbot-renew в docker-compose.stand.yml). Без этого — только s-ui-local.
"""

from __future__ import annotations

import argparse
import os
import shlex
import shutil
import subprocess
import sys
from pathlib import Path

_SUI_ROOT = Path(__file__).resolve().parent
_REPO_ROOT = _SUI_ROOT.parent.parent

# Имя образа из docker-compose.stand.yml / docker-compose.local.yml (service s-ui-local).
_STAND_IMAGE = "s-ui-local-hiddify:latest"
_EXTRACT_CTR = "s-ui-extract-binary-tmp"


def _docker_subprocess_env() -> dict[str, str]:
    e = os.environ.copy()
    e.setdefault("DOCKER_BUILDKIT", "1")
    e.setdefault("COMPOSE_DOCKER_CLI_BUILD", "1")
    return e


def _load_dotenv() -> None:
    for name in ("run.env",):
        p = _SUI_ROOT / name
        if not p.is_file():
            continue
        for raw in p.read_text(encoding="utf-8").splitlines():
            line = raw.strip()
            if not line or line.startswith("#"):
                continue
            key, _, val = line.partition("=")
            key = key.strip()
            val = val.strip().strip("'").strip('"')
            if key:
                # Environment variables provided by the caller should have priority over run.env,
                # so ad-hoc deploy target overrides (e.g. SUI_RUN_HOST=...) work as expected.
                os.environ.setdefault(key, val)
    dep = _SUI_ROOT / "deploy" / "deploy.env"
    if dep.is_file():
        for raw in dep.read_text(encoding="utf-8").splitlines():
            line = raw.strip()
            if not line or line.startswith("#"):
                continue
            key, _, val = line.partition("=")
            key = key.strip()
            val = val.strip().strip("'").strip('"')
            if key:
                os.environ.setdefault(key, val)


def _cfg() -> dict:
    host = (os.environ.get("SUI_RUN_HOST") or os.environ.get("SUI_DEPLOY_HOST") or "").strip()
    user = (os.environ.get("SUI_RUN_USER") or os.environ.get("SUI_DEPLOY_USER") or "root").strip()
    remote_root = os.environ.get("SUI_RUN_REMOTE_ROOT", "/opt/hiddify-app").strip()
    compose = os.environ.get("SUI_RUN_COMPOSE_FILE", "docker-compose.stand.yml").strip()
    domain = os.environ.get("SUI_RUN_VERIFY_DOMAIN", "work.ai-qwerty.ru").strip()
    port = os.environ.get("SUI_RUN_VERIFY_PORT", "2095").strip()
    cert_domain = os.environ.get("SUI_RUN_CERTBOT_DOMAIN", domain).strip()
    cert_email = os.environ.get("SUI_RUN_CERTBOT_EMAIL", "").strip()
    return {
        "host": host,
        "user": user,
        "remote_root": remote_root,
        "compose": compose,
        "verify_domain": domain,
        "verify_port": port,
        "cert_domain": cert_domain,
        "cert_email": cert_email,
    }


def _require_cfg(cfg: dict) -> None:
    if not cfg.get("host"):
        sys.exit(
            "Задайте SUI_RUN_HOST или SUI_DEPLOY_HOST (run.env / deploy.env / окружение)."
        )


def _ssh(cfg: dict, remote_script: str, *, check: bool = True) -> subprocess.CompletedProcess:
    """Выполнить shell на удалённом хосте (одна сессия ssh)."""
    _require_cfg(cfg)
    h = cfg["host"]
    u = cfg["user"]
    return subprocess.run(
        ["ssh", "-o", "BatchMode=yes", f"{u}@{h}", remote_script],
        check=check,
    )


def _have_rsync() -> bool:
    return shutil.which("rsync") is not None


def _sync_rsync(cfg: dict) -> None:
    r = cfg["remote_root"].rstrip("/")
    u = cfg["user"]
    h = cfg["host"]
    target = f"{u}@{h}:{r}/"
    excludes = [
        "--exclude=frontend/node_modules",
        "--exclude=db",
        "--exclude=.git",
        "--exclude=*.db",
    ]
    # s-ui
    subprocess.run(
        [
            "rsync",
            "-az",
            "--delete",
            *excludes,
            f"{_SUI_ROOT}/",
            f"{target}vendor/s-ui/",
        ],
        check=True,
    )
    # hiddify-sing-box
    sing = _REPO_ROOT / "hiddify-core" / "hiddify-sing-box"
    if not sing.is_dir():
        sys.exit(f"Нет каталога {sing} (нужен hiddify-core/hiddify-sing-box для сборки образа).")
    subprocess.run(
        [
            "rsync",
            "-az",
            "--delete",
            "--exclude=.git",
            f"{sing}/",
            f"{target}hiddify-core/hiddify-sing-box/",
        ],
        check=True,
    )
    print("[run] sync: rsync OK", file=sys.stderr)


def _find_git_tar() -> str | None:
    for cand in (
        shutil.which("tar"),
        r"C:\Program Files\Git\usr\bin\tar.exe",
    ):
        if cand and Path(cand).is_file():
            return cand
    return None


def _sync_tar_ssh(cfg: dict) -> None:
    """Потоковая упаковка без rsync (Windows-friendly при наличии tar + ssh)."""
    tar_bin = _find_git_tar()
    if not tar_bin:
        sys.exit("Нет rsync и не найден tar — установите Git for Windows или rsync.")

    r = cfg["remote_root"].rstrip("/")
    u = cfg["user"]
    h = cfg["host"]
    sing = _REPO_ROOT / "hiddify-core" / "hiddify-sing-box"
    if not sing.is_dir():
        sys.exit(f"Нет каталога {sing}.")

    # GNU tar: исключения
    excludes = [
        "--exclude=vendor/s-ui/frontend/node_modules",
        "--exclude=vendor/s-ui/db",
        "--exclude=.git",
    ]
    repo = _REPO_ROOT
    # Запуск из корня репозитория: архивируем vendor/s-ui и hiddify-core/hiddify-sing-box
    cmd_tar = [
        tar_bin,
        "-C",
        repo.as_posix(),
        "-czf",
        "-",
        *excludes,
        "vendor/s-ui",
        "hiddify-core/hiddify-sing-box",
    ]
    # На сервере: распаковать в remote_root (создать дерево)
    remote = (
        f"set -e; mkdir -p {r}; tar xzf - -C {r} "
        f"&& echo '[run] sync: tar|ssh OK'"
    )
    p1 = subprocess.Popen(cmd_tar, stdout=subprocess.PIPE, stderr=sys.stderr)
    assert p1.stdout is not None
    subprocess.run(
        ["ssh", "-o", "BatchMode=yes", f"{u}@{h}", remote],
        stdin=p1.stdout,
        check=True,
    )
    p1.stdout.close()
    rc = p1.wait()
    if rc != 0:
        sys.exit(f"tar завершился с кодом {rc}")


def cmd_sync(cfg: dict) -> None:
    _require_cfg(cfg)
    if _have_rsync():
        _sync_rsync(cfg)
    else:
        print("[run] rsync не найден, используется tar|ssh", file=sys.stderr)
        _sync_tar_ssh(cfg)


def _remote_compose_dir(cfg: dict) -> str:
    return f'{cfg["remote_root"].rstrip("/")}/vendor/s-ui'


def cmd_build_local(cfg: dict) -> None:
    """Сборка образа на локальной машине (кэш BuildKit / go из Dockerfile)."""
    if not _have_docker():
        sys.exit("Нужен Docker (docker compose).")
    c = cfg["compose"]
    print(
        f"[run] build: DOCKER_BUILDKIT=1 docker compose -f {c} build s-ui-local",
        file=sys.stderr,
    )
    subprocess.run(
        ["docker", "compose", "-f", c, "build", "s-ui-local"],
        cwd=_SUI_ROOT,
        env=_docker_subprocess_env(),
        check=True,
    )
    print("[run] build: OK", file=sys.stderr)


def cmd_ship_image(cfg: dict) -> None:
    """Передать образ на VPS потоком (без огромного tar на диске)."""
    _require_cfg(cfg)
    if not _have_docker():
        sys.exit("Нужен Docker.")
    u = cfg["user"]
    h = cfg["host"]
    print(
        f"[run] ship-image: docker save {_STAND_IMAGE} | ssh … docker load",
        file=sys.stderr,
    )
    save = subprocess.Popen(
        ["docker", "save", _STAND_IMAGE],
        stdout=subprocess.PIPE,
        stderr=sys.stderr,
    )
    assert save.stdout is not None
    load = subprocess.Popen(
        ["ssh", "-o", "BatchMode=yes", f"{u}@{h}", "docker", "load"],
        stdin=save.stdout,
        stderr=sys.stderr,
    )
    save.stdout.close()
    rc_load = load.wait()
    rc_save = save.wait()
    if rc_load != 0:
        sys.exit(f"ssh docker load завершился с кодом {rc_load}")
    if rc_save != 0:
        sys.exit(f"docker save завершился с кодом {rc_save}")
    print("[run] ship-image: OK", file=sys.stderr)


def cmd_push_compose(cfg: dict) -> None:
    """Положить compose на VPS и создать каталоги для томов (db, cert)."""
    _require_cfg(cfg)
    d = _remote_compose_dir(cfg)
    c = cfg["compose"]
    comp = _SUI_ROOT / c
    if not comp.is_file():
        sys.exit(f"Нет файла {comp}")
    u = cfg["user"]
    h = cfg["host"]
    _ssh(cfg, f"set -e; mkdir -p {d}/db {d}/cert", check=True)
    dest = f"{u}@{h}:{d}/{c}"
    print(f"[run] push-compose: scp {c} -> {dest}", file=sys.stderr)
    subprocess.run(
        ["scp", "-o", "BatchMode=yes", str(comp), dest],
        check=True,
    )
    print("[run] push-compose: OK", file=sys.stderr)


def _env_bool(name: str, default: bool = True) -> bool:
    v = (os.environ.get(name) or "").strip().lower()
    if not v:
        return default
    return v not in ("0", "false", "no", "off")


def _compose_profile_args_shell() -> str:
    """
    Фрагмент для удалённого shell: `docker compose ...` + `--profile X` для каждого имени из
    SUI_RUN_COMPOSE_PROFILES (через запятую), например `le` для certbot-renew в stand compose.
    """
    raw = (os.environ.get("SUI_RUN_COMPOSE_PROFILES") or "").strip()
    if not raw:
        return ""
    parts: list[str] = []
    for p in raw.split(","):
        p = p.strip()
        if p:
            parts.append(f"--profile {shlex.quote(p)}")
    return " " + " ".join(parts) if parts else ""


def cmd_down(cfg: dict) -> None:
    """Остановить стек на VPS (перед сменой образа с тем же тегом — контейнеры пересоздаются при up)."""
    _require_cfg(cfg)
    d = _remote_compose_dir(cfg)
    c = cfg["compose"]
    prof = _compose_profile_args_shell()
    # mkdir — первый деплой на чистый VPS; down только если есть compose (иначе зелёное поле).
    script = (
        f"set -e; mkdir -p {d}/db {d}/cert; "
        f"cd {d} && if [ -f {shlex.quote(c)} ]; then docker compose -f {shlex.quote(c)}{prof} down --remove-orphans; fi"
    )
    print(f"[run] down: {script}", file=sys.stderr)
    _ssh(cfg, script, check=True)


def cmd_up(cfg: dict) -> None:
    """Поднять стек на VPS из уже загруженного образа (без сборки на сервере)."""
    _require_cfg(cfg)
    d = _remote_compose_dir(cfg)
    c = cfg["compose"]
    prof = _compose_profile_args_shell()
    script = f"set -e; cd {d} && docker compose -f {c}{prof} up -d --no-build"
    print(f"[run] up: {script}", file=sys.stderr)
    _ssh(cfg, script, check=True)


def cmd_deploy(cfg: dict) -> None:
    """Основной деплой: локальная сборка → (down) → загрузка образа → compose → up --no-build → (prune)."""
    cmd_build_local(cfg)
    if _env_bool("SUI_RUN_DEPLOY_DOWN_FIRST", True):
        cmd_down(cfg)
    cmd_ship_image(cfg)
    cmd_push_compose(cfg)
    cmd_up(cfg)
    if _env_bool("SUI_RUN_DEPLOY_IMAGE_PRUNE", True):
        print("[run] image prune: docker image prune -f", file=sys.stderr)
        _ssh(cfg, "docker image prune -f", check=False)


def cmd_remote_build(cfg: dict) -> None:
    """Старая схема: полный sync исходников и сборка go на VPS (долго, без вашего docker-кэша)."""
    cmd_sync(cfg)
    d = _remote_compose_dir(cfg)
    c = cfg["compose"]
    prof = _compose_profile_args_shell()
    script = (
        f"set -e; cd {d} && docker compose -f {c}{prof} build && docker compose -f {c}{prof} up -d"
    )
    print(f"[run] remote-build: {script}", file=sys.stderr)
    _ssh(cfg, script, check=True)


def cmd_certbot(cfg: dict) -> None:
    _require_cfg(cfg)
    dom = cfg["cert_domain"]
    em = cfg["cert_email"]
    if not em:
        sys.exit("Задайте SUI_RUN_CERTBOT_EMAIL для certbot.")
    d = _remote_compose_dir(cfg)
    c = cfg["compose"]
    script = f"""set -e
cd {d}
docker compose -f {c} --profile cert run --rm --service-ports certbot certonly --standalone \\
  -d {dom} --email {em} --agree-tos --non-interactive
"""
    print("[run] certbot: порт 80 на VPS должен быть свободен.", file=sys.stderr)
    _ssh(cfg, script, check=True)


def cmd_binary() -> None:
    """Только замена `sui` в уже запущенном контейнере (без полной пересборки образа)."""
    deploy_py = _SUI_ROOT / "deploy" / "deploy_sui.py"
    if not deploy_py.is_file():
        sys.exit(f"Не найден {deploy_py}")
    r = subprocess.run([sys.executable, str(deploy_py), "binary"])
    raise SystemExit(r.returncode)


def _have_docker() -> bool:
    return shutil.which("docker") is not None


def cmd_redeploy_binary(cfg: dict) -> None:
    """
    Редеплой только бинарника на хосте с Docker: локальный `docker compose build`,
    извлечение `sui` из образа, затем `deploy_sui.py binary` (docker cp в s-ui-local).
    Не пересобирает фронт отдельно — он уже в образе из предыдущего build.
    """
    _require_cfg(cfg)
    if not _have_docker():
        sys.exit("Нужен Docker (docker compose build). Иначе соберите sui вручную и выполните: python run.py binary")
    c = cfg["compose"]
    print(f"[run] redeploy-binary: docker compose -f {c} build s-ui-local", file=sys.stderr)
    subprocess.run(
        ["docker", "compose", "-f", c, "build", "s-ui-local"],
        cwd=_SUI_ROOT,
        env=_docker_subprocess_env(),
        check=True,
    )
    subprocess.run(["docker", "rm", "-f", _EXTRACT_CTR], capture_output=True)
    subprocess.run(
        ["docker", "create", "--name", _EXTRACT_CTR, _STAND_IMAGE],
        check=True,
    )
    out = _SUI_ROOT / "sui"
    try:
        subprocess.run(
            ["docker", "cp", f"{_EXTRACT_CTR}:/app/sui", str(out)],
            check=True,
        )
    finally:
        subprocess.run(["docker", "rm", "-f", _EXTRACT_CTR], capture_output=True)
    print(f"[run] извлечён {out}, загрузка на сервер…", file=sys.stderr)
    cmd_binary()


def cmd_restart() -> None:
    deploy_py = _SUI_ROOT / "deploy" / "deploy_sui.py"
    if not deploy_py.is_file():
        sys.exit(f"Не найден {deploy_py}")
    r = subprocess.run([sys.executable, str(deploy_py), "restart"])
    raise SystemExit(r.returncode)


def _curl_head(url: str, resolve: str) -> tuple[int, str, str]:
    r = subprocess.run(
        [
            "curl",
            "-sSI",
            "--connect-timeout",
            "10",
            "--resolve",
            resolve,
            url,
        ],
        capture_output=True,
        text=True,
    )
    return r.returncode, r.stdout, r.stderr


def cmd_verify(cfg: dict) -> None:
    _require_cfg(cfg)
    dom = cfg["verify_domain"]
    port = cfg["verify_port"]
    h = cfg["host"]
    resolve = f"{dom}:{port}:{h}"

    url_https = f"https://{dom}:{port}/"
    code, out, err = _curl_head(url_https, resolve)
    if code == 0:
        print(out)
        print("[run] verify: OK (HTTPS)", file=sys.stderr)
        return

    print(f"[run] HTTPS недоступен: {err or out}", file=sys.stderr)
    url_http = f"http://{dom}:{port}/"
    code2, out2, err2 = _curl_head(url_http, resolve)
    if code2 == 0:
        print(out2)
        print("[run] verify: OK (HTTP — TLS не поднят или сертификат не выдан)", file=sys.stderr)
        return

    print(err2 or out2, file=sys.stderr)
    sys.exit(
        f"verify failed: ни HTTPS ({url_https}), ни HTTP ({url_http}), resolve={resolve}"
    )


def main() -> None:
    _load_dotenv()
    p = argparse.ArgumentParser(description="s-ui stand: deploy / certbot / verify")
    sub = p.add_subparsers(dest="cmd", required=True)

    sub.add_parser("sync", help="Синхронизировать исходники на VPS (без образа)")
    sub.add_parser(
        "remote-build",
        help="sync + сборка образа на VPS (медленно; только если нужен билд на сервере)",
    )
    sub.add_parser("build", help="Локально: docker compose build s-ui-local (кэш Docker/Go)")
    sub.add_parser("ship-image", help="Локально собранный образ -> ssh docker load")
    sub.add_parser("push-compose", help="scp docker-compose.stand.yml на VPS + mkdir db/cert")
    sub.add_parser("down", help="На VPS: docker compose down --remove-orphans")
    sub.add_parser("up", help="На VPS: docker compose up -d --no-build")
    sub.add_parser(
        "deploy",
        help="build (локально) -> ship-image -> push-compose -> up --no-build",
    )
    sub.add_parser("certbot", help="На VPS: certonly --standalone (профиль cert)")
    sub.add_parser("verify", help="curl -I HTTPS (с --resolve на SUI_RUN_HOST)")
    ap = sub.add_parser("all", help="deploy + verify")
    sub.add_parser(
        "binary",
        help="Только загрузить локальный sui в контейнер (нужен deploy/deploy.env с DOCKER_* )",
    )
    sub.add_parser(
        "redeploy-binary",
        help="docker compose build -> извлечь sui из образа -> deploy_sui binary (редеплой бинарника)",
    )
    sub.add_parser("restart", help="Перезапуск через deploy_sui.py restart")

    args = p.parse_args()
    cfg = _cfg()

    if args.cmd == "binary":
        cmd_binary()
    elif args.cmd == "redeploy-binary":
        cmd_redeploy_binary(cfg)
    elif args.cmd == "restart":
        cmd_restart()
    elif args.cmd == "sync":
        cmd_sync(cfg)
    elif args.cmd == "remote-build":
        cmd_remote_build(cfg)
    elif args.cmd == "build":
        cmd_build_local(cfg)
    elif args.cmd == "ship-image":
        cmd_ship_image(cfg)
    elif args.cmd == "push-compose":
        cmd_push_compose(cfg)
    elif args.cmd == "down":
        cmd_down(cfg)
    elif args.cmd == "up":
        cmd_up(cfg)
    elif args.cmd == "deploy":
        cmd_deploy(cfg)
    elif args.cmd == "certbot":
        cmd_certbot(cfg)
    elif args.cmd == "verify":
        cmd_verify(cfg)
    elif args.cmd == "all":
        cmd_deploy(cfg)
        cmd_verify(cfg)
    else:
        p.error("unknown command")


if __name__ == "__main__":
    main()
