# Static-only `l3router` scenario

Стенд hub-and-spoke: статические `routes` в JSON, без hot route API в штатном режиме.

## Что должно быть на месте (чеклист готовности)

| Компонент | Требование |
|-----------|------------|
| **Сервер (VPS)** | Бинарь sing-box **собран из этого форка** (`l3router` в upstream-образах нет). См. сборку в корневом `AGENTS.md`. |
| **Конфиг сервера** | `configs/server.l3router.static.json` — TLS-сертификаты на путях из конфига, `secret` clash API заменён, при необходимости `external_controller` слушает только localhost + доступ по SSH-туннелю. |
| **Клиенты** | В `configs/client-*.static.json` подставить IP VPS вместо `31.56.211.60`, `server_name` TLS совпадает с сертификатом (`insecure`/CA по ситуации). |
| **Docker** | Образ **не** `ghcr.io/sagernet/sing-box:latest` — используется локальная сборка из `docker/Dockerfile` (см. ниже). |
| **Два клиента на одном хосте** | Разные TUN: `client-a` — `tun0`, `client-b` — `tun1` (уже в репозитории). |

## Файлы

- `configs/server.l3router.static.json` — хаб, `l3router` + inbound VLESS.
- `configs/client-a.static.json`, `configs/client-b.static.json` — TUN + VLESS до хаба.
- `docker/Dockerfile` — образ sing-box с тегами `with_gvisor,with_clash_api,with_utls`.
- `docker/Dockerfile.smb-client` + `docker/entrypoint-smb-client.sh` — клиентский образ: `sing-box` + `smbd` + `smbclient` в одном контейнере.
- `docker-compose.l3router-static-clients.yml` — два клиента (build из корня `hiddify-sing-box`).
- `docker-compose.l3router-e2e-reality-smb.yml` — **два SMB-клиента в Docker**, каждый с собственным share/логином/паролем, сервер (VPS) ничего не знает про SMB.
- `scripts/deploy_l3router_server_static.sh` — копирование конфига на VPS и `systemctl restart sing-box`.
- `scripts/smoke_l3router_static.ps1` — smoke (Windows), в т.ч. offline.
- `scripts/smoke_static_config.sh` — offline-проверка JSON (Linux/macOS, нужен `jq`).
- `scripts/smoke_l3router_controller.sh` — проверка метрик без hot route API (нужен `curl` + доступ к controller).
- `scripts/run_stand_tests.sh` — запуск offline-тестов + опционально controller.
- `scripts/smb_transfer_100mb_static.sh` — **не SMB**: локальный dd/cp/sha256 на одной машине (устаревшее имя файла).
- `scripts/smb_transfer_100mb_e2e.sh` — **настоящий SMB** (smbd на `10.0.0.3` / tun1, `smbclient` с `10.0.0.2` / tun0): 100 MiB PUT, замер времени, **MiB/s и Mbit/s**, отчёт `runtime/smb_100mb_e2e_latest.json`. Нужны пакеты `samba` и `smbclient` на хосте с tun (обычно VPS после `e2e`).
- `scripts/e2e_tcp_udp_100mb.ps1` — 100 MiB туда-обратно без SMB: отдельные TCP/UDP listener+sender, listener перезапускается на каждый прогон направления, отчёт `runtime/tcp_udp_100mb_latest.json`.
- `scripts/e2e_iperf_matrix.ps1` — матрица `iperf3` для нагрузки: TCP (`single`, `reverse`, `-P 4`, `-P 8`, `--bidir`) и UDP (`-u`, разные `-b`, `-P`), сырые JSON по кейсам + сводка.
- `scripts/e2e_ping_peer.sh` — ICMP до IP peer-подсети после поднятия туннелей.

## Сборка бинаря сервера (Linux amd64)

