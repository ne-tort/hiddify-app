# Static-only `l3router` scenario

Этот стенд фиксирует единственный штатный режим: `l3router` в модели hub-and-spoke со статическими peer-like маршрутами в JSON.

## Правила сценария

- Никаких runtime вызовов `POST/DELETE /proxies/{name}/routes`.
- Никаких fallback/legacy/compatibility обходов.
- Изменения маршрутизации делаются только через правку `configs/*.json` и rollout.
- Пиры (`routes`) для клиентов создаются заранее на сервере в `configs/server.l3router.static.json`.
- `clashapi` остаётся только для наблюдения и администрирования, но не для hot-create пиров в этом стенде.

## Файлы

- `configs/server.l3router.static.json` — сервер-хаб с endpoint `l3router` и статическими route.
- `configs/client-a.static.json` — клиент `client-a`.
- `configs/client-b.static.json` — клиент `client-b`.
- `scripts/deploy_l3router_server_static.sh` — деплой static-only конфига на сервер.
- `scripts/smoke_l3router_static.ps1` — smoke-проверка без route API.
- `scripts/smb_transfer_100mb_static.sh` — обязательный тест `>=100MB` + hash/time.
- `runtime/latest_100mb_metrics.json` — итоговые метрики последнего прогона.

## Быстрый порядок запуска

1. Скопировать static конфиги на сервер/клиенты.
2. Применить `deploy_l3router_server_static.sh`.
3. Запустить `smoke_l3router_static.ps1` (online режим дополнительно валидирует отсутствие hot-upsert по метрикам).
4. Запустить `smb_transfer_100mb_static.sh`.
5. Проверить `runtime/latest_100mb_metrics.json` (`throughput_mib_per_sec`, `throughput_mbit_per_sec`).
