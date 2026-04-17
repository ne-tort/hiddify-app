# l3router — единый интеграционный стенд

Один каталог: конфиги, Docker Compose, скрипты e2e и **входная точка** [`run.py`](run.py) (Python 3, только стандартная библиотека).

## Быстрый старт

```bash
cd experiments/router/stand/l3router
cp .env.example .env   # задать L3ROUTER_SERVER_HOST и при необходимости пользователя SSH
python run.py all --smb-build
```

`all` выполняет: **сборка** linux/amd64 бинаря → **деплой** `configs/server.l3router.static.json` на VPS → **Docker** (образ `sing-box-l3router:local`, затем стек SMB) → **тест** 100 MiB SMB в обе стороны между клиентами (`scripts/e2e_smb_clients_100mb.sh`).

Флаги: `--skip-deploy`, `--skip-clients`, `--skip-test`; `--with-binary` дополнительно заливает `artifacts/sing-box-linux-amd64` на сервер.

## Команды `run.py`

| Команда | Действие |
|---------|----------|
| `build` | `go build` из `hiddify-core/hiddify-sing-box` → `artifacts/sing-box-linux-amd64` |
| `deploy` | `scp` серверного JSON + `ssh systemctl restart`; `--with-binary` — ещё бинарь |
| `clients` | `docker compose`; по умолчанию стек **smb** (нужен предварительно собранный базовый образ) |
| `test` | SMB 100 MiB; `--legacy-local-disk` — только offline dd/cp (`scripts/legacy/`) |
| `all` | цепочка выше |

Модули: [`l3router_stand/`](l3router_stand/).

## Сборка образа sing-box (Docker)

Source of truth для сборки стенда: `hiddify-core/hiddify-sing-box` (через `l3router_stand/paths.py` -> `SING_BOX_ROOT`).
Контейнеры клиентов собираются из локального артефакта `artifacts/sing-box-linux-amd64`.

Локально:

```bash
cd experiments/router/stand/l3router
docker compose -f docker-compose.l3router-static-clients.yml build
```

## Требования

- **Python 3.10+**
- **Go** (для `build`)
- **Docker** + `docker compose`
- **ssh/scp** к VPS (для `deploy`)
- **bash** для e2e-скриптов (Linux, macOS, **WSL2** или Git Bash на Windows)

## Конфиги

Каталог [`configs/`](configs/): сервер `server.l3router.static.json`, клиенты `client-*.json` / Reality — подставьте IP VPS и секреты. Пути к TLS в JSON должны существовать на целевых машинах.

В endpoint `l3router` у каждого элемента **`peers`** обязательны **`peer_id`**, **`user`** (как имя пользователя inbound) и **`allowed_ips`** (LPM, семантика WG AllowedIPs). Поля **`filter_source_ips` / `filter_destination_ips`** используются только при **`packet_filter`: true**; по умолчанию выключено.

## Прочие скрипты

| Скрипт | Назначение |
|--------|------------|
| `scripts/deploy_l3router_server_static.sh` | только конфиг + restart (как вызывает `deploy`) |
| `scripts/e2e_vps_run.sh` | ping + опционально SMB на хосте с tun (VPS) |
| `scripts/smb_transfer_100mb_e2e.sh` | SMB через tun на VPS |
| `scripts/run_stand_tests.sh` | локальные smoke без Docker |
| `scripts/legacy/smb_transfer_100mb_static.sh` | **не** l3router, локальный диск (имя историческое) |

Синтетические CPU-бенчмарки: [`../../bench/collect_phase0_baseline.ps1`](../../bench/collect_phase0_baseline.ps1) (не часть стенда деплоя).

## Устаревшие пути

Старая копия `experiments/router/hiddify-sing-box` не используется в build/deploy цепочке стенда.
