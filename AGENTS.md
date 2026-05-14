# AGENTS — handoff: s-ui — интеграция MASQUE и MASQUE WARP (панель + БД + подписки + клиенты)

Документ для ИИ/разработчика по **текущей задаче**: архитектурно корректно встроить в **форк s-ui** (`vendor/s-ui`) sing-box-эндпоинты **`masque`** (сервер/клиент) и **`warp_masque`**, по тем же слоям, что уже используются для других endpoint’ов, инбаундов, клиентов, групп и подписок. Это **общее задание** для следующих итераций (не детальное ТЗ).

**Корень монорепо:** `c:\Users\qwerty\git\hiddify-app` (или эквивалент). Панель: **`vendor/s-ui/`**. Семантика полей MASQUE в ядре: **`docs/masque/MASQUE-SINGBOX-CONFIG.md`**, опции Go: **`hiddify-core/hiddify-sing-box/option/masque.go`**, рантайм: **`hiddify-core/hiddify-sing-box/protocol/masque/`**.

**Деплой Docker-панели** (отдельно от этой задачи): см. **`vendor/s-ui/AGENTS.md`** (`run.py`, `run.env`).

---

## 1. Цель

1. **Endpoint `masque`** в панели: создание/редактирование серверного и клиентского варианта (как для `wireguard` / `l3router` / `awg`), с **выбором схемы аутентификации** на сервере (`server_auth`: policy, Bearer/Basic/mTLS и т.д. по доке MASQUE), который **одновременно** задаёт поведение ядра и **то, что попадает в подписку** (или в отдельный формат выдачи — см. §6).

2. **Endpoint `warp_masque`** (или согласованное имя типа в UI ↔ `warp_masque` в JSON ядра): отдельный путь от **legacy WARP** (`type: warp` в БД → в ядро как `wireguard`). Нужна явная модель хранения **профиля/состояния** после регистрации и способ **материализации** в sing-box (см. §5).

3. **Система клиентов**: для каждого клиента — секреты, привязанные к протоколу **аналогично** `client.config` для SS/VLESS/… и **wireguard** (отдельные computed в модалке). Для MASQUE: как минимум **Bearer / Basic / клиентский mTLS-материал** (и при необходимости inline PEM/base64), **даже если** соответствующий server endpoint ещё не создан — те же абстракции, что для «секретов под outbound» / WG private key (источник истины и UI — согласовать в реализации).

4. **Привязки клиентов/групп к endpoint’у**: как у **`member_group_ids` / `member_client_ids`** в **`Wireguard.vue`** / **`L3Router.vue`**, или как **inbound policies** (`inbound_policy.go` + `Users.vue` / `Inbound.vue` с `inbound_init`). Нужен **единый** выбранный паттерн для MASQUE server (не смешивать без причины два разных механизма в одном типе).

5. **Подписка**: расширить цепочки **`sub/jsonService.go`**, **`util/genLink.go`**, при необходимости **`util/linkToJson.go`**, **`sub/clashService.go`**, отдельные форматы (`?format=...`) по образцу **`wg_json_patch.go`** / **`awg_json_patch.go`** / **`l3_tun_patch.go`**. Сейчас **`InboundTypeWithLink`** и генераторы ссылок **не знают** MASQUE — это ожидаемая точка расширения.

6. **UI endpoint’а**: модалка **`frontend/src/layouts/modals/Endpoint.vue`**, типы в **`frontend/src/types/endpoints.ts`**, компонент протокола в **`frontend/src/components/protocols/`** (новый или композиция), импорт JSON — **`utils/endpointImport.ts`** + **`EndpointImport.vue`**. **i18n** — все ключи во **всех** `frontend/src/locales/*.ts`.

---

## 2. Карта репозитория (куда смотреть в первую очередь)

