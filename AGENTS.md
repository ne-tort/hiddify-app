# Правила для ассистентов (репозиторий `hiddify-app`)

## Основная цель текущего этапа

Интегрировать `L3Router` в `s-ui` как first-class endpoint:

- сборка `s-ui` на ядре `hiddify-core/hiddify-sing-box`,
- поддержка `l3router` в backend API и frontend UI панели,
- автогенерация и поддержание peer-привязок пользователей,
- корректная генерация итогового `sing-box` конфига с rules,
- отсутствие регрессий существующего поведения панели.

## Где находится интеграция

- Вендорная копия `s-ui`: `vendor/s-ui`.
- Backend панели: `vendor/s-ui/service`, `vendor/s-ui/core`, `vendor/s-ui/database`.
- Frontend панели: `vendor/s-ui/frontend`.
- Ядро: `hiddify-core/hiddify-sing-box`.

## Архитектурные инварианты (обязательные)

- `l3router` остается endpoint-типом sing-box (`type: l3router`) и не переупаковывается в outbound.
- У каждого клиента есть стабильный `peer_id` в `client.config.l3router`, одинаковый для всех `l3router` endpoint.
- У каждого клиента есть фиксированный `user` в `client.config.l3router`, синхронизированный с именем клиента.
- При создании endpoint или клиента peer-запись должна автоматически появляться без ручного шага администратора.
- ACL-поля (`filter_source_ips`/`filter_destination_ips`) не должны принудительно включаться; `packet_filter` по умолчанию `false`.
- Значения по умолчанию должны быть безопасными: без loopback-адресов и без пустых peer-конфигов, ломающих старт endpoint.

## Правила синхронизации данных

- Источник истины по пользователям: таблица `clients`.
- Источник истины по endpoint: таблица `endpoints`.
- Источник истины по peer identity: `clients.config.l3router`.
- Любая операция над клиентами (`new/edit/del/bulk`) должна вызывать пересборку peer-списков всех `l3router` endpoint.
- Любая операция `new/edit` над endpoint типа `l3router` должна нормализовать peers на основе актуальных клиентов.
- Удаленный клиент должен исчезать из peers всех `l3router` endpoint.

## Runtime и совместимость

- Базовый режим: панель генерирует полный JSON и умеет runtime add/remove endpoint.
- Runtime API `l3router` в clashapi считать дополнительным ops-инструментом; не делать его обязательным для базового пути.
- Если требуется runtime-пересинхронизация peers, допускается безопасный remove/add конкретного endpoint.
- Не ломать существующие протоколы (`wireguard`, `warp`, `tailscale`, inbounds/outbounds/services).

## Требования к UI

- В `s-ui-frontend` тип `L3Router` должен быть доступен в селекторе endpoint.
- Форма endpoint должна поддерживать хотя бы базовые поля:
  - `tag`,
  - `overlay_destination`,
  - `packet_filter`,
  - `peers` (редактируемый список).
- Для `l3router` не выводить нерелевантные поля dial/multiplex, если они не используются.
- Отображение endpoint в списке должно быть читаемым (минимум peers count и overlay destination).

## Требования к сборке

- `vendor/s-ui/go.mod` должен использовать локальный `hiddify-sing-box` через `replace`.
- Build tags для `s-ui` и Docker-образов должны включать `with_l3router`.
- Регистрация `l3router` в `vendor/s-ui/core` обязана иметь:
  - `with_l3router`-реализацию,
  - `!with_l3router`-stub с явной ошибкой пересборки.

## Проверки перед сдачей

- `go test -mod=mod ./...` в `vendor/s-ui`.
- Минимум smoke по ядру: `go test ./protocol/l3router -count=1` в `hiddify-core/hiddify-sing-box`.
- Проверка, что в diff нет тяжелых артефактов (`node_modules`, `dist`, бинарники, дампы).
- Проверка, что `client.config` мигрируется и не теряет существующие креды других протоколов.

## Политика изменений в ядре

- Правки в `hiddify-core/hiddify-sing-box` допускаются только для совместимости с `s-ui`.
- Любая правка ядра должна быть минимальной и иметь понятный интеграционный мотив.
- Не расширять API без необходимости для текущего этапа.

## Формат отчета по этапу

В итоговом отчете обязательно указывать:

- что изменено в `vendor/s-ui` (backend, frontend, core registration, build),
- что изменено в `hiddify-core/hiddify-sing-box` (если изменялось),
- какие команды верификации выполнены и их результат,
- какие риски/ограничения остаются (например, loopback-сценарии без tun address требуют отдельного e2e).
