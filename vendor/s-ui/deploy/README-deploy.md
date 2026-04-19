# Деплой s-ui на VPS (образец l3router stand)

Скрипт [`deploy_sui.py`](deploy_sui.py) повторяет идеи из `experiments/router/stand/l3router/l3router_stand/deploy.py`: загрузка бинарника по `scp`, проверка SHA-256, перезапуск **systemd** или обновление бинарника в **Docker**-контейнере.

## Требования

- Локально собранный `sui` (из корня `vendor/s-ui`: `go build -tags "..." -o sui .`).
- SSH-ключ к серверу, `scp`/`ssh` без интерактивного пароля.
- На сервере: либо unit `s-ui` и каталог `/usr/local/s-ui/` (см. основной README), либо Docker с проброшенным контейнером.

## Настройка

1. Скопируйте примеры (не коммитьте секреты):

   ```bash
   cd vendor/s-ui/deploy
   cp deploy.env.example deploy.env
   cp deploy.config.json.example deploy.config.json
   ```

2. Укажите хост (например VPS с адресом `31.x.x.x`) в `deploy.env`:

   ```env
   SUI_DEPLOY_HOST=31.x.x.x
   SUI_DEPLOY_USER=root
   SUI_DEPLOY_LOCAL_BINARY=../sui
   ```

   Либо отредактируйте `deploy.config.json` (те же поля; при наличии **обоих** файла переменные окружения имеют приоритет там, где заданы).

3. Опционально: `deploy.yaml` — дублирует JSON; для автоподхвата установите PyYAML (`pip install pyyaml`).

## Команды

```bash
# Загрузить бинарник и перезапустить сервис
python3 deploy_sui.py binary

# Только перезапуск (после ручных правок на сервере)
python3 deploy_sui.py restart
```

### Docker на удалённом хосте

В `deploy.env`:

```env
SUI_DEPLOY_DOCKER_CONTAINER=s-ui-local
SUI_DEPLOY_REMOTE_BINARY=/app/sui
```

Скрипт выполнит `docker stop`, `docker cp` нового `sui` в контейнер, `docker start`, ожидание статуса `running`.

### Свой перезапуск

```env
SUI_DEPLOY_RESTART_CMD=docker compose -f /path/to/compose.yml up -d
```

Тогда `systemctl` не используется.

## Удаление 3X-UI / x-ui (освободить 2095/2096)

На сервере обычно стоит `x-ui.service` и бинарь `/usr/bin/x-ui`. Консольное удаление:

```bash
x-ui uninstall
```

Неинтерактивно (подтверждение «y» на все вопросы):

```bash
yes | x-ui uninstall
```

После этого порты панели освобождаются для s-ui.

## Порты панели

- В настройках БД: `webPort` / `subPort` (как в UI).
- Переопределение без правки SQLite: переменные окружения процесса **`SUI_WEB_PORT`** и **`SUI_SUB_PORT`** (валидный порт 1–65535). Удобно в **systemd** (`Environment=` в override) или в `docker run -e`.

Проброс только с хоста: `docker run -p 8443:2095` — внутри контейнера порт остаётся 2095, если не задан `SUI_WEB_PORT`.

Локальный compose [`docker-compose.local.yml`](../docker-compose.local.yml) использует **`network_mode: host`**: панель и подписка слушают **2095/2096 прямо на хосте** (без `-p`).

Стенд с Let’s Encrypt (том сертификатов, env-пути к PEM, `certbot` и авто-renew с HUP): см. [`TLS.md`](TLS.md) и [`docker-compose.stand.yml`](../docker-compose.stand.yml). Деплой: [`../run.py`](../run.py) — образ **собирается локально** (кэш Docker/Go), на VPS передаётся готовый образ, `up --no-build`. Краткий порядок: DNS A на VPS → при необходимости `python run.py certbot` → `python run.py deploy`.

## Проверка на VPS (вручную)

После `deploy_sui.py binary`:

- systemd: `ssh user@host systemctl status s-ui`
- Откройте в браузере `http://HOST:WEB_PORT/...` согласно настройкам.

IP и ключи в репозиторий не помещать.
