# TLS: переменные окружения и стенд Let’s Encrypt

Полный деплой стенда: [`../run.py`](../run.py) `deploy` / `certbot` / `verify` (см. [`../AGENTS.md`](../AGENTS.md)).

Панель и подписка выбирают пары `cert`/`key` в таком порядке:

1. **БД** — оба пути заданы и **оба файла существуют** на диске.
2. **Env для панели** — `SUI_WEB_TLS_CERT` и `SUI_WEB_TLS_KEY` (оба заданы и файлы есть).
3. **Фоллбек** — `SUI_TLS_FALLBACK_CERT` и `SUI_TLS_FALLBACK_KEY`; если не заданы, используются `/app/cert/fullchain.pem` и `/app/cert/privkey.pem` (оба файла должны существовать).

Для **подписки** (отдельный порт):

1. БД — `subCertFile` / `subKeyFile`, оба файла на месте.
2. **Env** — `SUI_SUB_TLS_CERT` и `SUI_SUB_TLS_KEY`.
3. Иначе — **та же пара**, что уже разрешена для панели (один сертификат на оба сервиса).

Если в БД указаны пути, но файлов нет, битая пара из БД **не** используется — срабатывают env и фоллбек.

## Таблица переменных

| Переменная | Назначение |
|------------|------------|
| `SUI_WEB_TLS_CERT` | Полный путь к цепочке сертификата панели |
| `SUI_WEB_TLS_KEY` | Приватный ключ панели |
| `SUI_SUB_TLS_CERT` | Цепочка для подписки (опционально) |
| `SUI_SUB_TLS_KEY` | Ключ для подписки (опционально) |
| `SUI_TLS_FALLBACK_CERT` | Переопределение пути фоллбека (вместо `/app/cert/fullchain.pem`) |
| `SUI_TLS_FALLBACK_KEY` | Переопределение ключа фоллбека (вместо `/app/cert/privkey.pem`) |

URI подписки и команда `sui uri` выводят `https`, если после этого разрешения для соответствующего сервиса есть валидная пара.

## Bootstrap стенда (`docker-compose.stand.yml`)

Рекомендуемый путь — [`../run.py`](../run.py) из `vendor/s-ui`: **образ собирается на вашей машине**
(кэш слоёв Docker и go из Dockerfile), на VPS передаётся готовый образ (`docker save | ssh docker load`),
подъём — `docker compose up -d --no-build`. Так не получается «зависания» из‑за полного `go build` на сервере.

```bash
cd vendor/s-ui
cp run.env.example run.env   # SUI_RUN_HOST; опционально SUI_RUN_COMPOSE_PROFILES=le (авто-renew в compose)
python run.py deploy         # build (локально) → образ на VPS → up --no-build
python run.py certbot        # первичная выдача LE (порт 80 на VPS свободен)
python run.py verify         # curl -I (HTTPS, иначе HTTP)
```

Долгая сборка на VPS: только команда `python run.py remote-build` (полный sync + `docker compose build` там).

Если база раньше лежала в `/opt/s-ui/db`, один раз скопируйте её в дерево нового корня, например:
`/opt/hiddify-app/vendor/s-ui/db/` (см. `SUI_RUN_REMOTE_ROOT`).

Ручные команды на сервере (эквивалент):

1. **DNS**: запись `A` для `work.ai-qwerty.ru` → IP VPS (иначе HTTP-01 не пройдёт).
2. На хосте **свободен порт 80** на время выдачи сертификата (режим `standalone`).
3. Первичная выдача (один раз, том `letsencrypt_data` сохраняется между деплоями):

   ```bash
   cd vendor/s-ui
   docker compose -f docker-compose.stand.yml --profile cert run --rm --service-ports certbot certonly --standalone \
     -d work.ai-qwerty.ru --email netort@internet.ru --agree-tos --non-interactive
   ```

4. Запуск панели; **фоновый renew** — только если нужен Let’s Encrypt внутри compose:

   ```bash
   # только s-ui-local (TLS у nginx и т.п.)
   docker compose -f docker-compose.stand.yml up -d
   # панель + certbot-renew (профиль `le`)
   docker compose -f docker-compose.stand.yml --profile le up -d
   ```

   Из [`run.py`](../run.py): задайте в `run.env` **`SUI_RUN_COMPOSE_PROFILES=le`**, чтобы `deploy`/`up` передавали тот же профиль.

Сервис `certbot-renew` (профиль `le`) раз в 12 часов вызывает `certbot renew`; при успешном продлении отправляется **HUP** контейнеру `s-ui-local`, процесс перезапускает приложение и подхватывает новые файлы (симлинки в `/etc/letsencrypt/live/...`).

Проверка:

```bash
curl -sI "https://work.ai-qwerty.ru:2095/"
openssl s_client -connect work.ai-qwerty.ru:2095 -servername work.ai-qwerty.ru </dev/null 2>/dev/null | openssl x509 -noout -dates
```

При необходимости сопоставьте IP до появления DNS:

```bash
curl -sI --resolve "work.ai-qwerty.ru:2095:31.56.211.60" "https://work.ai-qwerty.ru:2095/"
```

Опционально: [`scripts/certbot-init.sh`](../scripts/certbot-init.sh) — проверка наличия файлов перед повторным `certonly`.

## Self-signed без домена

Для VPS без домена можно включить генерацию самоподписанного сертификата прямо в контейнере
(`entrypoint.sh`) через env:

```bash
cd vendor/s-ui
SUI_RUN_HOST=163.5.180.181 \
SUI_RUN_VERIFY_DOMAIN=163.5.180.181 \
SUI_TLS_SELF_SIGNED=1 \
SUI_TLS_SELF_SIGNED_DAYS=36500 \
SUI_TLS_SELF_SIGNED_CN=163.5.180.181 \
SUI_WEB_TLS_CERT=/app/cert/selfsigned/fullchain.pem \
SUI_WEB_TLS_KEY=/app/cert/selfsigned/privkey.pem \
python run.py deploy
```

Поведение:
- если файлов по `SUI_WEB_TLS_CERT/SUI_WEB_TLS_KEY` нет, контейнер сгенерирует пару автоматически;
- срок задаётся `SUI_TLS_SELF_SIGNED_DAYS` (например `36500`);
- путь до сертификата/ключа полностью управляется env.
