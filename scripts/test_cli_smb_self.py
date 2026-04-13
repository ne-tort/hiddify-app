#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
SMB к своему VPN-IP (10.8.0.x) через поднятый HiddifyCli: не только TCP 445,
а сессия и просмотр шар / чтение каталога.

Учётные данные не хранятся в коде: задайте переменные окружения перед запуском:
  HIDDIFY_SMB_USER
  HIDDIFY_SMB_PASSWORD

Пример (PowerShell):
  $env:HIDDIFY_SMB_USER='qwerty'
  $env:HIDDIFY_SMB_PASSWORD='...'
  python scripts/test_cli_smb_self.py --cli ... --workdir ... --config ...

На Windows: smbprotocol (прямой listdir по шарам) и net use \\\\<vpn-ip>\\IPC$ + net view.

Перед полной проверкой (--full-smb) на этой же машине должен существовать локальный
пользователь с указанным логином/паролем (или доменные креды), и должен быть включён
служба «Сервер» / общий доступ к файлам. Пример создания локальной учётки (админ. cmd):
  net user ИМЯ ПАРОЛЬ /add

Ошибки 1326 или STATUS_LOGON_FAILURE означают неверные креды или отсутствие учётной записи,
а не обязательно проблему VPN.

Скрипт перебирает формы логина: логин без префикса, .\\логин, ИМЯ_ПК\\логин, WORKGROUP\\логин
(имя рабочей группы с машины), USERDOMAIN\\логин (для ПК в домене — NetBIOS-имя домена),
localhost\\логин. Обычно не нужны «localhost» как домен для AD — для AD используйте
NetBIOS-домен (короткое имя), не DNS-суффикс.

Зависимость (опционально, для listdir): pip install smbprotocol