Из каталога `experiments/router/hiddify-sing-box`:

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -tags with_gvisor,with_clash_api,with_utls -o sing-box-linux-amd64 ./cmd/sing-box
```

Установить бинарь и unit `sing-box` на VPS, положить `server.l3router.static.json` в путь из `deploy_l3router_server_static.sh` (по умолчанию `/etc/sing-box/config.json`).

## Деплой конфига на VPS

```bash
export L3ROUTER_SERVER_HOST=your.vps.ip
# опционально: L3ROUTER_SERVER_USER, L3ROUTER_SERVER_CONFIG_PATH, L3ROUTER_SERVER_SERVICE
./scripts/deploy_l3router_server_static.sh
```

## Clash API для smoke (метрики)

В конфиге сервера `external_controller` часто `127.0.0.1:9090`. С ноутбука:

```bash
ssh -N -L 9090:127.0.0.1:9090 root@your.vps.ip
export L3ROUTER_CONTROLLER_URL=http://127.0.0.1:9090
export L3ROUTER_CONTROLLER_SECRET='тот же secret, что в конфиге сервера'
./scripts/smoke_l3router_controller.sh
```

## Клиенты в Docker

Из каталога **этого** стенда (`static-no-control-plane`):

```bash
docker compose -f docker-compose.l3router-static-clients.yml build
docker compose -f docker-compose.l3router-static-clients.yml up -d
```

Сборка контекста — три уровня вверх до корня `hiddify-sing-box`.

## Тесты стенда

**Локально (без VPS):**

```bash
chmod +x scripts/*.sh
./scripts/run_stand_tests.sh
```

**E2E L3 между клиентами** (после установления сессий к хабу), с хоста с доступом к peer IP:

```bash
./scripts/e2e_ping_peer.sh 10.10.2.2
```

### 100 MiB по SMB через l3router (замер скорости)

На **VPS**, где уже работают `docker-compose.l3router-e2e-reality.yml` (tun0=`10.0.0.2`, tun1=`10.0.0.3` на хосте):

```bash
apt-get update && apt-get install -y samba smbclient
cd /opt/l3router-e2e/static-no-control-plane   # или ваш путь к стенду
sudo bash scripts/e2e_vps_run.sh
```

Скрипт `e2e_vps_run.sh` после успешного ping запускает `smb_transfer_100mb_e2e.sh` (если установлены `smbd` и `smbclient`). Результат: консоль с **длительностью**, **~MiB/s** и **~Mbit/s**, плюс JSON `runtime/smb_100mb_e2e_latest.json`.

Детали сценария SMB:

- Порт **1445** (не 445): на сервере часто уже крутится системный Samba на 445; иначе клиент попадает в чужие шары (`NT_STATUS_BAD_NETWORK_NAME`). Переопределить: `SMB_PORT=...`.
- В `client.reality-client1/2.json` заданы **`iproute2_table_index`** (2025 / 2026) и **`inet4_route_address`**: иначе два sing-box на одном хосте с `network_mode: host` конфликтуют по policy-routing (`add route … file exists`), второй клиент не поднимает tun1.
- Тестовый share использует **`force user = root`** только для гостевого доступа на изолированном стенде; в проде замените на свою модель учёток.

Только SMB без перезапуска compose (compose уже поднят, tun есть):

```bash
sudo bash scripts/smb_transfer_100mb_e2e.sh
```

**Windows:** `scripts/smoke_l3router_static.ps1 -Offline` или с туннелем к controller без `-Offline`.

### Эталонный SMB-сценарий (SMB только в клиентских контейнерах)

Это сценарий, где:

- VPS-сервер: только `vless + l3router`.
- SMB: только внутри клиентских Docker-контейнеров.
- Клиенты ходят по SMB друг к другу через overlay (`10.0.0.2 <-> 10.0.0.4`), шары и креды разные.

Запуск:

```bash
cd /opt/l3router-e2e/static-no-control-plane   # или локальный путь к стенду
chmod +x scripts/e2e_smb_clients_100mb.sh
sudo bash scripts/e2e_smb_clients_100mb.sh
```

Что делает `e2e_smb_clients_100mb.sh`:

- поднимает `docker-compose.l3router-e2e-reality-smb.yml` (сборка `sing-box-l3router-smb:local`);
- проверяет L3 до peer через tun;
- передаёт 100 MB в обе стороны по SMB:
  - `l3router-smb-client1` -> `//10.0.0.4/PEER_C3_BETA` (`owner_c/owner_c_2026`);
  - `l3router-smb-client2` -> `//10.0.0.2/PEER_C1_ALPHA` (`owner_a/owner_a_2026`);
- считает время, примерно `Mbit/s`, проверяет SHA256;
- пишет отчёт в `runtime/smb_clients_100mb_latest.json`.

### TCP/UDP сценарий (без SMB, те же клиенты)

Запуск на локальной машине (PowerShell):

```powershell
docker compose -f docker-compose.l3router-e2e-reality-smb.yml up -d --no-build
powershell -ExecutionPolicy Bypass -File .\scripts\e2e_tcp_udp_100mb.ps1
```

Результаты:

- `runtime/tcp_udp_100mb_latest.json` — подробный отчёт по TCP и UDP в обе стороны.
- `runtime/compare_smb_tcp_udp_100mb_latest.json` — сводное сравнение SMB/TCP/UDP (средняя скорость и целостность).
- `runtime/iperf_matrix_latest.json` — матрица `iperf3` с loss/jitter/retransmits и агрегатной сводкой.
- `runtime/iperf-raw/*.json` — сырой вывод `iperf3 -J` по каждому кейсу.

## Правила сценария

- Не использовать runtime `POST/DELETE /proxies/{name}/routes` в штатном режиме.
- Изменения маршрутов — только правка JSON и rollout.
