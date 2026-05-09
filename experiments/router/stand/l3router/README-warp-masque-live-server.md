# WARP MASQUE stand на VPS (без захвата всего интернета)

Цель: поднять тот же Docker-стенд на Linux-сервере, где **до Cloudflare API/QUIC есть путь**, не ломая **default route** и обычный SSH. Таймауты smoke/`connect_timeout`: см. **`AGENTS.md`** в корне `hiddify-app` — без больших значений и «вечных» проверок.

## Почему такой TUN

См. официально: [`inbound/tun`](https://sing-box.sagernet.org/configuration/inbound/tun/) — **`route_address`** при **`auto_route: true`** задаёт **свои** префиксы вместо маршрута «весь интернет в tun». На Linux с Docker рекомендован **`auto_redirect`**, чтобы уменьшить конфликты с bridge и улучшить прозрачную маршрутизацию.

Два шаблона:

- **[configs/warp-masque-live.server.docker.json](configs/warp-masque-live.server.docker.json)** — для **Docker Compose** с **`network_mode: host`** ([docker-compose.warp-masque-live.server.yml](docker-compose.warp-masque-live.server.yml)): **`auto_route: false`**, `tun`+SOCKS. Так уходим от NAT bridge, из‑за которого часто обрывается **QUIC/HTTP3** к WARP (`open_http3_client_conn`, `no recent network activity`). На **bridge‑netns** без host‑network та же картина плюс риск **`add route … invalid argument`** при **`auto_route` + маршрутизации**.
- **[configs/warp-masque-live.server.json](configs/warp-masque-live.server.json)** — эталон **узких маршрутов** (`route_address`), **`auto_route` + `auto_redirect`**, **`mtu: 9000`** — для **`sing-box run` на самом VPS** или того же контейнера с **`network_mode: host`**.

Скрипт [scripts/init_warp_masque_server_local.sh](scripts/init_warp_masque_server_local.sh) создаёт `*.server.local.json` из **docker**‑шаблона.

В **`warp-masque-live.server.json`** (режим узких маршрутов):

- Адрес **`172.19.100.2/30`** на `tun` — только локальный интерфейс.
- **`mtu: 9000`** — виртуальный туннель; проверки через реальный QUIC/WARP упираются в PMTU пути до edge.
- **`route_address`**: **`198.18.0.0/15`** (benchmark), **`1.1.1.0/24`**, **`1.0.0.0/24`** — только эти назначения уходят в таблицы маршрутов через `tun` при **`auto_route: true`** (не «весь интернет»).

**SOCKS `0.0.0.0:1080`** — явный proxy; в Docker‑режиме им и пользуйтесь до появления рабочего `auto_route` в netns.

Smoke **без** SOCKS (прозрачно только если включены маршруты через `tun`, см. `warp-masque-live.server.json` / хост‑режим):

```bash
docker exec sing-box-warp-masque-live-server curl -sS --max-time 20 https://1.1.1.1/cdn-cgi/trace
```

Не вешайте **`default via`** на tun на **хосте**. `network_mode: host` открывает сервисы контейнера на сетевом стеке **хоста** (в т.ч. SOCKS на `:1080`); закрывайте firewall и не публикуйте лишнее наружу.

## Перенос на сервер

1. На машине с репозиторием (из **`experiments/router/stand/l3router`**).

   Архив **без** бинаря (меньше размер; sing-box собирать уже на VPS — см. шаг 3):

   ```bash
   bash scripts/pack_warp_masque_server_bundle.sh ./warp-masque-stand-bundle.tgz
   ```

   Архив **с** `artifacts/sing-box-linux-amd64` (удобно для полного `scp` с Windows/Linux):

   ```bash
   # из каталога l3router, после сборки ELF в artifacts/
   WITH_SINGBOX=1 bash scripts/pack_warp_masque_server_bundle.sh ../warp-masque-stand-bundle-with-binary.tgz
   scp ../warp-masque-stand-bundle-with-binary.tgz USER@YOUR_SERVER:~/
   ```

   **PowerShell / Windows** (`scp` входит в Git for Windows):

   ```powershell
   cd experiments\router\stand\l3router
   # предварительно: GOOS=linux GOARCH=amd64 CGO_ENABLED=0 из hiddify-core\hiddify-sing-box\ → artifacts\sing-box-linux-amd64
   .\scripts\upload_warp_masque_stand.ps1 -Target USER@YOUR_SERVER -RemotePath ~/warp-masque-stand-bundle-with-binary.tgz
   ```

   **`sing-box check`** для `warp-masque-live.server.json`: запускать **в Linux** (контейнер или VPS): на Windows ошибка **`initialize auto-redirect: invalid argument`** из‑за `auto_redirect` / nftables ожидаема и не означает, что конфиг для Docker невалиден.

   Первым аргументом можно указать любой путь к `.tgz`. Если аргумента нет, файл создаётся как **`../warp-masque-stand-bundle.tgz`** относительно `l3router`.

2. На сервере (в архиве — **содержимое `l3router`** в корне тарбола):

   ```bash
   mkdir -p ~/warp-masque-stand && tar xzf ~/warp-masque-stand-bundle.tgz -C ~/warp-masque-stand
   # или при загрузке with-binary архива замените имя файла на warp-masque-stand-bundle-with-binary.tgz
   cd ~/warp-masque-stand
   bash scripts/init_warp_masque_server_local.sh
   # Отредактируйте configs/warp-masque-live.server.local.json (profile); при нужде см. основной README про кэш edge и WARP_MASQUE_WARP_CACHE
   ```

3. **Пропускается**, если архив уже содержит **`artifacts/sing-box-linux-amd64`**. Иначе соберите sing-box (**из `hiddify-core/hiddify-sing-box`** на VPS) → `~/warp-masque-stand/artifacts/sing-box-linux-amd64`:

   ```bash
   mkdir -p ~/warp-masque-stand/artifacts
   cd /path/to/hiddify-core/hiddify-sing-box
   GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
     -tags "with_gvisor,with_clash_api,with_utls,with_l3router,with_masque" \
     -o ~/warp-masque-stand/artifacts/sing-box-linux-amd64 ./cmd/sing-box
   ```

4. Запуск на сервере:

   ```bash
   cd ~/warp-masque-stand
   docker compose -f docker-compose.warp-masque-live.server.yml build
   docker compose -f docker-compose.warp-masque-live.server.yml up -d
   ```

5. Smoke (SOCKS только **внутри контейнера** — весь интернет VPS не перевешивается на туннель):

```bash
docker exec sing-box-warp-masque-live-server curl -sS --max-time 20 \
  --proxy socks5h://127.0.0.1:1080 https://1.1.1.1/cdn-cgi/trace
```

При **`network_mode: host`** SOCKS слушает **на хосте** (`0.0.0.0:1080`). Ограничьте доступ **firewall** (или слушайте только `127.0.0.1`, если добавите опцию в inbound в JSON). На ПК: `ssh -N -L 1080:127.0.0.1:1080 user@YOUR_SERVER` и `curl --socks5-hostname 127.0.0.1:1080 https://example.com`.

Для режима **bridge** + `ports:` см. исторические варианты; для WARP **QUIC** bridge часто даёт таймауты — предпочтителен host‑network.

## Частые ошибки bootstrap

- **`open_http3_client_conn` / `warp_masque startup failed: timeout: no recent network activity`** при рабочем TCP к API: **Docker bridge UDP** → **`network_mode: host`** в [docker-compose.warp-masque-live.server.yml](docker-compose.warp-masque-live.server.yml). В compose включено **`QUIC_GO_DISABLE_GSO` / `QUIC_GO_DISABLE_ECN`**. DNS-стратегию для MASQUE задавайте через штатные поля sing-box (`dns.strategy`, `dns.rules`, `domain_resolver`) — без хардкода IPv4 в transport-слое, чтобы резолв проходил по общей политике sing-box. Не подставляйте **`server` как голый IP** в endpoint без порта/SNI из профиля — легко получить **`tls: handshake failure`**. Если в кэше или профиле виден **WG-стиль порта**, а QUIC к MASQUE ожидается иначе, задайте в JSON **`profile.dataplane_port`** (часто **443**) — см. [`docs/masque/MASQUE-SINGBOX-CONFIG.md`](../../../../docs/masque/MASQUE-SINGBOX-CONFIG.md) §7.2.1.
- После ошибки **`runtime start`** в stdout/stderr ищите **`class=<ключ>`** — это [`ClassifyMasqueFailure`](../../../../hiddify-core/hiddify-sing-box/protocol/masque/errors_classify.go) (Extended CONNECT vs datagrams vs TLS vs 401 и т.д.).
- **`quic_experimental.enabled requires MASQUE_EXPERIMENTAL_QUIC=1`** — если снова включите `quic_experimental` в JSON, задайте в compose `environment: { MASQUE_EXPERIMENTAL_QUIC: "1" }`, иначе процесс завершится при старте.

## Переменные Compose

| Переменная | Значение |
|------------|---------|
| `WARP_MASQUE_CONFIG` | Путь к JSON (по умолчанию `./configs/warp-masque-live.server.local.json`) |
| `WARP_MASQUE_WARP_CACHE` | Кэш edge; по умолчанию пустой [configs/warp-masque-warp-cache.empty.json](configs/warp-masque-warp-cache.empty.json); при офлайн API можно смонтировать свой JSON с `server`/`port`. Порт QUIC при необходимости дополнительно корректируется **`profile.dataplane_port`** в основном конфиге (не ключ кэша). |

Файлы `*-server.local.json` и заполненный кэш с секретами **не коммитить**.

## Файлы по умолчанию compose

Общий [docker-compose.warp-masque-live.yml](docker-compose.warp-masque-live.yml): кэш WARP монтируется из **`configs/warp-masque-warp-cache.empty.json`**, пока явно не задан `WARP_MASQUE_WARP_CACHE` на ваш файл с данными для edge.
