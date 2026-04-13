#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Запуск portable HiddifyCli (srun) с конфигом sing-box и проверка,
что исходящий IP при запросе через локальный mixed-inbound (127.0.0.1:12334)
отличается от прямого запроса без прокси (трафик через sing-box, не «напрямую»).

Пример:
  python scripts/test_portable_cli_vpn.py ^
    --cli "D:\\...\\portable\\windows-x64\\Hiddify\\HiddifyCli.exe" ^
    --workdir "D:\\tmp\\hiddify_cli_work" ^
    --config "D:\\path\\config.json"

Из лога приложения (строки HiddifyCoreService: ...):
  python scripts/test_portable_cli_vpn.py --cli ... --workdir ... --from-log core_log.txt
"""

from __future__ import annotations

import argparse
from typing import Optional
import json
import os
import re
import signal
import socket
import subprocess
import sys
import time
import urllib.error
import urllib.request

LOG_PREFIX_RE = re.compile(
    r"^\d{2}:\d{2}:\d{2}\.\d+\s+-\s+\[I\]\s+HiddifyCoreService:\s*(.*)$"
)


def extract_json_from_log(text: str) -> dict:
    lines = []
    for raw in text.splitlines():
        m = LOG_PREFIX_RE.match(raw.rstrip("\r"))
        if m:
            lines.append(m.group(1))
    body = "\n".join(lines).strip()
    return json.loads(body)


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


def http_get(url: str, proxy_url: Optional[str], timeout: float) -> str:
    handlers = []
    if proxy_url:
        handlers.append(
            urllib.request.ProxyHandler(
                {
                    "http": proxy_url,
                    "https": proxy_url,
                }
            )
        )
    opener = urllib.request.build_opener(*handlers)
    req = urllib.request.Request(
        url,
        headers={"User-Agent": "hiddify-portable-cli-test/1"},
    )
    with opener.open(req, timeout=timeout) as resp:
        return resp.read().decode("utf-8", errors="replace").strip()


def main() -> int:
    try:
        sys.stdout.reconfigure(encoding="utf-8")  # type: ignore[attr-defined]
        sys.stderr.reconfigure(encoding="utf-8")  # type: ignore[attr-defined]
    except AttributeError:
        pass

    ap = argparse.ArgumentParser()
    ap.add_argument("--cli", required=True, help="Путь к HiddifyCli.exe")
    ap.add_argument(
        "--workdir",
        required=True,
        help="Рабочий каталог для -D (создаётся data/, clash.db и т.д.)",
    )
    g = ap.add_mutually_exclusive_group(required=True)
    g.add_argument("--config", help="Файл JSON sing-box")
    g.add_argument(
        "--from-log",
        dest="from_log",
        metavar="FILE",
        help="Лог с префиксом HiddifyCoreService: (JSON собирается построчно)",
    )
    ap.add_argument("--proxy-host", default="127.0.0.1")
    ap.add_argument("--proxy-port", type=int, default=12334)
    ap.add_argument("--ip-check-url", default="https://api.ipify.org")
    ap.add_argument("--wait-port", type=float, default=120.0)
    ap.add_argument(
        "--dry-run",
        action="store_true",
        help="Только распарсить конфиг / извлечь из лога, без запуска CLI",
    )
    args = ap.parse_args()

    cli = os.path.abspath(args.cli)
    workdir = os.path.abspath(args.workdir)
    os.makedirs(workdir, exist_ok=True)

    if args.from_log:
        with open(args.from_log, encoding="utf-8") as f:
            text = f.read()
        cfg = extract_json_from_log(text)
        config_path = os.path.join(workdir, "_extracted_singbox.json")
        with open(config_path, "w", encoding="utf-8") as wf:
            json.dump(cfg, wf, ensure_ascii=False, indent=2)
        print("Конфиг из лога записан:", config_path)
    else:
        config_path = os.path.abspath(args.config)

    if args.dry_run:
        with open(config_path, encoding="utf-8") as f:
            json.load(f)
        print("dry-run: JSON валиден:", config_path)
        return 0

    if not os.path.isfile(cli):
        print("Не найден HiddifyCli:", cli, file=sys.stderr)
        return 2

    # До поднятия TUN: иначе при auto_route «прямой» urllib тоже уйдёт в туннель и IP совпадёт с VPN.
    try:
        ip_before_vpn = http_get(args.ip_check_url, None, 25.0)
    except urllib.error.URLError as e:
        print("Нет доступа к ip-check до запуска CLI:", e, file=sys.stderr)
        return 5
    print("IP до запуска VPN (реальный выход без туннеля):", ip_before_vpn)

    os.makedirs(os.path.join(workdir, "data"), exist_ok=True)

    cmd = [cli, "-D", workdir, "srun", "-c", config_path]
    print("Команда:", " ".join(cmd))

    creationflags = 0
    if sys.platform == "win32":
        creationflags = subprocess.CREATE_NEW_PROCESS_GROUP  # type: ignore[attr-defined]

    cwd = os.path.dirname(cli)
    proc = subprocess.Popen(
        cmd,
        cwd=cwd or ".",
        creationflags=creationflags,
    )
    proxy_url = f"http://{args.proxy_host}:{args.proxy_port}"
    try:
        if not wait_tcp(args.proxy_host, args.proxy_port, args.wait_port):
            print(
                "Таймаут: mixed inbound не слушает",
                f"{args.proxy_host}:{args.proxy_port}",
                file=sys.stderr,
            )
            print(
                "Проверьте конфиг (порт), права (TUN на Windows), лог в workdir/data/box.log.",
                file=sys.stderr,
            )
            return 3

        via_proxy_ip = http_get(args.ip_check_url, proxy_url, 35.0)

        print(f"Через mixed {proxy_url} (sing-box → VPN):", via_proxy_ip)

        if via_proxy_ip == ip_before_vpn:
            print(
                "ОШИБКА: IP через mixed совпадает с IP до VPN — туннель не сменил egress для проверки.",
                file=sys.stderr,
            )
            return 4

        print(
            "OK: IP через mixed отличается от IP до запуска CLI — проверка шла через ядро/VPN."
        )
        return 0
    except urllib.error.URLError as e:
        print("Сетевая ошибка при проверке IP:", e, file=sys.stderr)
        return 5
    finally:
        if proc.poll() is None:
            proc.terminate()
            try:
                proc.wait(timeout=20)
            except subprocess.TimeoutExpired:
                proc.kill()
                proc.wait(timeout=10)
        elif proc.returncode not in (0, -signal.SIGTERM, -15):
            if sys.platform == "win32":
                print("CLI завершился с кодом:", proc.returncode, file=sys.stderr)


if __name__ == "__main__":
    raise SystemExit(main())