"""

from __future__ import annotations

import argparse
import json
import os
import re
import socket
import subprocess
import sys
import time
import urllib.request

try:
    from smbclient import listdir as smb_listdir
    from smbclient import register_session as smb_register_session
    from smbprotocol.exceptions import SMBException

    _HAVE_SMBPROTOCOL = True
except ImportError:
    _HAVE_SMBPROTOCOL = False
    SMBException = OSError  # type: ignore[misc,assignment]

def _report_tun_mtu(config_path: str) -> None:
    """Информативно: задан ли mtu у tun-in в JSON (если нет — дефолты sing-box)."""
    try:
        with open(config_path, encoding="utf-8") as f:
            cfg = json.load(f)
        for ib in cfg.get("inbounds", []):
            if ib.get("type") == "tun" and ib.get("tag") == "tun-in":
                m = ib.get("mtu")
                if m is None:
                    print("tun-in: mtu в JSON не задан (дефолты sing-box / ОС)")
                else:
                    print(f"tun-in: mtu в JSON = {m}")
                return
        print(f"inbound tun-in не найден в {config_path}", file=sys.stderr)
    except (OSError, json.JSONDecodeError) as e:
        print(f"Не удалось прочитать конфиг: {e}", file=sys.stderr)


def wait_tcp(host: str, port: int, timeout: float) -> bool:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        s.settimeout(1.0)
        try:
            if s.connect_ex((host, port)) == 0:
                return True
        finally:
            s.close()
        time.sleep(0.25)
    return False


def _net_run(argv: list[str]) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        argv,
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
        shell=False,
    )


def _windows_workgroup_name() -> str:
    """Имя рабочей группы (workgroup), не путать с доменом AD — через wmic."""
    if sys.platform != "win32":
        return ""
    try:
        r = subprocess.run(
            ["wmic", "computersystem", "get", "Workgroup"],
            capture_output=True,
            text=True,
            encoding="utf-8",
            errors="replace",
            timeout=15,
            shell=False,
        )
        lines = [x.strip() for x in (r.stdout or "").splitlines() if x.strip()]
        for i, line in enumerate(lines):
            if line.lower() == "workgroup" and i + 1 < len(lines):
                return lines[i + 1]
        if len(lines) >= 1:
            return lines[-1]
    except OSError:
        pass
    return ""


def smb_username_variants(login: str) -> list[str]:
    """
    Варианты имени для SMB на Windows: локальная учётка и рабочая группа, не FQDN.

    Типично работают: .\\user, ИМЯ_ПК\\user, WORKGROUP\\user (имя группы с этой машины).
    В AD: USERDOMAIN\\user (NetBIOS-имя домена из окружения).
    «localhost\\user» иногда срабатывает при доступе к себе по IP — в конец списка.
    """
    login = login.strip()
    out: list[str] = []
    seen: set[str] = set()

    def add(s: str) -> None:
        s = s.strip()
        if s and s not in seen:
            seen.add(s)
            out.append(s)

    computer = (os.environ.get("COMPUTERNAME") or "").strip()
    userdomain = (os.environ.get("USERDOMAIN") or "").strip()
    wg = _windows_workgroup_name()

    add(login)
    add(rf".\{login}")
    if computer:
        add(rf"{computer}\{login}")
    if wg:
        add(rf"{wg}\{login}")
    # В домене USERDOMAIN — NetBIOS-имя домена; на ПК без домена часто совпадает с именем ПК.
    if userdomain:
        add(rf"{userdomain}\{login}")
    add(rf"localhost\{login}")
    return out


def smb_net_use_ipc(
    vpn_ip: str, user: str, password: str
) -> tuple[bool, str, str | None]:
    """
    Подключить IPC$ к \\vpn_ip. Перебираем типичные формы локальной учётки Windows.
    Возвращает (успех, лог, remote_path для net use /delete или None).
    """
    unc = rf"\\{vpn_ip}\IPC$"
    last_err = ""
    for u in smb_username_variants(user):
        r = _net_run(["net", "use", unc, password, f"/user:{u}"])
        log = (r.stdout or "") + (r.stderr or "")
        if r.returncode == 0 or "успешно" in log.lower() or "successfully" in log.lower():
            return True, log, unc
        last_err = log
    return False, last_err, None


def smb_net_view(vpn_ip: str) -> tuple[int, str]:
    r = _net_run(["net", "view", rf"\\{vpn_ip}"])
    return r.returncode, (r.stdout or "") + (r.stderr or "")


def parse_net_view_disk_shares(text: str) -> list[str]:
    """Вытащить имена дисковых шар из вывода net view (англ/рус локаль)."""
    shares: list[str] = []
    for line in text.splitlines():
        line = line.strip()
        if not line or line.startswith("---") or "Share name" in line or "Имя" in line:
            continue
        parts = re.split(r"\s{2,}", line)
        if len(parts) < 2:
            parts = line.split(None, 2)
        if len(parts) < 2:
            continue
        name = parts[0].strip()
        kind = parts[1].strip().lower()
        if kind in ("disk", "диск") and name.upper() not in ("IPC$", "PRINT$"):
            shares.append(name)
    out: list[str] = []
    for s in shares:
        if s not in out:
            out.append(s)
    return out


def smb_try_listdir_direct(
    vpn_ip: str, user: str, password: str, shares: list[str]
) -> tuple[bool, str]:
    """Прямая сессия smbprotocol без net use (иногда net выдаёт 1326, а SMB всё равно пускает)."""
    if not _HAVE_SMBPROTOCOL:
        return False, ""
    last_err = ""
    for uname in smb_username_variants(user):
        for share in shares:
            base = rf"\\{vpn_ip}\{share}"
            try:
                smb_register_session(vpn_ip, username=uname, password=password)
                names = smb_listdir(base)
                preview = ", ".join(names[:15])
                more = f" … (+{len(names) - 15})" if len(names) > 15 else ""
                return True, f"smbprotocol OK {base} as {uname}: [{preview}{more}]"
            except (OSError, SMBException) as e:
                last_err = f"{base} as {uname}: {e}"
    return False, last_err


def smb_listdir_smbprotocol(vpn_ip: str, user: str, password: str, share: str) -> str:
    if not _HAVE_SMBPROTOCOL:
        return ""
    base = rf"\\{vpn_ip}\{share}"
    last_err = ""
    for uname in smb_username_variants(user):
        try:
            smb_register_session(
                vpn_ip,
                username=uname,
                password=password,
            )
            names = smb_listdir(base)
            preview = ", ".join(names[:15])
            more = f" … (+{len(names) - 15})" if len(names) > 15 else ""
            return f"smbprotocol listdir {base} as {uname}: [{preview}{more}]"
        except (OSError, SMBException) as e:
            last_err = f"{base} as {uname}: {e}"
    return last_err


def main() -> int:
    try:
        sys.stdout.reconfigure(encoding="utf-8")  # type: ignore[attr-defined]
        sys.stderr.reconfigure(encoding="utf-8")  # type: ignore[attr-defined]
    except AttributeError:
        pass

    ap = argparse.ArgumentParser()
    ap.add_argument("--cli", required=True)
    ap.add_argument("--workdir", required=True)
    ap.add_argument("--config", required=True)
    ap.add_argument("--vpn-ip", default="10.8.0.3")
    ap.add_argument("--smb-port", type=int, default=445)
    ap.add_argument("--wait-mixed", type=float, default=120.0)
    ap.add_argument(
        "--smb-user",
        default=os.environ.get("HIDDIFY_SMB_USER", ""),
        help="Или env HIDDIFY_SMB_USER",
    )
    ap.add_argument(
        "--require-smb-auth",
        action="store_true",
        help="Требовать успешный net view / listdir, а не только TCP 445",
    )
    ap.add_argument(
        "--full-smb",
        action="store_true",
        help="Полная проверка: net use + net view (+ smbprotocol listdir при наличии пакета). Включает --require-smb-auth.",
    )
    args = ap.parse_args()

    password = os.environ.get("HIDDIFY_SMB_PASSWORD", "")
    if args.full_smb:
        args.require_smb_auth = True
    if args.require_smb_auth and (not args.smb_user or not password):
        print(
            "Задайте HIDDIFY_SMB_USER и HIDDIFY_SMB_PASSWORD (опция --smb-user при необходимости).",
            file=sys.stderr,
        )
        return 2

    _report_tun_mtu(os.path.abspath(args.config))

    workdir = os.path.abspath(args.workdir)
    os.makedirs(os.path.join(workdir, "data"), exist_ok=True)

    try:
        ip_direct = urllib.request.urlopen("https://api.ipify.org", timeout=15).read().decode().strip()
    except OSError:
        ip_direct = ""
    print("IP до CLI (без туннеля):", ip_direct or "(недоступно)")

    cli = os.path.abspath(args.cli)
    cfg = os.path.abspath(args.config)
    cmd = [cli, "-D", workdir, "srun", "-c", cfg]

    creationflags = 0
    if sys.platform == "win32":
        creationflags = subprocess.CREATE_NEW_PROCESS_GROUP  # type: ignore[attr-defined]

    proc = subprocess.Popen(
        cmd,
        cwd=os.path.dirname(cli) or ".",
        creationflags=creationflags,
    )
    try:
        if not wait_tcp("127.0.0.1", 12334, args.wait_mixed):
            print("mixed 12334 не поднялся", file=sys.stderr)
            return 2

        vpn = args.vpn_ip
        addr = (vpn, args.smb_port)
        sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        sock.settimeout(10.0)
        try:
            sock.connect(addr)
        except OSError as e:
            print(f"TCP {vpn}:{args.smb_port}: {e}", file=sys.stderr)
            return 3
        finally:
            sock.close()
        print(f"TCP {vpn}:{args.smb_port}: OK")

        smb_auth_ok = False
        smb_listdir_ok = False
        net_view_out = ""
        ipc_unc: str | None = None

        if args.smb_user and password and sys.platform == "win32":
            if args.require_smb_auth and _HAVE_SMBPROTOCOL:
                ok_dir, msg_dir = smb_try_listdir_direct(
                    vpn,
                    args.smb_user,
                    password,
                    ["Users", "C$", "PUBLIC", "Public"],
                )
                print("smbprotocol (без net use):", msg_dir[:1200] if msg_dir else "")
                if ok_dir:
                    smb_listdir_ok = True

            ok_ipc, ipc_log, ipc_unc = smb_net_use_ipc(vpn, args.smb_user, password)
            print("net use IPC$:", ipc_log.strip()[:800])
            try:
                if ok_ipc:
                    code, net_view_out = smb_net_view(vpn)
                    print("net view exit:", code)
                    print(net_view_out[:4000])
                    smb_auth_ok = code == 0

                    if smb_auth_ok and _HAVE_SMBPROTOCOL:
                        shares = parse_net_view_disk_shares(net_view_out)
                        print("disk shares (parsed):", shares)
                        for sh in shares[:5]:
                            msg = smb_listdir_smbprotocol(
                                vpn, args.smb_user, password, sh
                            )
                            print(msg)
                            if msg and "listdir" in msg and "[" in msg:
                                smb_listdir_ok = True
                                break
                else:
                    print(
                        "Не удалось подключить IPC$ с указанными учётными данными.",
                        file=sys.stderr,
                    )
            finally:
                if ipc_unc:
                    _net_run(["net", "use", ipc_unc, "/delete", "/y"])
        elif args.require_smb_auth:
            print("Нужны учётные данные для SMB (Windows).", file=sys.stderr)
            return 2

        try:
            via = urllib.request.build_opener(
                urllib.request.ProxyHandler(
                    {"http": "http://127.0.0.1:12334", "https": "http://127.0.0.1:12334"}
                )
            ).open("https://api.ipify.org", timeout=30)
            proxy_ip = via.read().decode().strip()
        except OSError as e:
            print("ipify через mixed:", e, file=sys.stderr)
            proxy_ip = ""

        print("ipify через mixed:", proxy_ip)

        if args.require_smb_auth:
            if smb_auth_ok or smb_listdir_ok:
                print(
                    "OK: SMB к \\{} по VPN-IP: net view={} listdir={}.".format(
                        vpn, smb_auth_ok, smb_listdir_ok
                    )
                )
                return 0
            print("FAIL: не прошли SMB (net view и listdir).", file=sys.stderr)
            return 4

        if smb_auth_ok or smb_listdir_ok:
            print("OK: SMB auth / listdir.")
            return 0
        if ip_direct and proxy_ip and ip_direct != proxy_ip:
            print("OK: только egress через mixed (без полной SMB-проверки).")
            return 0
        print("Частичный успех: TCP 445 OK; для шар задайте креды и --require-smb-auth.", file=sys.stderr)
        return 0
    finally:
        if proc.poll() is None:
            proc.terminate()
            try:
                proc.wait(timeout=20)
            except subprocess.TimeoutExpired:
                proc.kill()
                proc.wait(timeout=10)


if __name__ == "__main__":
    raise SystemExit(main())