| Слой | Пути |
|------|------|
| Модель endpoint, `Options` / `Ext`, strip для sing-box | `vendor/s-ui/database/model/endpoints.go`, `endpoints_test.go` |
| Миграции / нормализация БД | `vendor/s-ui/database/db.go`, `migrate_*.go`, при необходимости `vendor/s-ui/cmd/migration/` |
| Клиенты, группы, ACL инбаунда | `vendor/s-ui/database/model/model.go`, `user_group.go`, `inbound_policy.go`, `l3_router_peer.go` (эталон «отдельная таблица per-client») |
| CRUD, сборка sing-box | `vendor/s-ui/service/config.go`, `service/endpoints.go`, `service/inbounds.go`, `service/client.go`, `service/warp.go` |
| HTTP API | `vendor/s-ui/api/apiService.go` (`Save` / `LoadPartialData`), `apiHandler.go`, `apiV2Handler.go` |
| Подписка | `vendor/s-ui/sub/jsonService.go`, `subService.go`, `linkService.go`, `clashService.go`, `wg_json_patch.go`, `awg_json_patch.go`, `l3_tun_patch.go`, `wg_conf_service.go` |
| Утилиты ссылок / внешних подписок | `vendor/s-ui/util/genLink.go`, `linkToJson.go`, `subToJson.go`, `subInfo.go` |
| UI клиента | `vendor/s-ui/frontend/src/layouts/modals/Client.vue`, `types/clients.ts` |
| UI endpoint / протоколы | `vendor/s-ui/frontend/src/layouts/modals/Endpoint.vue`, `components/protocols/*.vue`, `types/endpoints.ts` |

---

## 3. Инварианты форка (как уже сделано для других типов)

- **`model.Endpoint`**: поле **`type`** (строка sing-box / псевдоним панели), **`tag`**, **`Options`** (JSON всех полей кроме `ext` / служебных), **`Ext`** (отдельный JSON-блоб). **`UnmarshalJSON`** / **`MarshalJSON`** — обязательная дисциплина: в ядро **не** утекают UI-only ключи (см. ветки `l3router`, `wireguard`, `awg`, особый случай **`warp` → `wireguard`**).
- **Сохранение**: единая точка **`POST .../save`** с `object`, `action`, `data` → **`ConfigService.Save`** → **`EndpointService.Save`** / транзакции, при необходимости **`NeedsReload`** / **`ApplyRuntimeAction`** (см. `service/endpoints.go`).
- **Секреты в логах и в репозитории** — не коммитить живые токены, приватные ключи, полные прод-профили `warp_masque`.

---

## 4. Слой БД и миграций

- Новый тип может жить **только** в JSON `options`/`ext` **или** потребовать **отдельную таблицу** (паттерн **`L3RouterPeer`**: per-client строки, `endpoint_id`, `client_id`, JSON массивов; `peer_id` не хранится — читается из `client.Config` при материализации). Для MASQUE server с **большим ACL** и **per-client overrides** заранее спроектировать, что остаётся в **`server_auth`** JSON эндпоинта, а что вынесено в таблицу ради запросов и целостности.
- Любая нормализация «из JSON endpoint в таблицу» — идемпотентные **`Migrate*`** после `AutoMigrate` в **`database/db.go`** (как `migrate_l3_router_peers.go`).

**Решения на реализацию (не зафиксированы здесь):** одна таблица «masque_client_credentials» vs расширение `client.config` vs гибрид; критерий — **предсказуемость** подписок и **совпадение** с UX «секреты клиента в одной модалке».

---

## 5. Legacy WARP в s-ui vs MASQUE WARP (`warp_masque`)

**Legacy WARP (уже в панели):**

- Регистрация: **`service/warp.go`** → Cloudflare API `v0a2158`, результат в **`ep.Ext`**: `access_token`, `device_id`, `license_key`; в **`ep.Options`** — поля wireguard (`private_key`, `address`, `peers`, `listen_port: 0`, …).
- В **`model.Endpoint.MarshalJSON`** для `type == "warp"` наружу уходит **`type: "wireguard"`**; **`ext` в sing-box JSON не попадает** — токены остаются в БД.
- **`Save`** для `warp` не выставляет `NeedsReload` так же, как для WG-сервера (см. ветки в **`service/endpoints.go`**).
- Подписка WG: **`sub/wg_conf_service.go`** и патчи ориентируются на **`type = wireguard`** и условия вроде **`listen_port > 0`** — **legacy WARP в типичный WG-server subscription path не попадает**.

**MASQUE WARP (`warp_masque`):**

