# AGENTS — MASQUE dataplane: HTTP/2-слой (RFC), конфигурация и fallback

## 1. Задача (текущий фокус)

Добавить **второй внешний транспортный слой** MASQUE для клиентских эндпоинтов **`type: masque`** и **`warp_masque`**: помимо уже реализованного пути **HTTP/3 поверх QUIC (UDP)** ввести нормативный путь **HTTP/2 поверх TLS/TCP (`ALPN: h2`)** с теми же семантиками MASQUE (Extended CONNECT + HTTP Datagrams по RFC 9297, в т.ч. капсулы на потоке), на **одном архитектурном уровне**, что действующий H3/QUIC-слой.

**Инвариант интеграции:** расширять существующие [`transport/masque`](hiddify-core/hiddify-sing-box/transport/masque), [`protocol/masque`](hiddify-core/hiddify-sing-box/protocol/masque), [`option/masque.go`](hiddify-core/hiddify-sing-box/option/masque.go), интерфейсы `ClientSession` / `ListenPacket` / `OpenIPSession` / `DialContext`, `common/masque.Runtime` и фабрику клиента — **без** отдельного «второго эндпоинта сбоку» и без дублирования политики маршрутизации.

**Статус:** на момент фиксации этого файла **реализация H2-слоя в дереве не завершена** (поля `http_layer` в JSON и отдельные файлы вроде `http_layer_fallback.go` / `h2_udp_client.go` в ТЗ ниже — **целевые артефакты**, а не факт текущего репозитория). Код писать **только после явного сигнала** к реализации.

Точки WARP: [`endpoint_warp_masque.go`](hiddify-core/hiddify-sing-box/protocol/masque/endpoint_warp_masque.go), [`warp_control_adapter.go`](hiddify-core/hiddify-sing-box/protocol/masque/warp_control_adapter.go).

---

## 2. Нормативная база (RFC)

Кратко (детали — в текстах RFC):

