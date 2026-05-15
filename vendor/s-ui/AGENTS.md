# Ассистенты: репозиторий `vendor/s-ui` (в составе `hiddify-app`)

## Роль

Панель **s-ui** — веб-интерфейс и API поверх sing-box; в форке она живёт в `vendor/s-ui` и деплоится на VPS вместе с кастомным ядром (`hiddify-core`).

**Фокус корневого монорепо:** perf/reliability MASQUE — **`../AGENTS.md`** (цели **не закрыты**: H2 мёртв на стенде без Extended CONNECT, H3 медленный/нестабильный). **Полевой эталон:** §15.1c — Docker → **193.233.216.26:4438** → iperf **163.5.180.181** (`Benchmark-MasqueE2EChain.ps1`). **Деплой сервера:** `python run.py deploy` — обязателен после правок `hiddify-sing-box` в образе панели.

## Деплой стенда (основной способ)

Полный цикл: **локальная сборка Docker-образа** → передача на сервер → `docker compose up --no-build` (без долгой компиляции на VPS). Реализовано в **`run.py`** в корне `vendor/s-ui`.

1. Скопируйте `run.env.example` → `run.env` (файл в `.gitignore`, не коммитить).
2. Задайте `SUI_RUN_HOST`, `SUI_RUN_USER`, при необходимости `SUI_RUN_REMOTE_ROOT`, `SUI_RUN_COMPOSE_FILE` и TLS/verify (см. комментарии в `run.env.example`).

**PowerShell (Windows):**

```powershell
cd vendor/s-ui
$env:SUI_RUN_HOST = '193.233.216.26'   # или положить то же в run.env
python run.py deploy
```

**Bash:**

```bash
cd vendor/s-ui
SUI_RUN_HOST=193.233.216.26 python run.py deploy
```

Кратко, что делает `deploy`: `build` (локально) → `ship-image` → `push-compose` → `up --no-build`. Подробности в шапке `run.py` и в `deploy/TLS.md`.

Опции окружения (`SUI_RUN_DEPLOY_DOWN_FIRST`, `SUI_RUN_COMPOSE_PROFILES=le` и т.д.) — в комментариях `run.env.example` и в docstring `run.py`.

## Быстрые команды `run.py`

| Команда | Назначение |
|--------|------------|
| `deploy` | Полный деплой образа (см. выше) |
| `verify` | Проверка HTTPS/HTTP панели (`SUI_RUN_VERIFY_DOMAIN` / порт) |
| `certbot` | Первичная выдача Let's Encrypt на VPS (порт 80 свободен) |
| `binary` | Только залить уже собранный `vendor/s-ui/sui` в запущенный контейнер (см. ниже) |
| `redeploy-binary` | Локальный `docker compose build` → извлечь `sui` из образа → тот же путь, что `binary` |
| `restart` | Перезапуск через `deploy_sui.py restart` |

## Загрузка одного бинарника (`binary` / `redeploy-binary`)

Внутри вызывается **`deploy/deploy_sui.py`** (scp + `docker cp` в контейнер с именем из `SUI_DEPLOY_DOCKER_CONTAINER` и т.д.).

Хост и пользователь берутся так же, как в основном сценарии:

- предпочтительно **`SUI_RUN_HOST`** / **`SUI_RUN_USER`** из `run.env` (наследуются при `python run.py binary`);
- либо явно **`SUI_DEPLOY_HOST`** / **`SUI_DEPLOY_USER`** в `deploy/deploy.env` при ручном вызове `deploy_sui.py`.

Убедитесь, что задан контейнер, например в `run.env` нет `SUI_DEPLOY_*`, тогда в `deploy/deploy.env` (пример: `deploy.env.example`) — раскомментируйте `SUI_DEPLOY_DOCKER_CONTAINER=s-ui-local` и пути, как у вашего стенда.

**Не** считайте отдельный «легаси-деплой» сам факт использования `deploy_sui.py`: это низкоуровневая реализация для `binary`/`restart`; **легаси** — пытаться деплоить *только* через ручной `go build` + scp, минуя `run.py`, когда нужен полный образ.

## Документация в дереве s-ui

- `run.env.example` — переменные стенда.
- `deploy/TLS.md` — сертификаты, профили compose, `certbot`.
- `deploy/README-deploy.md` — детали `deploy_sui.py` (systemd/Docker, опциональные пути).
- `CONTRIBUTING.md` — разработка, теги Go, тесты.

## Принципы правок (согласовано с `hiddify-app`)

- Точечные изменения, обратная совместимость, без лишних фич вне задачи.
- Секреты и персональные `run.env` / `deploy.env` в git не коммитить.