- В **`vendor/s-ui`** сейчас **нет** `masque` / `warp_masque` в подписках и типах ссылок.
- В ядре это **отдельный** endpoint с bootstrap MASQUE, state, профилем Cloudflare и т.д. (см. **`endpoint_warp_masque.go`**, **`MASQUE-SINGBOX-CONFIG.md`**).
- **Задание для реализации:** явно описать продуктово: где после «регистрации» лежит state (колонка `ext`, отдельный файл на хосте только в ядре, поле `warp_masque_state_path`, синхронизация с панелью), **нужна ли** в панели повторная логика `v0a2158` или достаточно **триггера/секретов** с делегированием ядру, и **что именно** отдаётся в подписке (сырой JSON клиента `warp_masque`, обрезанный профиль, ссылка на внешний конфиг).

---

## 6. Подписка и выбор способа аутентификации

- **Сервер MASQUE:** `server_auth` в опциях endpoint (см. доку). **Клиент:** `server_token`, `client_basic_*`, `client_tls_*` (в т.ч. inline PEM в JSON — как в клиентском лаб-конфиге hiddify-app).
- **Выбор auth в UI endpoint’а** должен:
  - влиять на **сохранённые** поля `server_auth` / TLS / политику;
  - влиять на **генерацию подписки**: какие поля подставляются из **`Client.config["masque"]`** (или выбранного ключа) при мерже в итоговый JSON (паттерн **`getOutbounds`** в **`jsonService.go`**: общий merge по имени типа + особые ветки как для `shadowsocks`).
- Если нужен **отдельный URL-формат** (аналог `json-wg`): новый handler в **`sub/subHandler.go`** + метод на сервисе + документация пути в UI «копировать ссылку».

---

## 7. UI: модалка клиента и модалка endpoint’а

- **Клиент (`Client.vue`):** вкладки, `clientConfig`, для WG — отдельные computed на `clientConfig.wireguard.*`. Для MASQUE — **либо** тот же стиль, **либо** вложенный объект под ключом протокола + отображение секретов в одном месте с остальными протоколами.
- **Endpoint (`Endpoint.vue`):** `EpTypes`, `createEndpoint`, `v-if` по типу; для MASQUE — новый блок + при необходимости **селектор политики auth** с валидацией под sing-box.
- **Группы/клиенты на endpoint:** переиспользовать **`GroupMultiSelect`**, `member_client_ids`, как в **Wireguard** / **L3Router**.

---

## 8. Критерии готовности (черновик)

- [ ] В БД и UI существуют типы **`masque`** (и **`warp_masque`** или согласованное имя), не ломая существующие endpoint’ы.
- [ ] **`MarshalJSON`** / **`GetAllConfig`** отдают в ядро валидный sing-box JSON (тесты на уровне `database/model` + smoke `sing-box check` при необходимости).
- [ ] Клиенты: в модалке видны и редактируются секреты MASQUE; сохраняются в согласованном месте (БД).
- [ ] Привязка клиентов/групп к endpoint согласована с одним из принятых в форке паттернов.
- [ ] Подписка: минимум **JSON**-выдача с рабочим клиентским `masque` / согласованное поведение для `warp_masque`; при необходимости Clash/текстовые ссылки.
- [ ] Документация в **`docs/masque/`** дополнена разделом «s-ui» только после реализации (в этой итерации **не** обязательно).

---

## 9. Локальные проверки (памятка после кода)

```powershell
Set-Location "c:\Users\qwerty\git\hiddify-app\vendor\s-ui"
go test ./... -count=1 -short
```

Фронт: `npm run build` / линтер в `frontend/` по правилам проекта.

---

## 10. Контекст от разведки (субагенты)

По репозиторию уже пройдены зоны: **модели БД и миграции**, **подписка (`sub/` + `util/`)**, **фронт (`Client.vue`, `Endpoint.vue`, протоколы, i18n)**, **API (`Save` / `LoadPartialData`, сервисы)**, **legacy WARP vs `warp_masque`**. Итоговые выводы **сведены в разделы §2–§7**; детальные цитаты файлов при реализации снова смотреть в **`vendor/s-ui/`** и в **`docs/masque/`**.