| Тема | Документ | Следствие для H2 |
|------|-----------|------------------|
| Extended CONNECT на HTTP/2 | [RFC 8441](https://www.rfc-editor.org/rfc/rfc8441.html) | `SETTINGS_ENABLE_CONNECT_PROTOCOL`, псевдо-заголовки CONNECT, `:protocol` для `connect-udp` / `connect-ip` |
| Extended CONNECT на HTTP/3 | [RFC 9220](https://www.rfc-editor.org/rfc/rfc9220.html) | Параллельная семантика на H3 (текущий эталон в коде) |
| QUIC DATAGRAM | [RFC 9221](https://www.rfc-editor.org/rfc/rfc9221.html) | **Только транспорт QUIC**; на чистом H2 **не используется** |
| HTTP Datagrams, Capsule Protocol | [RFC 9297](https://www.rfc-editor.org/rfc/rfc9297.html) | На H2: демультиплексирование по **потоку**, в т.ч. **DATAGRAM capsules**; нет «ненадёжного» кадра как у QUIC |
| CONNECT-UDP | [RFC 9298](https://www.rfc-editor.org/rfc/rfc9298.html) | H2 и H3 — нормативные носители; для внешнего TCP рекомендуют H3+QUIC по перформансу, **но H2 не запрещён** |
| CONNECT-IP | [RFC 9484](https://www.rfc-editor.org/rfc/rfc9484.html) | Аналогично: Extended CONNECT + капсулы; учёт MTU/PMTU и отличий доставки от QUIC datagram |

**Ключевое различие H2 vs H3:** на H2 весь «датаплейн» после CONNECT идёт через **надёжный** TCP и flow-control HTTP/2; UDP/IP-семантика сохраняется на уровне **HTTP Datagram payload** и капсул, но **физические** задержки, head-of-line blocking и CC **другие**, чем у H3+QUIC DATAGRAM.

---

## 3. Cloudflare / WARP (границы продукта)

- В **changelog** релизов вроде **2025.4.929.0** (Linux/Windows/macOS GA) Cloudflare указывает **TCP fallback для туннеля MASQUE** и переход на **HTTP/2** при блокировке **HTTP/3** / UDP в части сетей. Это **мотивация** для H2-слоя в стороннем клиенте.
- Публичного контракта уровня «на H2 всегда доступны те же `:protocol` / `cf-connect-ip`, что на H3» **нет**; матрица методов, ALPN, портов и совместимость с **CONNECT-IP** на H2-path требуют **эмпирической проверки** на живом edge **без утечки секретов** в артефактах.
- Ссылки для ориентира: [changelog WARP Linux GA](https://developers.cloudflare.com/changelog/2025-05-12-warp-linux-ga/), пост [Zero Trust WARP MASQUE](https://blog.cloudflare.com/zero-trust-warp-with-a-masque) (в основном про H3; fallback в посте не детализирован).

---

## 4. Стек Go: библиотеки и слойность

**Фактическое ядро H3-пути сейчас:** `quic-go` + `http3`, форки [`third_party/masque-go`](hiddify-core/hiddify-sing-box/third_party/masque-go), [`third_party/connect-ip-go`](hiddify-core/hiddify-sing-box/third_party/connect-ip-go), при необходимости патчи quic-go. Оба форка **заточены под `http3.ClientConn`** и проверки SETTINGS Extended CONNECT / Datagrams на **HTTP/3**.

**Целевой стек H2 (рекомендация ТЗ):**

1. **TCP dial** и **TLS** через существующие хуки sing-box (SNI, pin, mTLS для WARP — отдельно от `NextProtos: h3`).
2. **`ALPN: h2`** (не смешивать с одной конфигурацией «только h3» для QUIC).
3. **`golang.org/x/net/http2`** (Transport / ClientConn) с поддержкой **RFC 8441** на клиенте; при ограничениях high-level API — контролируемый риск **низкоуровневого** фреймера (явно заложить в фазу исследования/prototype).
4. Поверх одного или пула соединений — **Extended CONNECT** с `:protocol` = `connect-udp` / `connect-ip` (и совместимыми расширениями профиля), заголовок согласования **Capsule-Protocol** по RFC 9297.
5. **Общий** разбор/сборка **капсул RFC 9297** по возможности **вынести** из http3-специфичных путей, чтобы не дублировать логику контекстов.

**Отдельная библиотека «masque-h2-go» в экосистеме** как готовый аналог masque-go **не обязательна**; приоритет — **минимальные зависимости**, совместимость с семантикой sing-box и тестируемость.

---

## 5. Встраивание в архитектуру (без параллельного endpoint)

Опорные абстракции:

- `transport/masque.ClientSession` / `ClientFactory` — внешний контракт сессии.
- `CoreClientFactory.NewSession` → `coreSession` с шаблонами URI и сегодняшними полями **`qmasque.Client`**, **`http3.Transport` / `http3.ClientConn`** для UDP/IP/TCP по веткам.

**Принцип:** не вводить новый `type` эндпоинта в JSON; политика слоя (`h2` / `h3` / `auto`) и fallback живут в **опциях** и в **фабрике/обёртке** (`clientFactoryWithHTTPLayer` *или* ветвление внутри `CoreClientFactory`), с **одним** публичным `ClientSession`.

**Паритет по возможностям:** `CapabilitySet` должен **честно** отражать отсутствие QUIC DATAGRAM на H2 (и любые отличия, влияющие на `connect_ip` / размеры PDU).

**CONNECT-IP на H2 — самый жёсткий узел:** текущий `connect-ip-go` использует **`ReceiveDatagram` / HTTP/3 datagram API** там, где QUIC-путь отделён от капсул на stream; для H2 требуется **эквивалентная абстракция** «tunnel + datagram delivery» с доставкой **через капсулы** (и совместимостью с остальной логикой `transport/masque`).

Узловые файлы для чтения при реализации: [`transport/masque/transport.go`](hiddify-core/hiddify-sing-box/transport/masque/transport.go), [`protocol/masque/quic_dialer.go`](hiddify-core/hiddify-sing-box/protocol/masque/quic_dialer.go), [`common/masque/runtime.go`](hiddify-core/hiddify-sing-box/common/masque/runtime.go).

---

## 6. Конфигурация sing-box (целевой контракт JSON)

Клиентские поля (расширение `MasqueEndpointOptions`; **сервер — отклонять** непустые `http_layer*`, как и прочие client-only поля):

| Поле | Тип | Default | Смысл |
|------|-----|---------|--------|
| `http_layer` | string | `h3` | `h3` — только QUIC/HTTP3; `h2` — TLS+TCP+HTTP/2; `auto` — политика старта (см. ниже) |
| `http_layer_fallback` | bool | `false` | После ошибки классифицированной как «переключаемая» — **одна** попытка альтернативного слоя **H2↔H3** (не путать с `fallback_policy` / TCP direct) |
| `http_layer_cache_ttl` | duration | например `5m` | TTL **in-memory** кэша успешно выбранного слоя; имеет смысл при `http_layer_fallback: true` и/или `http_layer: auto` |

**Семантика `auto` (рекомендация ТЗ):** при старте — порядок **H3 затем H2** (быстрый путь по умолчанию индустрии + fallback на TCP); при включённом `http_layer_fallback` — согласовать с кэшем (после успеха фиксировать слой на время TTL).

**Отличие от существующих полей:** `fallback_policy` / `tcp_mode` / `tcp_transport` относятся к **исходящему TCP** (MASQUE vs direct), **не** к обмену H2/H3 на внешнем MASQUE-туннеле.

---

## 7. Валидация и конфликтующие настройки

Место: [`protocol/masque/endpoint.go`](hiddify-core/hiddify-sing-box/protocol/masque/endpoint.go) (`validateMasqueOptions`, `validateMasqueServerOptions`), плюс при необходимости [`endpoint_warp_masque.go`](hiddify-core/hiddify-sing-box/protocol/masque/endpoint_warp_masque.go).

**Обязательные правила (ТЗ):**

1. Нормализация `http_layer` (пустое → `h3` или явная ошибка — выбрать одно и зафиксировать в коде и доке).
2. **`http_layer: h2`** и **`quic_experimental.enabled`** — **несовместимы** (QUIC-тюнинг без QUIC бессмысленен); **ошибка конфигурации**.
3. Пока **CONNECT-IP over H2** не реализован как инвариант — при сочетании `transport_mode: connect_ip` и **только** `http_layer: h2` — **ошибка** или жёсткое предупреждение с запретом старта (предпочтительно **ошибка**, чтобы не вводить в заблуждение).
4. Пока **CONNECT-stream (template_tcp) over H2** не завершён — при `tcp_transport: connect_stream` и **`http_layer: h2`** без допустимого обхода — **ошибка** или явная документированная блокировка (временно разрешить `h3` / `auto` с fallback).
5. `http_layer_cache_ttl`: при `http_layer_fallback: false` и **не** `auto` — допускается игнорирование или предупреждение «не используется» (зафиксировать одно поведение).
6. Сервер: любые непустые `http_layer*` → ошибка.

Тесты: расширить [`endpoint_validation_test.go`](hiddify-core/hiddify-sing-box/protocol/masque/endpoint_validation_test.go) и при необходимости [`endpoint_test.go`](hiddify-core/hiddify-sing-box/protocol/masque/endpoint_test.go).

---

## 8. Fallback и in-memory кэш (рантайм)

**Требования:**

- **`http_layer_fallback` по умолчанию выключен** — пользователь явно включает попытку второго слоя.
- Логика **простая и предсказуемая:** после неуспеха выбранного слоя с ошибками, классифицированными как «transport/handshake/Extended CONNECT / TLS / HTTP settings» (список категорий — зафиксировать в коде и логах), выполнить **ограниченное** число попыток альтернативы (рекомендация: **одна** смена слоя на «сессию подъёма», без бесконечных циклов).
- **Кэш:** ключ из **хост, порт, SNI, hop tag, политика auto/fallback** (без секретов); значение — выбранный рабочий слой `h2` | `h3`; **только память процесса**, **TTL** = `http_layer_cache_ttl`.
- Поведение при истечении TTL: повторная деградация как при холодном старте (с учётом `auto`).
- Логи: структурированные метки уровня **`masque_http_layer_attempt`**, **`masque_http_layer_chosen`**, **`masque_http_layer_fallback`** — **без** токенов и ключей.

**Паттерн:** кэш и политика — в `protocol/masque` (отдельный небольшой модуль) или в обёртке фабрики; избегать глобальных синглтонов без привязки к жизненному циклу эндпоинта/процесса.

---

## 9. Паритет функциональности (целевое состояние)

| Плоскость | H3 (текущая) | H2 (цель) |
|-----------|----------------|-----------|
| CONNECT-UDP | `masque-go` / qmasque | Новый клиент: Extended CONNECT + капсулы |
| CONNECT-stream (TCP) | `http3.Transport` RoundTrip | Extended CONNECT stream по H2 (RFC 8441) |
| CONNECT-IP (+ UDP bridge / netstack TCP) | `connect-ip-go` + HTTP/3 datagram + stream | Тот же **логический** контракт IP/datagram, **другая** доставка PDU (капсулы) |

Реализацию вести **фазами** (см. §10); не объявлять фичу «готовой», пока не выполнены критерии фазы и нет регрессий H3.

---

## 10. Фазы внедрения и Definition of Done

| Фаза | Содержание | Критерий готовности |
|------|------------|---------------------|
| **A** | Поля JSON + нормализация + валидация + тесты опций | `validateMasqueOptions` / сервер / WARP; тесты на конфликты |
| **B** | Рантайм: выбор слоя, **fallback** (опционально), **TTL-кэш** | Юнит-тесты политики; логи без секретов |
| **C** | **CONNECT-UDP** end-to-end на H2 | Стенд / интеграционный тест; сравнение с H3 на том же сценарии |
| **D** | **CONNECT-stream** (template_tcp) на H2 | TCP через MASQUE на H2 без регрессии H3 |
| **E** | **CONNECT-IP** на H2 (в т.ч. мост UDP и согласованность с `transport/masque`) | Отдельный DoD: MTU, капсулы, отсутствие зависимости от `ReceiveDatagram` там, где путь H2 |

**Документация пользователя:** обновить [`docs/masque/MASQUE-SINGBOX-CONFIG.md`](docs/masque/MASQUE-SINGBOX-CONFIG.md) при появлении полей в коде.

**Объёмный дизайн** при необходимости вынести в отдельный файл (например `docs/masque/H2-DATAPLANE-DESIGN.md`) со ссылкой из этого раздела.

---

## 11. История: live QUIC dataplane WARP (справочно)

Ранее фокусом был разрыв **H3/QUIC** на эталонном VPS и стенд Docker; чеклисты по портам, UDP path, буферам и `ClassifyMasqueFailure` остаются релевантными **ветке H3**. Операционка: [`README-warp-masque-live-server.md`](experiments/router/stand/l3router/README-warp-masque-live-server.md).

---

## 12. Инварианты процесса

- Цикл: дизайн → код → стенд / тест → артефакт **без секретов**.
- Изменения ядра — в **`hiddify-core`**; при необходимости bump сабмодуля в приложении.
- Источники истины по коду: `protocol/masque/*`, `transport/masque`, `option/masque.go`; затем [`hiddify-core/docs/masque-warp-architecture.md`](hiddify-core/docs/masque-warp-architecture.md), [`IDEAL-MASQUE-ARCHITECTURE.md`](IDEAL-MASQUE-ARCHITECTURE.md), [`docs/masque/MASQUE-SINGBOX-CONFIG.md`](docs/masque/MASQUE-SINGBOX-CONFIG.md), этот файл.

---

## 13. Текущая итерация (перезаписывать)

| Поле | Значение |
|------|----------|
| Дата | 2026-05-10 |
| Фокус | ТЗ: MASQUE **HTTP/2 dataplane** (RFC 8441 + RFC 9297…), конфиг **`http_layer`**, **`http_layer_fallback`**, **`http_layer_cache_ttl`**, валидация, фазирование |
| Следующий шаг | Реализация **только после явного разрешения**; старт с фазы **A** (опции + валидация), затем **C** (CONNECT-UDP H2) |

**Контекст исследования:** уточнение RFC, публичных формулировок Cloudflare, точек встраивания в `transport/masque` и валидатора выполнено с привлечением вспомогательного обзора кода/доков (в т.ч. по `third_party/masque-go` / `connect-ip-go`).

