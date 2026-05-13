# WARP MASQUE stand на VPS (без захвата всего интернета)

**Handoff для ИИ (команды go test, сборка ELF, scp, compose, TUN-smoke, grep по логам):** корневой [`AGENTS.md`](../../../../AGENTS.md) в репозитории `hiddify-app`.

Цель: поднять тот же Docker-стенд на Linux-сервере, где **до Cloudflare API/QUIC есть путь**, не ломая **default route** и обычный SSH. Таймауты smoke/`connect_timeout`: см. **`AGENTS.md`** в корне `hiddify-app` — без больших значений и «вечных» проверок.

## Почему такой TUN

См. [`inbound/tun`](https://sing-box.sagernet.org/configuration/inbound/tun/). На Linux при **`auto_route: true`** и **пустом** `route_address` sing-box может отправить в ядро **`0.0.0.0/0`** через TUN — на VPS это опасно. Поэтому в шаблонах стенда **`route_address`** задан явно (`198.18.0.0/15`, `1.1.1.0/24`, `1.0.0.0/24`): в таблицу попадают **только** эти сети, не полный default route. В docker-шаблоне **`auto_route`** может быть **`true`** (удобнее для смока) или **`false`** (если на вашем хосте `auto_route` ломает поднятие TUN при `network_mode: host`) — в обоих случаях **`route_address`** обязателен.

Два шаблона:

- **[configs/warp-masque-live.server.docker.json](configs/warp-masque-live.server.docker.json)** — **Docker Compose**, **`network_mode: host`**: **`route_address`**: `198.18.0.0/15`, `1.1.1.0/24`, `1.0.0.0/24`, `tun` + (опционально) SOCKS **`127.0.0.1:1080`**, endpoint **`http_layer: h3`**, **`http_layer_fallback: true`**. В шаблоне может быть **`auto_route: true`** или **`false`** — см. раздел про smoke ниже. **H2:** [warp-masque-live.server.docker.h2.json](configs/warp-masque-live.server.docker.h2.json).
- **[configs/warp-masque-live.server.json](configs/warp-masque-live.server.json)** — **узкие префиксы** в **`route_address`**, **`auto_route` + `auto_redirect`**, **`mtu: 9000`** — для запуска **sing-box на хосте**, без привязки к docker-шаблону выше.

Скрипт [scripts/init_warp_masque_server_local.sh](scripts/init_warp_masque_server_local.sh) создаёт `*.server.local.json` из **docker**‑шаблона.

В **`warp-masque-live.server.json`** (режим узких маршрутов):

- Адрес **`172.19.100.2/31`** на `tun` — точка-точка (два хоста в префиксе); для **`mixed`/`system`** одного **`/32`** мало (**`need one more IPv4 address in first prefix`**). **`/30`** с адресом **`.2/30`** на Linux даёт nexthop **`.3`**, с которым **`ip route … via … dev tun`** часто получает **invalid gateway** / **`add route 0: invalid argument`** — поэтому на стенде **`/31`**.
- **`mtu: 9000`** — виртуальный туннель; проверки через реальный QUIC/WARP упираются в PMTU пути до edge.
- **`route_address`**: **`198.18.0.0/15`** (benchmark), **`1.1.1.0/24`**, **`1.0.0.0/24`** — только эти назначения в маршрутах через `tun` (в docker-шаблоне при **`auto_route: false`**; в **`warp-masque-live.server.json`** — вместе с **`auto_route: true`** и правилами). Никогда не включайте **`auto_route: true`** без явного **`route_address`** на Linux-хосте.

**Smoke и проверки агентов — только через TUN**, без SOCKS: исходящий трафик к `1.1.1.1` должен идти через `tun-warp-test` и outbound `warp_masque`. SOCKS в конфиге — вспомогательный inbound для людей; **не использовать его как основной smoke** (и не гонять автоматические тесты через `--proxy socks5h`). Из-за узкого **`route_address`** проверяйте **литералом** (`https://1.1.1.1/…`, `https://1.0.0.1/…` или адрес из `198.18.0.0/15`): иначе DNS через **`detour: direct`** может вернуть IP **вне** захваченных префиксов, и трафик **не попадёт** в TUN при том же конфиге.

Перед `curl` убедитесь, что маршрут к `1.1.1.1` уходит в tun (при **`auto_route: true`** и заполненном **`route_address`**):

```bash
docker exec sing-box-warp-masque-live-server ip route get 1.1.1.1
docker exec sing-box-warp-masque-live-server curl -sS --max-time 25 https://1.1.1.1/cdn-cgi/trace
```

В выводе trace ожидается строка **`warp=on`** (остальные поля не публиковать без необходимости).

### Матрица H2/H3 и параллельные конфиги

- Матрица проверки на VPS (h3-only, h2-only, auto+fallback) без ручного редактирования:
  - `scripts/singbox_h2h3_matrix_vps.sh` (копируйте на VPS и запускайте через `bash`; скрипт восстанавливает исходный local JSON в конце).
- Подготовка отдельных конфигов без взаимных перезаписей:
  - `scripts/stand_prepare_http_layer_variants.sh` создаёт рядом с local JSON три файла:
    - `*.h3.json`
    - `*.h2.json`
    - `*.auto.json`
  - Для запуска конкретного варианта выставляйте `WARP_MASQUE_CONFIG=<path>` в compose.

Не вешайте **`default via`** на tun на **хосте**. `network_mode: host` открывает сервисы на стеке **хоста**; при необходимости SOCKS на **`127.0.0.1:1080`** — с ПК: `ssh -L 1080:127.0.0.1:1080 user@VPS` (это не замена TUN-smoke для агентов).

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
   # Отредактируйте configs/warp-masque-live.server.local.json (profile); при офлайн API см. README-warp-masque-live.md про ручной профиль / dataplane_port
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

5. Smoke (**TUN**, без SOCKS — см. раздел «Smoke и проверки агентов» выше):

```bash
docker exec sing-box-warp-masque-live-server ip route get 1.1.1.1
docker exec sing-box-warp-masque-live-server curl -sS --max-time 25 https://1.1.1.1/cdn-cgi/trace
```

### Порты MASQUE vs WireGuard (важно для live)

В публичной таблице firewall Cloudflare **MASQUE** часто идёт на **UDP 443** (и запасные порты), а **2408** в той же доке привязан к **WireGuard**. Профиль устройства API может отдавать массив вида `[2408 500 1701 4500]` с `tunnel_protocol`, указывающим на MASQUE: в текущем ядре при **`profile.dataplane_port_strategy: auto`** (по умолчанию) клиент **перебирает** кандидатов (443 и fallbacks, затем порты из профиля). Для строгого порядка только из API задайте **`profile.dataplane_port_strategy: api_first`**. Один фиксированный порт по-прежнему задаёт **`profile.dataplane_port`**.

В логах старта будет строка **`warp_masque dataplane try port=…`** при переборе; успешный порт кэшируется в **`hiddify_warp_masque_dataplane_cache.json`** в том же каталоге, что и **`warp_masque_device_state.json`** (по умолчанию `~root/.config/sing-box/` в контейнере), а не в фиксированном `/tmp` — так кэш остаётся записываемым при `network_mode: host` и без лишних bind-mount.

**Персистентные учётные MASQUE (usque-паритет):** control-plane Bearer + device id и ключи можно сохранять в JSON — поле **`profile.warp_masque_state_path`** в конфиге или **`HIDDIFY_WARP_MASQUE_DEVICE_STATE`** (путь к файлу). Если не заданы, по умолчанию используется каталог конфигурации пользователя процесса (`…/sing-box/warp_masque_device_state.json`). При `network_mode: host` путь обычно относится к **home root** на VPS; для фиксированного тома задайте env или абсолютный путь в JSON. Явные поля в конфиге sing-box перекрывают значения из state-файла.

Опционально для разбора маленьких UDP-ответов при handshake: переменная **`HIDDIFY_MASQUE_QUIC_HEX_SMALL_READS=1`** в `environment` сервиса compose (см. ниже).

При **`network_mode: host`** SOCKS слушает **на хосте** (`127.0.0.1:1080` в шаблоне) — только для ручного клиента с ПК: `ssh -N -L 1080:127.0.0.1:1080 user@YOUR_SERVER`. Для проверки работы WARP на стенде используйте **TUN + `curl` без `--proxy`**, как выше.

Для режима **bridge** + `ports:` см. исторические варианты; для WARP **QUIC** bridge часто даёт таймауты — предпочтителен host‑network.

## Диагностика dataplane (CONNECT-IP / netstack)

- **Происхождение бинаря:** образ копирует `artifacts/sing-box-linux-amd64` ([docker/Dockerfile.warp-masque-live](docker/Dockerfile.warp-masque-live)). На VPS: `docker exec sing-box-warp-masque-live-server strings /usr/local/bin/sing-box | grep -iE 'URLTest|monitoring'` и `sing-box version` — в выводе должны быть теги сборки (`with_masque`, …) и **Revision** коммита.
- **H2 vs H3:** сравните `http_layer: h2` ([configs/warp-masque-live.server.docker.h2.json](configs/warp-masque-live.server.docker.h2.json)) и `h3` ([configs/warp-masque-live.server.docker.json](configs/warp-masque-live.server.docker.json)) при одном и том же `profile`. При **`http_layer_fallback: true`** при сбое H2 клиент может перейти на H3 — смотрите логи `masque_http_layer_attempt` / `masque_http_layer_chosen`. TUN-smoke: `ip route get 1.1.1.1` и `curl` без SOCKS (см. выше).
- **Политика CONNECT-IP (отбрасывание пакетов):** в логах ищите `connect-ip: datagram … not allowed` и `connect-ip: dropping invalid outgoing proxied packet`. Для детализации **без payload** задайте в compose **`HIDDIFY_MASQUE_CONNECT_IP_DEBUG=1`** (см. таблицу ниже) — логируются адреса, протокол и длина кадра.
- **Префиксы для gVisor TCP (`tcp_transport: connect_ip`):** при **`HIDDIFY_MASQUE_CONNECT_IP_DEBUG=1`** при создании netstack пишется число префиксов из `CurrentAssignedPrefixes` / `LocalPrefixes` и выбранные локальные IPv4/IPv6. Если долго пусто — увеличьте **`MASQUE_CONNECT_IP_TCP_NETSTACK_PREFIX_WAIT_SEC`** (0–60 с, по умолчанию 8) в `environment` compose.
- **Метрики `CONNECT_IP_OBS`:** значение **`timeout`** у `connect_ip_packet_read_drop_reason` может соответствовать **`context.Canceled`** при закрытии сессии, а не только сетевому idle; отдельно учитываются **`canceled`** и **`deadline_exceeded`**.

Обзор MASQUE в продукте Cloudflare: [блог WARP + MASQUE](https://blog.cloudflare.com/masque-now-powers-1-1-1-1-and-warp-apps-dex-available-with-remote-captures); нормативка: RFC 9484 / 9298.

## Частые ошибки bootstrap

- **`profile.masque_ecdsa_private_key is required when device tunnel is MASQUE`**: для MASQUE dataplane нужен **ECDSA-ключ устройства** (как у usque). Либо задайте **`masque_ecdsa_private_key`** вручную (EC SEC1 base64 / PEM), либо оставьте **`profile.auto_enroll_masque`** включённым по умолчанию: при успешном control-plane и непустых **`profile.auth_token`** + **`profile.id`** ядро сгенерирует пару и выполнит enroll (**PATCH** `/reg/{id}`). Одного **WireGuard `private_key`** без enroll/ручного MASQUE-ключа по-прежнему недостаточно. Без рабочего MASQUE-ключа outbound не поднимется; **TUN-smoke** к `1.1.1.1` зависнет или вернёт таймаут.
- **`open_http3_client_conn` / `warp_masque startup failed: timeout: no recent network activity`** при рабочем TCP к API: **Docker bridge UDP** → **`network_mode: host`** в [docker-compose.warp-masque-live.server.yml](docker-compose.warp-masque-live.server.yml). В compose включено **`QUIC_GO_DISABLE_GSO` / `QUIC_GO_DISABLE_ECN`**. DNS-стратегию для MASQUE задавайте через штатные поля sing-box (`dns.strategy`, `dns.rules`, `domain_resolver`) — без хардкода IPv4 в transport-слое, чтобы резолв проходил по общей политике sing-box. Не подставляйте **`server` как голый IP** в endpoint без порта/SNI из профиля — легко получить **`tls: handshake failure`**. Если в кэше или профиле виден **WG-стиль порта**, а QUIC к MASQUE ожидается иначе, задайте в JSON **`profile.dataplane_port`** (часто **443**) — см. [`docs/masque/MASQUE-SINGBOX-CONFIG.md`](../../../../docs/masque/MASQUE-SINGBOX-CONFIG.md) §7.2.1.
- После ошибки **`runtime start`** в stdout/stderr ищите **`class=<ключ>`** — это [`ClassifyMasqueFailure`](../../../../hiddify-core/hiddify-sing-box/protocol/masque/errors_classify.go) (Extended CONNECT vs datagrams vs TLS vs 401 и т.д.).
- **`quic_experimental.enabled requires MASQUE_EXPERIMENTAL_QUIC=1`** — если снова включите `quic_experimental` в JSON, задайте в compose `environment: { MASQUE_EXPERIMENTAL_QUIC: "1" }`, иначе процесс завершится при старте.

## Переменные Compose

| Переменная | Значение |
|------------|---------|
| `WARP_MASQUE_CONFIG` | Путь к JSON (по умолчанию `./configs/warp-masque-live.server.local.json`) |
| `HIDDIFY_WARP_MASQUE_DEVICE_STATE` | Путь к JSON с **`auth_token`**, **`id`**, WireGuard и **MASQUE ECDSA** ключами (см. `profile.warp_masque_state_path` в [MASQUE-SINGBOX-CONFIG.md](../../../../docs/masque/MASQUE-SINGBOX-CONFIG.md) §7.2.1). Удобно на host network, чтобы не писать state в home root без явного тома. Рядом с этим файлом же хранится **`hiddify_warp_masque_dataplane_cache.json`** (кэш успешного UDP-порта). |

Дополнительно (пробросьте через `environment` в [docker-compose.warp-masque-live.server.yml](docker-compose.warp-masque-live.server.yml) при нужде):

| Переменная | Значение |
|------------|---------|
| `HIDDIFY_MASQUE_QUIC_HEX_SMALL_READS` | **`1`** — логировать hex первых байт входящих малых UDP-дейтаграмм на QUIC-dial обёртке (диагностика «16 байт» и т.п.; см. [MASQUE-SINGBOX-CONFIG.md](../../../../docs/masque/MASQUE-SINGBOX-CONFIG.md) раздел `warp_masque`). |
| `HIDDIFY_MASQUE_CONNECT_IP_DEBUG` | **`1`** — доп. логи политики CONNECT-IP (вход/исход) и выбора локальных адресов netstack (только адреса/длины, без payload). |
| `MASQUE_CONNECT_IP_TCP_NETSTACK_PREFIX_WAIT_SEC` | **0–60** — секунды ожидания `LocalPrefixes`, если `CurrentAssignedPrefixes` пуст (клиент по умолчанию 8). |

Файлы `*-server.local.json` и заполненный кэш с секретами **не коммитить**.

## Файлы по умолчанию compose

Общий [docker-compose.warp-masque-live.yml](docker-compose.warp-masque-live.yml): отдельный bind-mount под кэш dataplane **не** используется — см. `hiddify_warp_masque_dataplane_cache.json` рядом с state (выше).
