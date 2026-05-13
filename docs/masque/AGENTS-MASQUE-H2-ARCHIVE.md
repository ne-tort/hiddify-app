# Архив: MASQUE H2 / RFC / история итераций

Снимок прежнего корневого [`AGENTS.md`](../../AGENTS.md) до переписи под handoff live-WARP (2026-05).  
**Актуальный цикл разработки, деплой и команды:** корень репозитория `hiddify-app` → `AGENTS.md`.

---

# AGENTS — MASQUE dataplane: HTTP/2-слой (RFC), конфигурация и fallback

## 1. Задача (текущий фокус)

Добавить **второй внешний транспортный слой** MASQUE для клиентских эндпоинтов **`type: masque`** и **`warp_masque`**: помимо уже реализованного пути **HTTP/3 поверх QUIC (UDP)** ввести нормативный путь **HTTP/2 поверх TLS/TCP (`ALPN: h2`)** с теми же семантиками MASQUE (Extended CONNECT + HTTP Datagrams по RFC 9297, в т.ч. капсулы на потоке), на **одном архитектурном уровне**, что действующий H3/QUIC-слой.

**Инвариант интеграции:** расширять существующие [`transport/masque`](hiddify-core/hiddify-sing-box/transport/masque), [`protocol/masque`](hiddify-core/hiddify-sing-box/protocol/masque), [`option/masque.go`](hiddify-core/hiddify-sing-box/option/masque.go), интерфейсы `ClientSession` / `ListenPacket` / `OpenIPSession` / `DialContext`, `common/masque.Runtime` и фабрику клиента — **без** отдельного «второго эндпоинта сбоку» и без дублирования политики маршрутизации.

**Статус реализации:** в `hiddify-sing-box` добавлены клиентские поля `http_layer` / `http_layer_fallback` / `http_layer_cache_ttl`, валидация, TTL-кэш выбора слоя, **CONNECT-UDP по HTTP/2**, **CONNECT-stream (`template_tcp`) по HTTP/2**, **CONNECT-IP по HTTP/2** (Extended CONNECT; IP-плоскость через DATAGRAM capsules RFC 9297 на CONNECT-потоке — без QUIC `ReceiveDatagram`), опциональный **H2↔H3 fallback** на CONNECT-UDP, интеграция в `transport/masque`, форк `third_party/connect-ip-go` (`DialHTTP2`), `CoreClientFactory`, `common/masque.Runtime`.

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

**CONNECT-IP на H2:** в форке `connect-ip-go` добавлены `DialHTTP2` и ветка `Conn` с очередью датаграмм из **DATAGRAM capsules** на CONNECT-потоке (вместо QUIC `ReceiveDatagram`); см. `client_http2.go` и `receiveProxiedDatagram` в `conn.go`.

Узловые файлы для чтения при реализации: [`transport/masque/transport.go`](hiddify-core/hiddify-sing-box/transport/masque/transport.go), [`protocol/masque/quic_dialer.go`](hiddify-core/hiddify-sing-box/protocol/masque/quic_dialer.go), [`common/masque/runtime.go`](hiddify-core/hiddify-sing-box/common/masque/runtime.go).

---

## 6. Конфигурация sing-box (целевой контракт JSON)

Клиентские поля (расширение `MasqueEndpointOptions`; **сервер — отклонять** непустые `http_layer*`, как и прочие client-only поля):

| Поле | Тип | Default | Смысл |
|------|-----|---------|--------|
| `http_layer` | string | `h3` | `h3` — только QUIC/HTTP3; `h2` — TLS+TCP+HTTP/2; `auto` — политика старта (см. ниже) |
| `http_layer_fallback` | bool | `false` | После ошибки классифицированной как «переключаемая» — **одна** попытка альтернативного слоя **H2↔H3** (не путать с `fallback_policy` / TCP direct) |
| `http_layer_cache_ttl` | duration | например `5m` | TTL **in-memory** кэша успешно выбранного слоя; **поле действует только при `http_layer: auto`** (положительный TTL при явном `h2`/`h3` — ошибка валидации); fallback при `auto` опционален |

**Семантика `auto` (рекомендация ТЗ):** при старте — порядок **H3 затем H2** (быстрый путь по умолчанию индустрии + fallback на TCP); при включённом `http_layer_fallback` — согласовать с кэшем (после успеха фиксировать слой на время TTL).

**Отличие от существующих полей:** `fallback_policy` / `tcp_mode` / `tcp_transport` относятся к **исходящему TCP** (MASQUE vs direct), **не** к обмену H2/H3 на внешнем MASQUE-туннеле.

---

## 7. Валидация и конфликтующие настройки

Место: [`protocol/masque/endpoint.go`](hiddify-core/hiddify-sing-box/protocol/masque/endpoint.go) (`validateMasqueOptions`, `validateMasqueServerOptions`), плюс при необходимости [`endpoint_warp_masque.go`](hiddify-core/hiddify-sing-box/protocol/masque/endpoint_warp_masque.go).

**Обязательные правила (ТЗ):**

1. Нормализация `http_layer` (пустое → `h3` или явная ошибка — выбрать одно и зафиксировать в коде и доке).
2. **`http_layer: h2`** и **`quic_experimental.enabled`** — **несовместимы** (QUIC-тюнинг без QUIC бессмысленен); **ошибка конфигурации**.
3. `transport_mode: connect_ip` с **`http_layer: h2`** **разрешён** (CONNECT-IP по H2 через капсулы RFC 9297); при несовместимости живого сервера — ошибка рукопожатия, без ложной отбраковки конфига только из-за режима.
4. `tcp_transport: connect_stream` с **`http_layer: h2`** разрешён: идёт тот же `template_tcp` / CONNECT-stream, но внешний транспорт — TLS+H2 Extended CONNECT (`golang.org/x/net/http2`); слой синхронизован с состоянием выбора H2/H3 для CONNECT-UDP (`currentUDPHTTPLayer`, в т.ч. после однократного fallback).
5. **`http_layer_cache_ttl`:** учитывается **только** при `http_layer: auto`; иное сочетание с положительным TTL — **ошибка конфигурации** (раньше значение молча игнорировалось).
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
| CONNECT-IP (+ UDP bridge / netstack TCP) | `connect-ip-go` + HTTP/3 datagram + stream | `DialHTTP2`: IP PDU в DATAGRAM capsules по потоку (RFC 9297); логический контракт сохранён |

Фазы см. ниже (§10); при правках кодовых путей не допускать регрессии H3.

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

**Объёмный дизайн** и точки расширения (CONNECT-stream / CONNECT-IP по H2): [`docs/masque/H2-DATAPLANE-DESIGN.md`](docs/masque/H2-DATAPLANE-DESIGN.md).

---

## 11. История: live QUIC dataplane WARP (справочно)

Ранее фокусом был разрыв **H3/QUIC** на эталонном VPS и стенд Docker; чеклисты по портам, UDP path, буферам и `ClassifyMasqueFailure` остаются релевантными **ветке H3**. Операционка: [`README-warp-masque-live-server.md`](experiments/router/stand/l3router/README-warp-masque-live-server.md).

---

## 12. Инварианты процесса

- Цикл: дизайн → код → стенд / тест → артефакт **без секретов**.
- Изменения ядра — в **`hiddify-core`**; при необходимости bump сабмодуля в приложении.
- Источники истины по коду: `protocol/masque/*`, `transport/masque`, `option/masque.go`; затем [`hiddify-core/docs/masque-warp-architecture.md`](hiddify-core/docs/masque-warp-architecture.md), [`IDEAL-MASQUE-ARCHITECTURE.md`](IDEAL-MASQUE-ARCHITECTURE.md), [`docs/masque/MASQUE-SINGBOX-CONFIG.md`](docs/masque/MASQUE-SINGBOX-CONFIG.md), этот файл.
- **Локальные `go test` MASQUE на Windows:** если в окружении застряли **`GOOS=linux`** / **`GOARCH=amd64`** после кросс-сборки, бинарник тестов не запускается (`%1 is not a valid Win32 application`). Сбросить: `Remove-Item Env:GOOS, Env:GOARCH` или использовать [`hiddify-core/scripts/go-test-masque.ps1`](hiddify-core/scripts/go-test-masque.ps1). Альтернатива: WSL — [`hiddify-core/scripts/go-test-masque-wsl.sh`](hiddify-core/scripts/go-test-masque-wsl.sh).

---

## 13. Текущая итерация (перезаписывать)

| Поле | Значение |
|------|----------|
| Дата | 2026-05-11 |
| Фокус | Live-разрыв `warp_masque` CONNECT-IP vs usque на одном VPS: после `CONNECT 200` нет `ADDRESS_ASSIGN`/`packet_rx`, трафик не проходит. |

**Чеклист текущей итерации:**  
- Закрыть диагностический разрыв «edge не шлёт `ADDRESS_ASSIGN`» vs «клиент теряет control/datagram path».  
- Довести live-проверку на VPS до критерия `warp=on` через TUN-smoke (без SOCKS как primary check).  
- Зафиксировать минимально-воспроизводимый стендовый сценарий в раннере/операционке без секретов.

**Обязательный способ деплоя (текущий контур):**  
- Любая правка по **`transport/masque`** / **`third_party/connect-ip-go`** засчитывается только после полного цикла на VPS **`163.5.180.181`**: rebuild `sing-box` (linux/amd64, `with_masque` теги) → upload артефакта → `docker compose build` → `up -d --force-recreate`.  
- Запуск только через стенд **`experiments/router/stand/l3router`** и конфиг **`warp-masque-live.server.local.json`**; без секретов в отчётах.

**Обязательный способ тестирования / критерии успеха:**  
- **Только путь через TUN sing-box:** smoke выполняется из контейнера **без** `--proxy socks5h://…`; inbound `socks-in` в стенде — вспомогательный, **не** считать успехом деплоя проверку через SOCKS.  
- **Цель должна попадать в `route_address` TUN** (шаблоны стенда: `198.18.0.0/15`, `1.1.1.0/24`, `1.0.0.0/24`). Канонический запрос: `curl -sS --max-time 45 https://1.1.1.1/cdn-cgi/trace` (или `https://1.0.0.1/…`) с **литералом IP**, чтобы трафик гарантированно шёл в `tun-warp-test`, а не мимо TUN. Произвольный hostname + DNS с `detour: direct` может резолвиться **вне** этих префиксов — такой `curl` **не** валидирует dataplane через TUN.  
- Перед `curl`: `ip route get 1.1.1.1` (или выбранный адрес из `route_address`) — в выводе должен быть **`dev tun-warp-test`** (или фактическое `interface_name` из конфига).  
- Контрольные логи: `CONNECT status=200`, bootstrap (`RequestAddresses`/`AdvertiseRoute`), `assigned prefix count`, отсутствие `startup in progress/failed` на steady-state.  
- Критерий DoD live: **`warp=on`** именно на **TUN-smoke** выше и рост `connect_ip_packet_rx_total`/рабочий TX-RX dataplane. `open_ip_session_success` без RX не считается успехом. Рабочий **usque** на том же VPS — лишь внешний контроль «хост/сеть живы», не замена TUN-проверки sing-box.

**Последняя правка кода / аудит этой итерации:**  
- Добавлены неблокирующие пути доставки HTTP DATAGRAM в **`third_party/connect-ip-go/conn.go`** (stream+QUIC pump), чтобы чтение control capsules не блокировалось полными очередями ingress.  
- Вынесен цикл ожидания непустого `ADDRESS_ASSIGN` в **`transport/masque/connect_ip_prefix_wait.go`** и подключён в bootstrap/`netstack_adapter`.  
- Bootstrap CONNECT-IP переведён на scoped `RequestAddresses` (`0.0.0.0/0`, `::/0`), добавлен env-gate **`MASQUE_CONNECT_IP_BOOTSTRAP_REQUIRE_PREFIX`** для live-диагностики strict/non-strict.  
- Локальные прогоны: `go test` по **`github.com/sagernet/sing-box/transport/masque/...`** и **`github.com/quic-go/connect-ip-go/...`** — OK.  
- Live на VPS: `usque` SOCKS даёт `warp=on`; `sing-box` стабильно получает `CONNECT 200` и bootstrap, но в strict-режиме не видит `assigned prefixes`, в non-strict возможен `open_ip_session_success` без RX-трафика.

**Предыдущая запись (§13):** **`masque_http_layer_attempt`** для **CONNECT-UDP H2** в **`dialUDPAddr`** (**[`transport.go`](hiddify-core/hiddify-sing-box/transport/masque/transport.go)**) сразу после валидации шаблона (как для H3), чтобы логировался и путь с **`h2UDPConnectHook`**. В **`dialUDPOverHTTP2`** — вычисление **`dialAddr`** для **`h2ConnectUDPPacketConn.localAddr`**.

**Ранее (§13 — relay-контекст H2):** устранено **дублирование** relay-контекста Extended CONNECT H2: **`connectip.NewH2ExtendedConnectRequestContext`** в [`third_party/connect-ip-go/client_http2.go`](hiddify-core/hiddify-sing-box/third_party/connect-ip-go/client_http2.go) (экспорт), вызовы из **`dialUDPOverHTTP2`** и **`dialTCPStreamH2`** (+ тестовый **`h2ConnectRequestContextFactory`**); удалены локальные **`transport/masque/h2_request_context*.go`**. Источник истины один — меньше риска расхождения с **`DialHTTP2`**. Прогоны: **`go test ./transport/masque/... ./protocol/masque/... ./common/masque/...`**, **`go test .`** в **`third_party/connect-ip-go`**, **`staticcheck ./transport/masque/...`**.

**Ранее (§13 — `h2UDPUploadWriter.Write`):** при ошибке записи в тело CONNECT-UDP возвращалось **`(0, err)`** при возможном **`n > 0`** у нижнего **`io.Writer`**; исправление контракта **`io.Writer`** + **`TestH2UDPUploadWriterPropagatesPartialWriteOnError`**.

**Ранее (§13 — CONNECT-UDP HTTP/2 uplink `WriteTo`):** **`h2ConnectUDPPacketConn.WriteTo`** использует **`writeUDPPayloadAsH2DatagramCapsules`** + **`h2UDPUploadWriter`**; **`masqueUDPDatagramSplitConn.WriteTo`** для **H2** ограничивает **`maxPayload`** **`h2ConnectUDPMaxUDPPayloadPerDatagramCapsule`**. Юниты: **`TestH2ConnectUDPPacketConnWriteToSplitsLargePayloadIntoRFC9297Capsules`**, **`TestMasqueUDPDatagramSplitConnH2CapsTunnelChunkSize`**.

**Ранее (§13 — сервер downlink):** **`ServeH2ConnectUDP`**: буфер UDP **`h2ConnectUDPServerUDPReadBuf` (65535)**, downlink — **`writeUDPPayloadAsH2DatagramCapsules`**. Юниты: **`TestWriteUDPPayloadAsH2DatagramCapsulesEmpty`**, **`TestWriteUDPPayloadAsH2DatagramCapsulesSplitsLargePayload`**.

**Ранее (§13 — flushWriter CONNECT-stream):** **`relayTCPBidirectional`** в [`endpoint_server.go`](hiddify-core/hiddify-sing-box/protocol/masque/endpoint_server.go): **`flushWriter`** всегда оборачивает ответ (**`Flush`** только при **`Flusher`**). Юниты: **`TestFlushWriterCompletesPartialWritesWithoutFlusher`**, **`TestRelayTCPBidirectionalDownloadUsesCompletingWriterWithoutFlusher`**.

**Ранее (та же тема §13 — `flushWriter` только с Flusher):** **`flushWriter`** (скачивание CONNECT-stream с сервера через **`relayTCPBidirectional`** / **`io.Copy`**): нижний **`ResponseWriter`** может вернуть **частичный** **`Write`** с **`nil`** error; цикл дозаписи как у **`writeAllIOWriter`** + **`Flush`** после каждого прогресса при наличии **`Flusher`**; базовый юнит **`TestFlushWriterCompletesPartialUnderlyingWrites`**.

**Ранее (та же итерация — writeAll H2 UDP/stream):** **`writeAllIOWriter`** (`transport/masque/h2_connect_udp.go`): дозапись в **`io.Writer`** по контракту. Подключено к **`h2ConnectUDPPacketConn.WriteTo`** / **`awaitH2UDPReqBodyWrite`**, серверному **`writeUDPH2ConnectDatagramCapsule`**, H2 CONNECT-stream upload (**`streamConn`**: **`writeH2ExtendedConnectPipe`**, **`awaitH2PipeWriterBlockedWriteInterruptible`**). Юнит: **`TestWriteAllIOWriterCompletesPartialWrites`**.

**Ранее (§13 — split conn `PacketConn`):** **`masqueUDPDatagramSplitConn.WriteTo`:** после нарезки на фрагменты ≤ **`maxPayload`** каждый фрагмент дозаписывается **внутренним** циклом, пока нижний **`PacketConn.WriteTo`** не примет все байты среза `p[pos:end]` (или не вернёт ошибку). Раньше при гипотетическом **частичном** успехе **`n < len(fragment)`** следующая итерация начинала **следующий** фрагмент со смещённой границей и ломала выравнивание MASQUE datagram chunks. Юнит: **`TestMasqueUDPDatagramSplitConnWriteToCompletesPartialFragmentWrites`** (`cappedWritePacketConn`).

**Ранее (§13 — `maxPayload` ≤ 0 и срезы):** **`masqueUDPDatagramSplitConn.WriteTo`:** при **`maxPayload <= 0`** делается **один** вызов нижнего **`WriteTo`** без нарезки. Юнит: **`TestMasqueUDPDatagramSplitConnWriteToNonPositiveMaxPayloadNoSplit`**.

**Ранее (§13 — регрессия пустого UDP):** **`TestMasqueUDPDatagramSplitConnWriteToEmpty`**: CONNECT-UDP через **`masqueUDPDatagramSplitConn`** при **`len(p)==0`** обязан один раз вызвать нижний **`PacketConn.WriteTo`** с пустым слайсом (паритет с **H3 `proxiedConn`**, **H2 `h2ConnectUDPPacketConn`**, UDP-мостом CONNECT-IP).

**Ранее (§13 — CONNECT-IP UDP-мост `WriteTo`):** при **`len(p)==0`** цикл не выполнялся → **`WritePacket`** не вызывался; исправлено на **`first || offset < len(p)`**. Юнит: **`TestConnectIPUDPPacketConnWriteToEmptySendsOnePacket`**.

**Ранее (§13 — классификатор fallback oversized):** **`IsMasqueHTTPLayerSwitchableFailure`** (`transport/masque/http_layer_fallback.go`): явное **`errors.Is(…, errMasqueH2ConnectUDPOversizedDeclared)`** — отклонение враждебных объявлений длины капсулы CONNECT-UDP по H2 не должно тратить бюджет **`http_layer_fallback`** даже при смене формулировки текста. Юнит: расширение **`TestIsMasqueHTTPLayerSwitchableFailure`**.

**Ранее (та же запись §13 — dialTCPStream / hop):** **`dialTCPStream`:** ошибки с **`errors.Is(…, ErrCapability)`** (невалидный **`Socksaddr`** из **`resolveDestinationHost`**, поломка **`templateTCP`**.Expand/Parse, «internal» uninitialized H3 transport в attempt) **не тратят** **`hopOrder`** и не гоняют **`http_layer_fallback`** churn — паритет с **`ListenPacket`**. Хелпер **`tcpConnectStreamErrMayBenefitFromNextHop`**, юнит **`TestDialTCPStreamDoesNotAdvanceHopOnLocalConfigError`**. Прогоны: **`go test ./transport/masque/... ./protocol/masque/... ./common/masque/...`**, **`go vet`**, **`staticcheck ./transport/masque/...`** — ок.

**Ранее (та же запись §13):** Прогоны **`third_party/connect-ip-go`** / **`third_party/masque-go`** (`go test .`). **`ClassifyMasqueFailure`:** ветка **`connect_http_auth`** выше **`masque h2:`** / **`masque connect-ip h2:`**; тест **`TestClassifyMasqueFailure`**.

**Ранее (§13, та же ось — H2 транспорт):** **`transport/masque` `ensureH2UDPTransport`:** **`http2.Transport`** с **`DisableCompression: true`**; юнит **`TestEnsureH2UDPTransportSetsDisableCompression`**.

**Ранее (§13 — masque-go H3 UDP):** **`third_party/masque-go` `Client.ensureConnected`:** **`http3.Transport`** для QUIC CONNECT-UDP с **`DisableCompression: true`** (раньше — только при **`LegacyH3Extras`**). Паритет с **`openHTTP3ClientConn`**.

**Ранее (§13, та же дата — HTTP/3 CONNECT-stream / `streamConn`):** в форке **`replace/quic-go-patched`** у **`Response.Body`** тип **`hijackableBody`** дополнен пробросом **`SetReadDeadline` → Stream`**; **`gzipReader`** пробрасывает дальше при gzip-ответе; юнит **`TestHijackableBodySetReadDeadlineForwardsToStream`** в **`http3/body_test.go`**.

**Ранее (§13, та же дата — CONNECT-stream H2):** **HTTP/2 CONNECT-stream (download / `resp.Body`):** тело ответа от **`x/net/http2`** (`transportResponseBody` и т.п.) **не** реализует **`SetReadDeadline`**, из‑за чего **`streamConn.SetReadDeadline`** на скачивании заканчивался на **`ErrDeadlineUnsupported`**, в отличие от аплоада (pipe + **`h2PipeWriteDL`**). Добавлены **`h2ConnectStreamResponseBody`**, **`newH2ConnectStreamResponseBody`**, **`awaitReadInterruptible`** (горутина + дедлайн + **`Close()`** тела при таймауте, как у **`h2ConnectUDPPacketConn`**). Юниты **`TestH2ConnectStreamResponseBodyReadDeadline*`**, **`TestStreamConnH2DownloadReadDeadlineThroughStreamConn`**.

**Ранее (§13, та же дата):** **`streamConn` (H2 CONNECT-stream):** для **`h2UploadPipe != nil`** запись идёт в **`io.PipeWriter`**, у которого нет **`SetWriteDeadline`**. Добавлены атомики **`h2PipeWriteDL connDeadlines`**, сериализация **`h2PipeWriteMu`**, сценарий **`awaitH2PipeWriterBlockedWriteInterruptible`** (горутина + **`context.WithDeadline`** + **`Close()`** сеанса при дедлайне, как **`h2ConnectUDPPacketConn.awaitH2UDPReqBodyWrite`). Ошибки завершаются через **`connectStreamFinishWriteError`**. Юниты **`TestStreamConnH2UploadWriteDeadlineElapsed`**, **`TestStreamConnH2UploadWriteDeadlineInterruptsBlockedPipeWrite`**.

**Ранее (§13):** **`h2ConnectUDPPacketConn.WriteTo`:** при установленном **`SetWriteDeadline`** (атомарное значение ≠ 0) запись в **`reqBody`** идёт через **`awaitH2UDPReqBodyWrite`** — горутина + **`context.WithDeadline`** + при срабатывании дедлайна **`Close()`** сеанса (как на **`ReadFrom`**), чтобы разблокировать залипший **`io.Pipe.Write`**. Без дедлайна — прежний синхронный **`Write`** без лишних аллокаций. Повторные проверки **`closed`/`writeTimeoutExceeded`** под **`writeMu`**. Юнит **`TestH2ConnectUDPPacketConnWriteDeadlineInterruptsBlockedBodyWrite`**.

**Ранее (§13):** **HTTP/2 CONNECT-UDP клиент (`h2ConnectUDPPacketConn`):** один **`context.WithDeadline`** на один вызов **`ReadFrom`** … Юниты **`TestH2ConnectUDP*`**, **`TestH2ConnectUDPPacketConnReadDeadlineInterruptsBlockedBodyRead`**; переименован **`TestH2ConnectUDPPacketConnConcurrentReadFromSerializesReads`**.

**Ранее (§13):** **`connectIPH2CapsulePipeCleanUploadTermination`** на **`h2ConnectUDPPacketConn.WriteTo`**; **`streamConn.Write`** и **`ErrClosedPipe`** при H2 upload — см. **`TestStreamConnWriteKeepsErrClosedPipeWhenDialCtxCanceledH2UploadPipe`**, **`TestDialTCPStreamInProcessHTTP3ProxyRelayPhase*`**.

**Ранее (§13):** **`third_party/connect-ip-go` CONNECT-IP HTTP/2:** **`connectIPH2CapsulePipeCleanUploadTermination`** на **`WritePacket`** / **`writeToStream`** / **`SendDatagram`**; без лишнего лога **`writing to stream failed`** на чистом **`EOF`/`ErrClosedPipe`**. Юниты: **`TestWritePacketFailures`**, **`TestSendCapsule*`** (HTTP/2).

**Ранее (§13):** **`h2ConnectUDPPacketConn.WriteTo`**: как **`ReadFrom`** на границе потока — при **`errors.Is(err, io.EOF)`** с **`reqBody.Write`** возврат **`io.EOF`** **без** **`Close()`** и без префикса **`masque h2 dataplane`**; **`TestH2ConnectUDPPacketConnWriteToCleanEOFWithoutClose`**. Паритет **`streamConn`** Read/Write: **`TestStreamConnWriteBlamesDialCtxWhenCanceledOnPeerError`** и смежные тесты.

**Ранее (§13):** **`transport/masque` `streamConn.Write`**: **`io.EOF`** до **`context.Cause(c.ctx)`** — паритет с **`Read`** и **`TestStreamConnWriteKeepsEOFWhenDialCtxCanceled`** (CONNECT-stream H3 при краткоживущем dial **`ctx`**).

**Ранее (§13):** **`third_party/connect-ip-go` `readFromStream` / горутина `newProxiedConn`** (`conn.go`): граничный **`io.EOF`** на разборе капсульного потока CONNECT-IP **не** оборачивается в **`wrapConnectIPStreamDataplaneErr`** (**паритет** с **`h2ConnectUDPPacketConn.ReadFrom`** и **`streamConn.Read`**): текст без префикса **`masque … dataplane`** и классификаторы **`IsMasqueHTTPLayerSwitchableFailure` / handshake** не цепляют нормальное закрытие тела ответа. В горутине чтения стрима лог **`handling stream failed`** не пишется на **`errors.Is(err, io.EOF)`**, teardown (**`CloseError`** / **`closeChan`**) без изменений. Юнит: **`TestReadFromStreamCapsuleBoundaryEOFWithoutDataplaneWrap`**. Прогоны: **`go test .`** в каталоге модуля **`third_party/connect-ip-go`**, **`go test ./transport/masque/... ./protocol/masque/...`**.

**Ранее (§13):** **`h2ConnectUDPPacketConn.ReadFrom`** (`transport/masque/h2_connect_udp.go`): распознавание **нормального** завершения тела CONNECT на **`errors.Is(err, io.EOF)`**, возврат **`err`** как есть (**паритет** с **`streamConn.Read`** и **`ServeH2ConnectUDP`**). Юнит: **`TestH2ConnectUDPPacketConnReadFromCleanEOFWrapped`**.

**Ранее (§13):** **`third_party/connect-ip-go` `parseConnectIPStreamCapsule`** — оба QUIC-varint префикса капсулы (тип и длина) читаются через **`capsuleCountingVarintReader`** (**паритет** с **`parseH2ConnectUDPCapsule`** / **`h2countingVarintReader`**): при усечённом varint на границе потока — **`io.ErrUnexpectedEOF`**, а не голый **`io.EOF`** при уже прочитанных байтах префикса; чистый конец потока до первого байта — по-прежнему **`io.EOF`**. Юниты: **`TestParseConnectIPStreamCapsuleCleanEOF`**, **`TestParseConnectIPStreamCapsuleTruncatedTypeVarint`**, **`TestParseConnectIPStreamCapsuleTruncatedLengthVarint`**. Прогоны: **`go test .`** в **`third_party/connect-ip-go`**, **`go test ./transport/masque/... ./protocol/masque/... ./common/masque/...`**, **`staticcheck ./transport/masque/...`**.

**Ранее (§13):** **`parseH2ConnectUDPCapsule`** — второй varint (длина капсулы) читается через тот же **`h2countingVarintReader`**, что и тип, вместо голого **`quicvarint.Read(r)`**: единые правила **`io.ErrUnexpectedEOF`** при усечённом втором varint и учёт байт на границе кадра (клиент **`h2ConnectUDPPacketConn`** и сервер **`ServeH2ConnectUDP`**). Полный **`Close()` под `readMu`/`writeMu` для H2 CONNECT-UDP не вводился: **`ReadFrom`** удерживает **`readMu`** на блокирующем чтении тела стрима — взаимная блокировка с **`Close`** (паритетно **`connectIPUDPPacketConn`**: **`Close`** не берёт **`readMu`**).

**Ранее (§13):** **`h2ConnectUDPPacketConn.ReadFrom`** — **`readMu`** для конкурентных **`ReadFrom`** (общий **`bufio.Reader`**); юнит **`TestH2ConnectUDPPacketConnConcurrentReadFromSerializesBufio`**, **`go test -race`** на этом сценарии.

**Ранее (§13):** форк **`third_party/masque-go`**, **`proxiedConn.SetReadDeadline`**: при установке **нового** будущего дедлайна после того, как момент **`oldDeadline` уже в прошлом** (`!now.Before(oldDeadline)`), но **`readCtx` уже отменён** таймером, раньше делался только **`Timer.Reset`** без смены контекста — **`ReadFrom`** входил в цикл **`ReceiveDatagram` → `Canceled` → `restart` → `goto start`** с тем же отменённым ctx (**busy-wait / timeout** в **`TestReadDeadline/extending_the_deadline`** при стресс-прогоне). Сейчас в этом случае выставляется свежий **`readCtx`** (аналогично раннему продлению внутри окна). Прогоны: **`go test .`** в **`third_party/masque-go`** (в т.ч. **`-run TestReadDeadline/extending -count=100`**), **`go test ./transport/masque/... ./protocol/masque/...`**.

**Ранее (§13):** **`proxiedConn.SetReadDeadline`**: вызовы **`readCtxCancel()`** и ожидание **`<-timer.C`** выполняются **после** отпускания **`deadlineMx`**, колбэк **`time.AfterFunc`** копирует **`readCtxCancel`** под замком (синхронная отмена и **`ReadFrom`** после **`ReceiveDatagram`** — без взаимного захвата с **`deadlineMx`**).

**Ранее (§13):** паритет **H2 ↔ H3** в **`dialConnectIPAttempt`**: при уже отменённом **`ctx`** на ветке CONNECT-IP **HTTP/2** вызывается **`clearHTTPFallbackConsumedAfterGivingUp()`** до любого входа в **`dialConnectIPHTTP2`** (как на ветке H3 перед **`openHTTP3ClientConn`**); в **`dialConnectIPHTTP2`** тот же сброс защёлки при раннем **`Cause(ctx)`**. Иначе изолированный вызов (тесты / узкие входы без повторного **`openIPSessionLocked`**) мог оставить **`httpFallbackConsumed`** и блокировать следующий **H3↔H2** pivot. Юнит: **`TestDialConnectIPAttemptH2ReturnsCanceledBeforeLayerWorkClearsFallbackLatch`**.

**Ранее (§13):** аудит **`protocol/masque`**, **`transport/masque`**, **`common/masque`**: на критических путях H2 новых блокеров не найдено. **`dialConnectIPHTTP2`** — ошибки **`ensureH2UDPTransport`** с префиксом **`masque connect-ip h2:`**. Док-комментарии: **`IPPacketSessionWithContext`**, **`WarpMasqueTLSPackageFromProfile`** — ST1020/ST1021; **`staticcheck`** / **`go test .`** в **`third_party/connect-ip-go`**, **`third_party/masque-go`**.

**Ранее (§13):** проход без блокеров; **`endpoint_warp_masque.go`** — **`bootstrapTLSName`** (ST1003); паритет логов CONNECT-stream H2/H3 (**`target=`** при пустом **`tcpURL.Host`**).

**Ранее (§13):** **`transport/masque`** — счётчики **`observability.go`** (**`SnapshotMetrics`**) на прод-путях **`coreSession.DialContext`**: **`recordTCPDialSuccess`/`recordTCPDialFailure`**, **`recordTCPDialErrorClass`**, **`recordTCPFallback`** перед **`dialDirectTCP`** при **`masque_or_direct`**, **`recordConnectIPStackReady`** при первом создании **`tcpNetstack`** в **`dialConnectIPTCP`**; удалён **`unavailableTCPNetstackFactory`** (**U1000**). Тест **`TestSnapshotMetricsTracksDialSuccessFallbackStackReady`**.

**Ранее (§13):** **`ClassifyMasqueFailure`** / **`IsMasqueHTTPLayerSwitchableFailure`**: вместо **`Contains(…, "401")`/`403`** — **`regexp.MustCompile(\`(401|403|407)\` с границами слова)`**, иначе номера портов (**`1401`**, **`84403`**) ложно попадали в **`connect_http_auth`** / «не switchable» и ломали ротацию портов WARP и **`http_layer_fallback`**. Добавлены **407** и фраза **`proxy authentication required`**. Регекс продублирован в **`transport/masque/http_layer_fallback.go`** (цикл импорта **`protocol` ↔ `transport`**). Тесты: **`TestClassifyMasqueFailure`**, **`TestIsRetryableWarpMasqueDataplanePortIdleTimeout`**, **`TestIsMasqueHTTPLayerSwitchableFailure`**.

**Ранее (§13):** статический аудит `protocol/masque` / `transport/masque`: удалены неиспользуемые **`connDeadlines.readTimeout`** и обёртка **`parseIPDestination`** в **`endpoint_server.go`**; убраны вызовы устаревшего **`net.Error.Temporary()`** (SA1019) в **`isRetryableConnectIPError`**, **`classifyConnectIPErrorReason`**, **`isRetryablePacketWriteError`** (**`netstack_adapter.go`**).

**Ранее (§13):** откат CONNECT-IP плоскости через **`releaseOpenedConnectIPSessionIfAbandoned()`** (**`tcpNetstack`**/**`ipConn`**, сброс **`ipHTTP`**/**`h2UdpTransport`**): ветка **`ListenPacket` / `connect_ip`** (отмена **`ctx`** до **`newConnectIPUDPPacketConn`**); **`dialConnectIPTCP`** — при отмене до **`ns.DialContext`** полный откат **только** если в этом вызове создан **`tcpNetstack`** (**`netstackCreatedThisCall`**); при ошибке **`DefaultTCPNetstackFactory.New`** — откат (**`TestReleaseOpenedConnectIPSessionIfAbandonedClearsHTTPLayers`**). В **`connect-ip-go`** — защитный **`(*Conn).Close`** при **`str==nil`** и т.д.

**Ранее (§13):** **`openIPSessionLocked`** (цикл **`for s.advanceHop()`** после неуспеха CONNECT-IP на entry hop): при ошибке **`resetHopTemplates`** — **`errors.Join(resetErr, context.Cause(ctx))`** при отменённом контексте; после неуспешного **`dialConnectIPOnCurrentHopLocked`** при **`ctx.Err()!=nil`** — **`errors.Join(err, context.Cause(ctx))`** и **без** лишнего **`advanceHop()`**. Тест: **`TestOpenIPSessionLockedCanceledDialInnerLoopDoesNotAdvanceHopAgain`**.

**Ранее (§13):** паритет отмены контекста при **исчерпании hop-цепочки** (окно после успешной проверки **`ctx.Err()==nil`** и до / внутри **`advanceHop`**): в [`transport/masque/transport.go`](hiddify-core/hiddify-sing-box/transport/masque/transport.go) — **`dialTCPStream`**: при **`!advanceHop()`** и при ошибке **`resetHopTemplates`** — **`errors.Join(..., context.Cause(ctx))`**; хук **`dialTCPStreamPreAdvanceHopHook`** (только тесты) перед **`mu.Lock`** для воспроизведения гонки; **`ListenPacket`** (connect_udp): те же правила на «нет следующего hop» и на **`resetHopTemplates`**, хук **`listenPacketPreChainEndReturnHook`**; **`openIPSessionLocked`**: финальная ошибка после перебора hop’ов. Тесты: **`TestDialTCPStreamPreAdvanceHopJoinsCauseWhenCanceledAtChainEnd`**, **`TestListenPacketUDPChainEndJoinsCauseWhenCanceledAtHopExhaustion`**.

**Ранее (§13):** **`dialTCPStream`**: если контекст отменён **после** **`dialTCPStreamAttempt`** и **до** следующей итерации — **`errors.Join(lastErr, context.Cause(ctx))`**; **`TestDialTCPStreamOuterLoopJoinsContextCauseWhenCanceledDuringRoundTrip`**.

**Ранее (§13):** **`openIPSessionLocked`**: при отмене **`ctx`** после неуспешного CONNECT-IP на hop — **`Cause(ctx)`** и **`clearHTTPFallbackConsumedAfterGivingUp()`** до **`advanceHop`** (не расходовать цепочку); **`TestOpenIPSessionLockedCanceledDialDoesNotAdvanceHop`**.

**Ранее (§13):** **`coreSession.dialConnectIPTCP`**: при раннем **`ctx.Done()`** до **`openIPSessionLocked`** и при ошибке **`normalizeTCPDestinationForConnectIPNetstack`** при уже отменённом **`ctx`** — **`clearHTTPFallbackConsumedAfterGivingUp()`**. Юнит **`TestDialConnectIPTCPCanceledClearsHTTPFallbackLatch`**.

**Ранее (§13):** **`coreSession.DialContext`**: при раннем **`select` по `ctx.Done()`** (до веток **`dialTCPStream`** / **`dialConnectIPTCP`**) **`clearHTTPFallbackConsumedAfterGivingUp()`** — **`TestDialContextCanceledBeforeTCPBranches`**.

**Ранее (§13):** **`runtimeImpl.OpenIPSession`** ([`common/masque/runtime.go`](hiddify-core/hiddify-sing-box/common/masque/runtime.go)): если **`ipPlane`** уже закэширован (**`Start`** с **`transport_mode: connect_ip`**) и **`ctx.Err() != nil`**, делегирование в **`session.OpenIPSession(ctx)`**, чтобы сработал входной путь **`coreSession.OpenIPSession`** (**`clearHTTPFallbackConsumedAfterGivingUp()`** до reuse **`ipConn`**). Голый **`return Cause(ctx)`** без захода в транспорт после предыдущего шага оставлял **`http_layer_fallback`**-защёлку навсегда активной на живой сессии. Юнит **`TestRuntimeOpenIPCanceledBeforeCachedIPPlaneReturn`**: ожидается один дополнительный вызов **`OpenIPSession`**. Прогоны: **`go test ./common/masque/... ./transport/masque/... ./protocol/masque/...`**, **`go vet`**.

**Ранее (§13):** **`runtimeImpl.OpenIPSession`** (без закэшированного **`ipPlane`**): ранний **`select`/`ctx.Err()` до ветки **`ipPlane`** ошибочно возвращал **`Cause`** без **`ClientSession.OpenIPSession`** при отмене — обход **`clearHTTPFallbackConsumedAfterGivingUp()`**; исправлено раньше через паритет **`testSession`** / **`TestRuntimeOpenIPCanceledStillCallsSessionWhenNoCachedPlane`**.

**Ранее (§13):** **`coreSession.ListenPacket`** (ветка **`connect_ip`**): после успешного **`openIPSessionLocked`** и **`Unlock`** — тестовый хук **`listenPacketPostOpenIPSessionUnlockHook`**, затем **`select` по `ctx.Done()`** + **`clearHTTPFallbackConsumedAfterGivingUp()`** перед **`newConnectIPUDPPacketConn`** … **`dialConnectIPTCP`**: после **`Unlock`** и до **`ns.DialContext`** — тот же **`select`** и сброс защёлки. Юниты: **`TestListenPacketConnectIPCanceledBeforeNewConnectIPUDPPacketConn`**.

**Ранее (§13):** **`coreSession.ListenPacket`** (ветка **`connect_udp`**): после снятия **`mu`** и **до** **`resolveDestinationHost`** / последующего UDP-диала — **`select` по `ctx.Done()`** + сброс защёлки **`http_layer_fallback`**. Тестовый хук **`listenPacketPreResolveDestinationHook`** только для пакета. Юнит: **`TestListenPacketConnectUDPCanceledBeforeResolveDestination`**.

**Ранее (§13):** **`directSession`** (пограничный бэкенд без MASQUE-оверлея): ранний **`select` по `ctx.Done()`** в **`DialContext`** (после проверки TCP network, до **`resolveDestinationHost`** / **`net.Dialer.DialContext`**), **`ListenPacket`** (до **`net.ListenPacket`**) и **`OpenIPSession`** (до проверок capability) — паритет с **`coreSession`**, без лишних системных вызовов при уже отменённом контексте. Юниты: **`TestDirectSessionListenPacketReturnsCanceledBeforeBind`**, **`TestDirectSessionDialContextReturnsCanceledBeforeHostResolve`**, **`TestDirectSessionOpenIPSessionReturnsCanceledBeforeCapabilityBoundary`**. Прогоны: **`go test ./transport/masque/... ./protocol/masque/...`**, **`go vet`**.

**Ранее (§13):** **`ListenPacket` (`coreSession`):** ранний **`select` по `ctx.Done()`** до **`s.mu.Lock()`** (паритет с **`DialContext`**, без входа в **`connect_ip` / `openIPSessionLocked`** и без лишнего mutex при уже отменённом контексте); при выходе по отмене — **`clearHTTPFallbackConsumedAfterGivingUp()`**. **`OpenIPSession`:** тот же ранний выход + сброс защёлки. Юниты: **`TestListenPacketConnectIPCanceledBeforeOpenIPSessions`**, **`TestOpenIPSessionCanceledBeforeLockSkipsConnectIPDial`**, **`TestListenPacketCanceledSkipsUDPDialHook`** / **`TestDialContextCanceledBeforeTCPBranches`**.

**Ранее (§13):** **`dialTCPStreamAttempt`:** после **`s.mu.Unlock()`** и **до** **`resolveDestinationHost` / `templateTCP.Expand`** — **`select` по **`ctx.Done()`** → **`errors.Join(ErrTCPConnectStreamFailed, context.Cause(ctx))`**.

**Ранее (§13):** **`dialConnectIPAttempt` (ветка H3):** после проверки отмены контекста и **до** **`masque_http_layer_attempt`** / **`openHTTP3ClientConn`** — **`templateIP == nil` → `ErrConnectIPTemplateNotConfigured`** (паритет с **`dialConnectIPHTTP2`**). Юнит: **`TestDialConnectIPAttemptH3ReturnsErrWhenTemplateNil`**.

**Ранее (§13):** **`dialConnectIPHTTP2`:** если **`templateIP == nil`** (ранний защитный путь до **`masque_http_layer_attempt`**), возвращается **`ErrConnectIPTemplateNotConfigured`** без префикса **`masque connect-ip h2:`** — иначе обёртка совпадала бы с эвристикой **`http_layer_fallback`** даже для чистой misconfig. Sentinel в **`transport/masque/errors.go`**; **`IsMasqueHTTPLayerSwitchableFailure`** игнорирует **`ErrConnectUDPTemplateNotConfigured`** / **`ErrConnectIPTemplateNotConfigured`** (в т.ч. при **`fmt.Errorf`…`%w`**). Юниты: **`TestDialConnectIPHTTP2ReturnsErrWhenTemplateNil`**, **`TestDialUDPAddrH2ReturnsErrWhenTemplateNil`**, расширение **`TestIsMasqueHTTPLayerSwitchableFailure`**.

**Ранее (§13):** **`dialUDPAddr`** (ветка **H2**): до **`dialUDPOverHTTP2`** добавлены **`select` по отменённому `ctx`** и проверка **`template == nil`** — паритет с веткой **H3** (ранний выход без входа в оверлей); юнит **`TestDialUDPAddrH2ReturnsCanceledBeforeDialUDPOverHTTP2`** (хук **`h2UDPConnectHook`** не вызывается при уже отменённом контексте).

**Ранее (§13):** лог **`masque_http_layer_attempt`** для CONNECT-UDP (**H3**/**H2**) с **`connect_udp=1`**, паритетно **`connect_ip=1`** / **`tcp_stream=1`**.

**Ранее (§13):** **`dialConnectIPAttempt` + тест-хук `dialConnectIPAttemptHook`:** при успешном подменном диале (юниты) порядок и наблюдаемость выровнены с прод-путём: **`maybeRecordHTTPLayerCacheSuccess`** → **`masque_http_layer_chosen`** (`connect_ip=1`, **`layer=h2|h3`**) → **`resetHTTPFallbackBudgetAfterSuccess`** (раньше сначала вызывался сброс fallback, не было строки **`chosen`**).

**Ранее (та же итерация — коротко):** **`dialTCPStreamAttempt` (CONNECT-stream):** при успешном **`dialTCPStreamH2` / `dialTCPStreamHTTP3`** порядок приведён к **`dialUDPAddr`** / **`dialConnectIPAttempt`** — **`maybeRecordHTTPLayerCacheSuccess`** → **`masque_http_layer_chosen`** (`tcp_stream=1`) → **`resetHTTPFallbackBudgetAfterSuccess`**. Низкоуровневые **`dialTCPStreamH2`** и **`dialTCPStreamHTTP3`** больше не пишут TTL/`chosen`/сброс fallback; внешний **`dialTCPStream`** не дублирует **`resetHTTPFallbackBudgetAfterSuccess`**.

**Ранее (та же итерация — коротко):** **`dialUDPAddr` (CONNECT-UDP, H2 и H3):** при успешном диале порядок приведён к **`dialConnectIPAttempt`** — **`maybeRecordHTTPLayerCacheSuccess`** → **`masque_http_layer_chosen`** → **`resetHTTPFallbackBudgetAfterSuccess`**; лог **`chosen`** перенесён из **`dialUDPOverHTTP2`** в **`dialUDPAddr`**. В сообщение добавлен **`connect_udp=1`**. Низкоуровневый **`dialUDPOverHTTP2`** остаётся без записи TTL и без «chosen».

**Ранее (та же итерация — коротко):** **`dialConnectIPHTTP2` / `dialConnectIPAttempt`:** запись TTL, **`masque_http_layer_chosen`** и сброс fallback перенесены из **`dialConnectIPHTTP2`** в **`dialConnectIPAttempt`**.

**Ранее (та же итерация — коротко):** **`transport/masque` `openIPSessionLocked`:** последовательность CONNECT-IP на **одном hop** (dial → `http_layer_fallback` → churn H3 **`ipHTTP`** → churn **`h2UdpTransport`**) вынесена в **`dialConnectIPOnCurrentHopLocked`**. **`dialConnectIPAttempt`** при успехе через **`dialConnectIPAttemptHook`** вызывает **`maybeRecordHTTPLayerCacheSuccess`** для **h2/h3**. Юнит: **`TestDialConnectIPAttemptHookRecordsHTTPLayerCacheSuccess`**.

**Ранее (та же итерация — коротко):** **`ClassifyMasqueFailure`:** ключ **`h3_extended_connect`** ставится при подстроке **`extended connect`** в **нижнем регистре**, а не только при каноническом **`Extended CONNECT`** — метрики и **`IsRetryableWarpMasqueDataplanePort`** не уходят в **`other`** на реальных/lowercased текстах QUIC/http3; кейс **`extended connect not supported`** по-прежнему **`h2_extended_connect_rfc8441`** (порядок веток). Юниты: **`TestClassifyMasqueFailure`**.

**Ранее (та же итерация — коротко):** **`transport/masque` `resolveTLSServerName`:** **`strings.TrimSpace`** для **SNI** / **`Server`** (TTL **`http_layer`**, паритет **`masqueQuicDialCandidateHost`**). **`TestResolveTLSServerNameTrimmed`**.

**Ранее (та же итерация — коротко):** **`common/masque/runtime.go` (`Start` → `NewSession`):** **`T.ClientOptions`** — **`TrimSpace`** для **`Server`**, **`DialPeer`**, **`ServerToken`**, **`TLSServerName`**. **`TestRuntimeNewSessionTrimsDialIdentityStrings`**.

**Ранее (та же итерация — коротко):** **`common/masque.BuildChain`:** для single-hop и для каждого hop в chain-mode **`Server`** нормализуется **`strings.TrimSpace`** — паритет с **`resolveMasqueEntryServerPort`**, TTL-кэшем **`http_layer`** и **`coreSession.options.Server`** в **`resetHopTemplates`** при **`advanceHop`**. Юнит: **`TestBuildChainTrimsServerWhitespace`**.

**Ранее (та же итерация — коротко):** **`resolveMasqueEntryServerPort`:** для hop’а с пустым **`Via`** возвращается **`strings.TrimSpace(h.Server)`** (паритет кэша/TLS; юнит **`TestResolveMasqueEntryServerPortTrimsWhitespace`**).

**Ранее (та же итерация — коротко):** **`warp_masque` bootstrap (перебор dataplane-портов):** до лога каждого кандидата вызывается **`EffectiveMasqueClientHTTPLayer`**; в сообщение добавлен **`masque_overlay=masque_tcp_h2_tls`** при эффективном **`http_layer: h2`** / **`auto`+TTL на h2**, иначе **`masque_udp_h3_quic`**; **`quic_peer`** → **`quic_dial_peer`**.

**Ранее (та же итерация — коротко):** **`ClassifyMasqueFailure`:** сырые **`http2: …`** (без **`masque h2:`**) → **`h2_masque_handshake`**; **`TestClassifyMasqueFailure`**; **`go test ./transport/masque/...`**, **`go vet`**.

**Ранее (та же итерация — аудит кодовой базы):** фазы A–E по H2 MASQUE в **`hiddify-sing-box` закрыты** (валдация, кэш/TTL, fallback, CONNECT‑UDP/stream/IP H2 + сервер; юниты и упомянутые стендовые сценарии **`masque_h2_smoke`**, **`masque_h2_connect_ip`**, **`masque_http_layer_auto_cache_smoke`**). Явных невыполненных пунктов в §13 перед этим проходом не было.

**Ранее (та же итерация — справочно):** **`third_party/masque-go` `proxiedConn`** (фон **`skipCapsules`** на request‑stream после CONNECT‑UDP H3): условие лога **`reading from request stream failed`** переведено на **`err != nil && !errors.Is(err, io.EOF)`**, паритетно ветке **`Proxy.ProxyConnectedSocket`**. Прогоны: **`go test .`** в **`third_party/masque-go`**.

**Ранее (та же итерация):** **`third_party/masque-go` `skipCapsules`**: убран **`log.Printf` на каждую капсулу**; **`transport/masque` `dialTCPStreamH2`** non‑2xx **`masque h2: … status=`**; **`ClassifyMasqueFailure`** **`masque connect-udp h3 skip-capsules`** → **`other`**; **`TestIsMasqueHTTPLayerSwitchableFailure`**.

**Ранее (та же итерация):** **`IsMasqueHTTPLayerSwitchableFailure`** исключает **`masque connect-udp h3 skip-capsules`** из **`http_layer_fallback`** (слив капсул на уже поднятом QUIC‑туннеле не «лечится» сменой H2↔H3).

**Ранее (та же итерация):** **`IsMasqueHTTPLayerSwitchableFailure`**: ответы прокси **HTTP non-2xx** на **H3 CONNECT-UDP** (`masque-go`, **`masque: server responded with`**) и **H3 CONNECT-IP** (`connect-ip-go`, **`connect-ip: server responded with`**) — явные подстроки для паритета с **`masque h2:`** / **`masque connect-ip h2:`**; **401/403** через общие проверки auth. **`TestIsMasqueHTTPLayerSwitchableFailure`**.

**Ранее (та же итерация):** **`IsMasqueHTTPLayerSwitchableFailure`**: non-2xx на **HTTP/3 CONNECT-stream** (`ErrTCPConnectStreamFailed` + **`tcp connect-stream failed: status=`**) — симметрия с **H2** и **`masque h2:`**; см. **`dialTCPStreamHTTP3`**, тот же тестовый файл.

**Ранее (та же итерация):** **`dialConnectIPAttempt`**: после успешного подъёма CONNECT-IP (**hook**, **`dialConnectIPHTTP2`**, **`dialWarpConnectIPTunnel`** с непустым **`Conn`**) вызывается **`resetHTTPFallbackBudgetAfterSuccess`** — паритет с **`dialUDPAddr`** (**httpFallbackConsumed** сбрасывается на handshake-слое); из **`openIPSessionLocked`** после присвоения **`ipConn`** без дублирующих вызовов.

**Ранее (та же итерация):** CONNECT-IP IPv4 UDP: учёт **фрагментации RFC 791** (MF / ненулевой fragment offset). Пакеты с признаками фрагментации не считаются кандидатами UDP-моста (**`classifyIPv4UDPBridgeCandidate` → не malformed**, уходит в netstack ingress при наличии стека); **`parseIPv4UDPPacketOffsets`** отвергает те же случаи, чтобы **`ReadFrom`** без подписчиков не выдавал усечённый payload как целый UDP. Хелпер **`ipv4HeaderIndicatesFragmentation`**, расширение **`TestClassifyIPv4UDPBridgeCandidate`**.

**Ранее (та же итерация):** **`connectIPIngressLoop`**: перед **`deliverIPv4UDPBridgedIngress`** пакеты с IPv4 **`protocol == UDP`**, но не проходящие **`parseIPv4UDPPacketOffsets`**, больше не клонируются в очереди подписчиков UDP-моста. Счётчик **`ingress_udp_malformed`**; функция **`classifyIPv4UDPBridgeCandidate`**.

**Ранее (та же итерация):** **`connectIPUDPPacketConn.ReadFrom`**, ветка **`ingressSub`** (`transport/masque/transport.go`): для **`len(p) >= connectIPUDPDirectReadMin`** убрано условие **`payloadOff != 0`** перед **`copy`** — UDP-payload из канала ingress **всегда** копируется в буфер вызова (как на пути **`ReadPacket`**); проверка **`payloadOff+payloadLen <= len(raw)`**. Юнит: **`TestConnectIPUDPPacketConnReadFromIngressDirectBuffer`**.

**Ранее (та же итерация):** **`connectIPUDPPacketConn.ReadFrom`** (ветка **`ReadPacket`**, прямой буфер): компактная выдача UDP-payload из **`raw`**; **`payloadOff+payloadLen <= len(raw)`**. Прогон: **`go test ./transport/masque/... -run ConnectIPUDP`**.

**Ранее (та же итерация):** **`IsMasqueHTTPLayerSwitchableFailure`** (`transport/masque/http_layer_fallback.go`): после проверки auth добавлен **`net.Error.Timeout()`** через **`errors.As`** и явное исключение **`net.DNSError` с `Timeout()==true`** (один резолвер для обоих оверлеев). Это покрывает таймауты TCP/TLS (в т.ч. Windows **`connectex` / «properly respond after a period of time»**), где в тексте ошибки часто нет **`i/o timeout`** / **`connection timed out`**. Юниты: синтетический **`dialTimeoutNetErr`**, кейсы **`DNSError`/wrapped** в **`TestIsMasqueHTTPLayerSwitchableFailure`**. Прогон: **`go test ./transport/masque/... ./protocol/masque/...`**.

**Ранее (та же итерация):** **`IsMasqueHTTPLayerSwitchableFailure`:** к переключаемым сетевым сбоям добавлено подстроковое **`connection timed out`** (Linux/обёртки без **`i/o timeout`**). **`TestIsMasqueHTTPLayerSwitchableFailure`**.

**Ранее (та же итерация):** **`transport/masque` `resetHopTemplates`:** при переходе на внутренний hop цепочки (**`hopIndex > 0`**) сбрасывается **`DialPeer`**, иначе override сокет-цели для входного hop’а оставался бы действительным при **`Server`/`ServerPort`**, взятых из inner hop — некорректный адрес для QUIC/TCP-оверлея MASQUE. Юнит: **`TestResetHopTemplatesClearsDialPeerOnInnerHop`**.

**Ранее (та же итерация):** **`third_party/connect-ip-go` (`DialWithOptions`, HTTP/3 CONNECT-IP):** после успешного **`ReadResponse`/2xx** добавлены тест-хук и проверка **`context.Cause(ctx)`** перед выдачей **`Conn`** — при уже отменённом контексте диала **`rstr.Close()`**, сеанс не возвращается; паритет с **`DialHTTP2`**, **`masque-go` CONNECT-UDP** и транспортным слоем без дублирующего **`abortConnectIPH3DialIfCanceled`** в **`transport/masque`**. Юнит: **`TestDialWithOptionsReturnsCauseWhenCanceledAfterSuccessfulCONNECTIPResponse`**. Прогоны: **`go test ./third_party/connect-ip-go`**, **`go test ./transport/masque/... ./protocol/masque/...`**.

**Ранее (справочно):** гейт отмены после HTTP/3 CONNECT-IP первоначально делался в **`transport/masque`** через **`abortConnectIPH3DialIfCanceled`** после **`dialWarpConnectIPTunnel`**; перенесён в **`connectip.DialWithOptions`** (см. абзац «Ранее (та же итерация)» про DialWithOptions выше). **`transport/masque`** — **`ErrConnectUDPTemplateNotConfigured`** / **`nil` UDP-шаблон**; **`third_party/masque-go`** — **`nil` шаблон**; **`connect-ip-go`** — **`validateFlowForwardingTemplateVars`**, префикс **`masque connect-ip h3 dataplane:`** для H3.

**Ранее (та же дата):** **`dialTCPStreamHTTP3`** — при ошибке **`http.NewRequestWithContext`** (**`build TCP MASQUE request`**) возврат переведён на **`errors.Join(ErrTCPConnectStreamFailed, E.Cause(…))`**, паритетно **`dialTCPStreamH2`** и внутренним веткам HTTP/3 (отмена, RoundTrip и т.д.), чтобы **`errors.Is(…, ErrTCPConnectStreamFailed)`**, ретраи CONNECT-stream и смежная логика не расходились между слоями.

**Ранее (та же дата):** **`dialTCPStreamH2`** — ошибки раннего **`ensureH2UDPTransport`** и сборки **`http.Request`** объединяются с **`ErrTCPConnectStreamFailed`** (**`errors.Join`**), как на ветке HTTP/3 и во внутреннем цикле H2. Юнит: **`TestDialTCPStreamH2JoinsErrWhenH2TransportUnconfigured`**.

**Ранее (та же дата):** повторный проход по **`protocol/masque`**, **`transport/masque`**, `go vet` — без иных находок; **`DialHTTP2`** с пост-handshake **`context.Cause(ctx)`**; **`TestClassifyMasqueFailure`** для **`masque connect-ip h2:`** vs **`… h2 dataplane`**. Примечание: **`go test -race ./transport/masque/...`** на части сборок может срываться внутри in-process QUIC/http3 harness.

**Ранее — relay-контекст Extended CONNECT H2 (`NewH2ExtendedConnectRequestContext`, [`third_party/connect-ip-go/client_http2.go`](hiddify-core/hiddify-sing-box/third_party/connect-ip-go/client_http2.go)):** та же гонка ранее дублировалась в копии в `transport/masque` — старый `select` между `parent.Done()` и `stopRelay` при одновременной готовности мог отменять `reqCtx` после успешного `stop(true)` (detachment) и рвать долгий CONNECT-IP H2. Логика relay синхронизирована; **`TestH2ExtendedConnectRequestContext*`** в пакете `connectip`. Прогоны: [`third_party/connect-ip-go`](hiddify-core/hiddify-sing-box/third_party/connect-ip-go) — `go test . -run TestH2ExtendedConnectRequestContext -count=20`; `go test .`; [`hiddify-sing-box`](hiddify-core/hiddify-sing-box) — `go test ./protocol/masque/... ./transport/masque/...`.

**Ранее — та же ось (гонка после detachment):** внешний `select` по `parent.Done()` и закрытым `stopRelay` после `stop(true)` и последующего `cancelParent()` выбирал ветку наугад; при попадании в `parent.Done()` первым без проверки, что handshake уже завершён (`stopRelay` закрыт), вызывался `cancel()` на `WithoutCancel`-потоке и рвался живой CONNECT-UDP/‑stream/IP. Исправление: после `parent.Done()` вложенный `select`/`default`: если `stopRelay` уже закрыт — **не** звать `cancel()`. Прогоны: `go test ./protocol/masque/... ./transport/masque/...`, **`TestH2ExtendedConnectRequestContextDetachesAfterHandshake`** (в т.ч. `-run … -count=20`).

**Ранее:** **H2 CONNECT-stream retry cleanup для request-context relay:** в `transport/masque` (`dialTCPStreamH2`) stop-функция relay-контекста больше не держится через `defer` внутри retry-цикла. Для каждой неуспешной попытки (`RoundTrip` error / non-2xx / cancel gate) relay закрывается немедленно, чтобы не накапливать активные relay-горутины до выхода из функции. Добавлен тест **`TestDialTCPStreamH2StopsFailedRequestRelayBeforeRetry`** и инъекционная фабрика `h2ConnectRequestContextFactory` для контроля жизненного цикла в юнитах.

**Ранее:** **`connect-ip-go` `Conn.Routes`:** возврат маршрутов переведён на **`slices.Clone(c.availableRoutes)`** (паритет с `LocalPrefixes`), чтобы вызывающий код не мог мутировать внутреннее состояние CONNECT-IP сессии через alias на общий slice (`availableRoutes`) и ломать policy-view в рантайме. Юнит: **`TestRoutesReturnsClonedSlice`**.

**Ранее:** **`http_layer_fallback` + защёлка `httpFallbackConsumed`:** после **полного** отказа подъёма датаплейна (ошибка наружу из `ListenPacket`, `openIPSessionLocked`, `dialTCPStream` без успешного handshake) защёлка **сбрасывается** (`clearHTTPFallbackConsumedAfterGivingUp`), иначе следующий пользовательский вызов мог **никогда не сделать второй pivot H3↔H2** после волны, где fallback уже один раз переключил слой, но второй попыткой сессия не поднялась. Юниты: расширения **`TestListenPacketHTTPFallbackRunsAfterReconnectDialSwitchableFailure`**, **`TestDialTCPStreamHTTPFallbackRunsAfterReconnectRoundTripSwitchableFailure`**, **`TestOpenIPSessionFailureClearsHTTPFallbackLatchForNextAttempt`**.

**Ранее:** **`ParseMasqueHTTPDatagramUDP` + H2 CONNECT-UDP клиент/сервер:** семантика как у **`third_party/masque-go` `parseProxiedDatagramPayload`** (`io.EOF` для усечённых префиксов контекста, пустого payload и т.д.). Некорректный **HTTP Datagram payload** внутри валидной DATAGRAM-капсулы **отбрасывается** (`continue`), туннель не рвём — паритет с H3 **`proxiedConn.ReadFrom`** (ненадёжная датаграм-плоскость). Сервер **`ServeH2ConnectUDP`** — то же для аплинка. Юнит: **`TestH2ConnectUDPPacketConnSkipsMalformedHTTPDatagramThenReadsValid`**.

**Ранее:** **`h2ConnectUDPPacketConn.WriteTo` (HTTP/2 CONNECT-UDP):** при ошибке **`reqBody.Write`** после успешного CONNECT вызывается **`Close()`** — паритет с дорожкой **`ReadFrom`** при ошибках **капсульного фрейминга** (не путать с отбрасыванием битого UDP payload выше). Юнит: **`TestH2ConnectUDPPacketConnClosesOnWriteBodyError`**.

**Ранее:** **`h2ConnectUDPPacketConn.ReadFrom`:** после успешного Extended CONNECT при ошибке разбора капсулы (не `io.EOF`), дренажа не-DATAGRAM капсулы, чтения тела DATAGRAM или разбора HTTP Datagram — **`Close()`**; **`dialTCPStreamH2`:** в `masque_http_layer_chosen` для `tcp_stream` — **`options.Tag`**. Юнит: **`TestH2ConnectUDPPacketConnClosesOnTruncatedCapsulePrefix`**.

**Ранее:** **`dialTCPStreamHTTP3`:** после успешного `RoundTrip`/2xx — проверка **`context.Cause(ctx)`** перед возвратом `streamConn` (паритет с **`dialTCPStreamH2`** и **`connectip.DialHTTP2`**): не возвращать «установленный» CONNECT-stream, если родительский dial-контекст уже отменён во время рукопожатия (в т.ч. кастомный `RoundTripper`). Юнит: **`TestDialTCPStreamHTTP3ReturnsCanceledAfterRoundTripSuccess`**.

**Ранее:** **паритет шаблонов CONNECT-UDP / CONNECT-stream с `connect-ip-go`:** в `protocol/masque` — `parseTCPTargetFromRequest`; в форке **`third_party/masque-go`** — `connectUDPTemplateMatchCandidates`. Кандидат для матча к абсолютному URI-template строится как у `matchTemplateRequestValues`: для `RequestURI` без схемы — нормализация ведущего `/`, затем `scheme://:authority + path`; для уже абсолютного `http(s)://…` — используется как есть. Иначе path-only стек H2 мог отдать `RequestURI` без ведущего слэша и ложно не матчить `{target_host}`/`{target_port}`. Юниты: **`TestParseTCPTargetFromRequestSchemelessRequestURIWithoutLeadingSlash`**, **`TestConnectUDPRequestPathTemplateSchemelessRequestURIWithoutLeadingSlash`**.

**Ранее:** **`connect-ip-go ParseRequest` (H2/H3 CONNECT-IP flow-forwarding):** в `matchTemplateRequestValues` добавлен кандидат `https://<:authority><RequestURI>` для path-only серверных запросов (`URL.Path` / `RequestURI` без scheme+host), чтобы абсолютный URI-template (`https://host/.../{target}/{ipproto}`) корректно матчил валидный CONNECT-запрос и не отдавал ложный `400 request does not match flow forwarding template`. Юнит: **`TestConnectIPRequestParsing/parse_scoped_flow_forwarding_variables_from_path-only_request_URI`**.

**Ранее:** **H2 Extended CONNECT (post-handshake cancel gate):** после успешного `RoundTrip`/2xx в **`dialUDPOverHTTP2`**, **`dialTCPStreamH2`** и **`connectip.DialHTTP2`** добавлена проверка **`context.Cause(ctx)`** до возврата dataplane-сессии. Это закрывает окно, где dial-контекст уже отменён во время рукопожатия, но код всё ещё возвращал «успешный» H2-туннель. Юниты: **`TestDialTCPStreamH2ReturnsCanceledAfterRoundTripSuccess`**, **`TestDialHTTP2ReturnsCanceledAfterRoundTripSuccess`**.

**Ранее:** **`openHTTP3ClientConn`:** при **`ctx.Err() != nil`** возврат **`context.Cause(ctx)`** **до** возврата кэшированного **`ipHTTPConn`** — паритет с **`openIPSessionLocked`** (не выдавать успешный reuse при уже отменённом контексте). Юнит: **`TestOpenHTTP3ClientConnReturnsCanceledBeforeReuse`**.

**Ранее:** **`openIPSessionLocked`:** если **`ctx` уже отменён**, возврат **`context.Cause(ctx)`** **до** ветки повторного использования **`ipConn`** (метрика **`classifyConnectIPErrorReason`**, событие **`open_ip_session_fail`**), паритет с **`dialConnectIPAttempt`**, **`ListenPacket` (connect_ip)** и **`dialConnectIPTCP`** по отмене. Юнит: **`TestOpenIPSessionLockedReturnsCanceledBeforeReuse`**.

**Ранее:** паритет H3 с уже исправленным H2: **`dialUDPAddr`** (ветка QUIC), **`dialConnectIPAttempt`** (HTTP/3 CONNECT-IP), **`dialTCPStreamHTTP3`** — ранний **`select` по `ctx.Done()`** до **`masque_http_layer_attempt`**, чтобы не логировать попытку слоя и не трогать QUIC/OpenIP при уже отменённом контексте. **`dialTCPStreamHTTP3`:** отмена до лога с **`errors.Join(ErrTCPConnectStreamFailed, …)`** как у цикла. Юниты: **`TestDialUDPAddrH3ReturnsCanceledBeforeLayerLog`**, **`TestDialConnectIPAttemptH3ReturnsCanceledBeforeLayerLog`**, **`TestDialTCPStreamHTTP3ReturnsCanceledBeforeLayerLog`**.

**Ранее:** точка входа H2-датаплана: **`select` по `ctx.Done()`** перенесён **раньше** — **`dialUDPOverHTTP2`**, **`dialConnectIPHTTP2`**, **`dialTCPStreamH2`**. Юниты: **`TestDialConnectIPHTTP2ReturnsCanceledBeforeTCPConfig`**, **`TestDialUDPOverHTTP2ReturnsCanceledBeforeTCPConfig`**, **`TestDialTCPStreamH2ReturnsCanceledBeforeTCPConfig`**.

**Ранее:** **`dialConnectIPHTTP2`:** проверка отмены **до** **`ensureH2UDPTransport()`** без маскировки «tcp dialer is not configured».

**Ранее — `third_party/masque-go` `Proxy.ProxyConnectedSocket`:** после дренажа капсул на **request stream** через **`skipCapsules`** ошибка **`io.EOF`** больше не логируется как **`reading from request stream failed`** (раньше стояло некорректное **`err == io.EOF`**). Реальных ошибок дренажа логируем только если **`err != nil && !errors.Is(err, io.EOF)`** и прокси ещё не в состоянии **`closed`** — паритет с **`proxiedConn`** в **`conn.go`**.

**Ранее — `readFromStream`:** (ветка неизвестной капсулы) ошибка **`io.Copy(io.Discard, cr)`** при сливе тела больше не игнорируется — иначе после обрыва потока цикл продолжал бы **`parseConnectIPStreamCapsule` не с границы капсулы**. Возврат через **`wrapH2ConnectIPStreamDataplaneErr`**; юнит **`TestReadFromStreamUnknownCapsuleDrainErrorWrapsDataplane`**.

**Ранее — H2 CONNECT‑UDP клиент и `ServeH2ConnectUDP`:** вместо сырого **`http3.ParseCapsule`** используется **`parseH2ConnectUDPCapsule`** — после type/length varint отвергается объявленное тело **`length`** до **`h2ConnectUDPMaxCapsulePayload`** для DATAGRAM и до **`h2ConnectUDPNondatagramMaxCapsulePayload`** иначе, **до** **`LimitedReader`**, устранён сценарий «прочли **maxPayload+1** из капсулы DATAGRAM → **`io.Copy(Discard, r)` до конца огромной длины**». Обёртка **`h2countingVarintReader`** повторяет правило **`http3.ParseCapsule`** для усечённого первого varint (**`UnexpectedEOF`** вместо «тихого» **`EOF`**). При политической ошибке клиент делает **`Close()`**. Юниты: **`TestParseH2ConnectUDPCapsuleRejectsAstronomicDatagramDeclaredLength`**, регрессия **`TestServeH2ConnectUDPWrapsCapsuleParseError`** / пороговые **`RejectsOversized*`** / **`SkipsLargeNonDatagramCapsule`**.

**Ранее — `third_party/masque-go` `skipCapsules`** (HTTP/3 CONNECT‑UDP: фоновый дренаж капсул на **request stream** у `proxiedConn` и завершение **`Proxy.ProxyConnectedSocket`):** раньше тело каждой капсулы сливалось через **`io.Copy(io.Discard, r)`** без лимита на объявленную длину — злоумышленник мог заставить считать **произвольно большое** объявление varint (**паритет с уже исправленным H2 путём**). Сейчас для **`ct == DATAGRAM (0)`** — потолок **`1500+128`**, для прочих типов **`65536`**, чтение через **`LimitReader(..., max+1)`**, при перевыходе — **`masque connect-udp h3 skip-capsules: type=… exceeds …`**. Юниты: **`TestSkipCapsulesRejectsOversizedNondatagramDeclaredLength`**, **`TestSkipCapsulesRejectsOversizedDatagramDeclaredLength`**, **`TestSkipCapsulesDrainSmallCapsulesUntilEOF`**.

**Ранее:** **`connect-ip-go`, CONNECT-IP поток (H2 и H3 со stream-капсулами):** вместо голого **`http3.ParseCapsule`** используется **`parseConnectIPStreamCapsule`** — после чтения type/length varint отвергается объявленное **`length`** сверх **`maxConnectIPNondatagramCapsulePayload` (65536)** для всех не‑DATAGRAM типов (включая неизвестные и RFC 9484 control) и сверх **`maxHTTPDatagramCapsulePayload`** для типа 0, **до** любого чтения тела; устранён сценарий «огромный varint → **`io.Copy(Discard, cr)`** до гигабайтов», паритетно политике **H2 CONNECT‑UDP** (`h2ConnectUDPNondatagramMaxCapsulePayload`). Тело читается через локальный **`capsuleExactReader`** (как **`http3.exactReader`**). Юниты: **`TestReadFromStreamRejectsOversizedUnknownCapsuleDeclaredLength`**, **`TestReadFromStreamRejectsOversizedAddressAssignDeclaredLength`**.

**Ранее:** **H2 CONNECT-UDP — лимит тела для капсул `ct != DATAGRAM`:** на клиенте (`h2ConnectUDPPacketConn.ReadFrom`) и на сервере (`ServeH2ConnectUDP`) слив через `LimitReader(..., nondatagramMax+1)`; при теле **`> h2ConnectUDPNondatagramMaxCapsulePayload` (65536 B)** возвращается ошибка **`masque h2 dataplane connect-udp … non-datagram capsule exceeds`** (не тратится `http_layer_fallback`), клиент вызывает **`Close`** чтобы не оставить поток в полуснятой капсуле. Юниты: **`TestH2ConnectUDPPacketConnRejectsOversizedNondatagramCapsule`**, **`TestServeH2ConnectUDPRejectsOversizedNondatagramCapsule`**.

**Ранее:** **CONNECT-UDP H2:** капсулы с типом **не** `DATAGRAM` обрабатывались до лимита DATAGRAM-пэйлоада; большие «чужие» не попадали под «DATAGRAM exceeds …». Юнит **`TestH2ConnectUDPPacketConnSkipsLargeNonDatagramCapsule`**.

**Ранее:** **`connect-ip-go` `Conn.errAfterClose`:** все возвраты из путей **`<-closeChan`** (в т.ч. **`ReadPacketWithContext`**, **`WritePacket`**, **`sendCapsule`**, **`LocalPrefixes`** / **`Routes`**, ingest в **`readFromStream`**, **`receiveProxiedDatagram`**) переведены на **`errAfterClose()`** — если **`closeErr` ещё не записан**, возвращается **`net.ErrClosed`**, а не **`nil`** (избегаем **`(0, nil)`** на чтении и бессмысленного успеха на записи при гонке teardown). Юнит: **`TestErrAfterCloseNeverNilWhenSignaled`**.

**Ранее:** **`IsMasqueHTTPLayerSwitchableFailure`:** вместо **`errors.As(…, *connectip.CloseError)`** — ранний **`errors.Is(err, net.ErrClosed)`**; явная проверка **`connectip.CloseError`** до sentinel против ложного **`http_layer_fallback`**. Путь **`receiveProxiedDatagram`** может вернуть **`net.ErrClosed`** при **`closeChan`** без **`closeErr`**. Тесты: **`TestIsMasqueHTTPLayerSwitchableFailure`**.

**Ранее:** **`streamConn` (`transport/masque/transport.go`), путь H2 CONNECT-stream:** после успешного Extended CONNECT ошибки **`Read`** / **`Write`** на потоке оборачиваются в **`masque h2 dataplane connect-stream read|write: …`**. Тесты: **`TestStreamConnH2DataplaneWrapsReadWriteErrors`**, **`TestClassifyMasqueFailure`**, **`TestIsMasqueHTTPLayerSwitchableFailure`**.

**Ранее:** **`ServeH2ConnectUDP` (`transport/masque/h2_connect_udp_server.go`):** ошибки разбора/записи капсул и UDP read/write при релее **CONNECT-UDP по HTTP/2** обёрнуты в **`masque h2 dataplane connect-udp server …`**, паритетно клиентскому **`ReadFrom`** / **`WriteTo`** — чтобы сырой текст (`http2:`, **`extended connect`** во вложенной причине и т.д.) не смешивался с handshake-классификацией и **`http_layer_fallback`** там, где релевантно. **`ensureH2UDPTransport` `DialTLSContext`:** ошибки **`TCPDial`** и **`HandshakeContext`** — с префиксом **`masque h2:`** (**`tcp dial`** / **`tls handshake`**). Тесты: **`TestServeH2ConnectUDPWrapsCapsuleParseError`**, расширения **`TestClassifyMasqueFailure`** / **`TestIsMasqueHTTPLayerSwitchableFailure`**.

**Ранее:** **`connect-ip-go` `WritePacket`:** на HTTP/2 capsule path ошибки **`composeDatagram`** обёрнуты в **`wrapH2ConnectIPStreamDataplaneErr`** (вложенные «handshake»/policy в причине compose не должны тратить **`http_layer_fallback`**). Юниты: **`TestWritePacketFailures`** / **`HTTP/2_capsule_dataplane_wraps_compose_error`**.

**Ранее:** **`connect-ip-go` (`third_party/connect-ip-go/conn.go`):** для HTTP/2 DATAGRAM-капсульного датаплейна (`datagramCapsuleIngress != nil`) ошибки **`Write`** контрольных капсул (`writeToStream`) и ошибки **`SendDatagram`** в **`WritePacket`** оборачиваются в **`wrapH2ConnectIPStreamDataplaneErr`** — паритет с чтением капсул и с CONNECT-UDP H2: иначе «broken pipe» / **`http2:`** без префикса **`masque connect-ip h2 dataplane:`** ложно трактовались **`IsMasqueHTTPLayerSwitchableFailure`** как сигнал **`http_layer_fallback`** на уже установленном туннеле. Юнит: **`TestIsMasqueHTTPLayerSwitchableFailure`** (`… h2 dataplane: write tcp: broken pipe`).

**Ранее:** **`connect-ip-go` `readFromStream`:** ошибки разбора капсул на HTTP/2 CONNECT-IP оборачиваются в **`wrapH2ConnectIPStreamDataplaneErr`**; тест **`TestIsMasqueHTTPLayerSwitchableFailure`** (nested «extended connect»).

**Ранее:** **`IsMasqueHTTPLayerSwitchableFailure` (`transport/masque/http_layer_fallback.go`):** до широких эвристик добавлен явный отказ для строк датаплейна **`masque h2 dataplane` / `masque connect-ip h2 dataplane`** (и варианты с опечаткой **`… h2: dataplane`**) — паритет с **`ClassifyMasqueFailure`**, чтобы вложенный текст с **`extended connect`** / **`handshake`** не тратил **`http_layer_fallback`** на уже поднятом туннеле. Юнит: **`TestIsMasqueHTTPLayerSwitchableFailure`**.

**Ранее:** **`ClassifyMasqueFailure` (`protocol/masque/errors_classify.go`):** ветка **RFC 8441** «extended connect not supported» сравнивается по **`strings.ToLower`**, чтобы варианты вроде `Extended Connect Not Supported` не ошибочно попадали в **`h3_extended_connect`** через общий матч **`Extended CONNECT`**. Юнит: **`TestClassifyMasqueFailure`**. В **`transport/masque/transport.go`**: **`normalizeTCPTransport`** возвращает константы **`option.MasqueTCPTransport*`** (вместо литералов + `"auto"`); ветви **`DialContext`** / **`directSession`** / **`tcpCapable`** переведены на те же константы; порог **`masqueUDPWriteMax`** согласован с **`connectIPUDPWriteHardCap`** без локального дубля **`1152`**.

**Ранее:** **`directSession.DialContext`:** порядок `switch tcpTransport` — сначала **`connect_ip`** (TUN-only), затем **`connect_stream`**, затем общий `net.Dialer`.

**Ранее:** **`coreSession.ListenPacket` при `transport_mode: connect_ip`:** открытие CONNECT-UDP-моста больше **не зависит** от ранней валидации `resolveDestinationHost(destination)` (узел CONNECT-IP не использует SOCKS-destination для `openIPSessionLocked`). Иначе нулевой/невалидный `destination` ложно блокировал `ListenPacket`. Юнит: **`TestListenPacketConnectIPSkipsDestinationResolution`**.

**Ранее:** **`ClassifyMasqueFailure`:** явная ветка для меток H2 **датаплейна** (`masque h2 dataplane`, `masque connect-ip h2 dataplane`, а также защита от опечатки **`masque h2: dataplane` / `masque connect-ip h2: dataplane`**) → ключ **`other`**, до сопоставления **`masque h2:`**/**`masque connect-ip h2:`** с **`h2_masque_handshake`**, чтобы метрики/WARP-портная классификация не смешивали поднятый туннель с ошибками рукопожатия. Тесты в **`TestClassifyMasqueFailure`**.

**Ранее:** **Разделение handshake vs датаплейн в текстах ошибок H2 CONNECT-UDP (клиент и сервер relay):** после успешного Extended CONNECT ошибки парсинга/записи капсул и oversized DATAGRAM оборачиваются в **`masque h2 dataplane connect-udp …`**, чтобы **не** содержать подстроку **`masque h2:`** и не включать **`http_layer_fallback`** / **`IsMasqueHTTPLayerSwitchableFailure`** на уже поднятом туннеле (ложный H3↔H2 при битом потоке). Ошибки рукопожатия (**`RoundTrip`**, статус CONNECT, конфиг **`TCPDial`**) по-прежнему с **`masque h2:`**. Юниты: **`TestIsMasqueHTTPLayerSwitchableFailure`**, **`TestClassifyMasqueFailure`**.

**Ранее:** **Канонический префикс `masque h2:`** для ошибок **рукопожатия** H2 (**CONNECT-UDP** / **CONNECT-stream** / статусы HTTP и т.п.), чтобы они попадали в **`ClassifyMasqueFailure` → `h2_masque_handshake`** и **`IsMasqueHTTPLayerSwitchableFailure`**; исторически строки вида **`masque h2 tcp:`** без подстроки **`masque h2:`** ломали fallback.

**Ранее (`ensureH2UDPTransport`):** сообщение об отсутствии **`TCPDial`** — префикс **`masque h2:`**; тест **`tcp dialer is not configured`**.

**Ранее:** **`IsMasqueHTTPLayerSwitchableFailure`:** добавлены **`use of closed network connection`**, Windows **`pipe is being closed`**, ранее — **`broken pipe` / `forcibly closed`** и тексты **`http2:`**/`GOAWAY`; тест **`TestIsMasqueHTTPLayerSwitchableFailure`**.

**Ранее:** **CONNECT-UDP и CONNECT-stream на HTTP/3:** **`dialUDPAddr`** (ветка QUIC и тестовый **`udpDial`**) и **`dialTCPStreamHTTP3`** пишут **`masque_http_layer_attempt` / `masque_http_layer_chosen`** с **`layer=h3`** (для TCP — **`tcp_stream=1`**), паритетно путям H2 из **`h2_connect_udp.go` / `h2_connect_stream.go`**. Вспомогательная функция **`masqueUDPExpandedURLAuthority`** (+ юнит **`TestMasqueUDPExpandedURLAuthority`**) задаёт **`target`** в логах по развёртыванию шаблона UDP.

**Ранее (последняя правка):** **CONNECT-IP HTTP/3:** в **`dialConnectIPAttempt`** добавлены **`masque_http_layer_attempt` / `masque_http_layer_chosen`** с **`layer=h3`** и **`connect_ip=1`** — паритет с **`dialConnectIPHTTP2`** и требованиями §8 к структурированным меткам без утечки секретов.

**Ранее (последняя правка):** **`type: masque` (не WARP):** при старте рантайма **`EffectiveMasqueClientHTTPLayer`** и запись TTL-кэша успешного слоя используют **один и тот же** нормализованный entry-порт (`DialPortOverride`), как уже делалось в **`warp_masque`**, чтобы **`http_layer: auto`** + **`http_layer_cache_ttl`** не расходились с фактическим **`ServerPort`** сессии. Документация пользователя: в [`docs/masque/MASQUE-SINGBOX-CONFIG.md`](docs/masque/MASQUE-SINGBOX-CONFIG.md) добавлен подпункт про **`http_layer*`**.

**Ранее (последняя правка):** **H2 Extended CONNECT upload (`connect-udp`, `connect-stream`):** тело запроса с **`io.PipeReader`** обёрнуто в **`h2ExtendedConnectUploadBody`** (no-op **`Close`**) — паритет с **`connect-ip-go` `DialHTTP2`**, чтобы **`net/http`** HTTP/2 не рвал upload из **`cleanupWriteRequest`** при half-close ответа. В **`h2ConnectUDPPacketConn.Close`** явно закрывается **`reqPipeR`**. В **`streamConn.Close`** для пути H2 закрывается **`h2UploadPipe`**. Юниты: **`TestH2ExtendedConnectUploadBodyCloseIsNoop`**, **`TestH2ConnectUDPPacketConnCloseClosesReqPipeReader`**. (Ранее: на ошибочных путях **`dialUDPOverHTTP2`** уже закрывались оба конца пайпа.)

**Ранее (последняя правка):** Стендовый раннер: сценарий **`masque_http_layer_auto_cache_smoke`** + конфиг **`configs/masque-client-http-layer-auto-cache-fallback.json`** (`http_layer: auto`, **`http_layer_cache_ttl`**, **`http_layer_fallback`**) на том же контуре, что **`masque_h2_smoke`**, чтобы не регрессировали валидация JSON и TTL-кэш/`EffectiveMasque*` при E2E.

**Ранее (§13):** **MASQUE server `handleTCPConnectRequest`:** если задан заголовок **RFC 8441 `:protocol`**, принимается только **`HTTP/2`** … Тест **`TestServerHandleTCPConnectRequestRejectsMisusedExtendedProtocol`**.

**Ранее в §13 (кратко):** **`resetHopTemplates` / `resetTCPHTTPTransport`** и общий **`ipHTTP`/`tcpHTTP`** на H3 (**`TestResetHopTemplatesClearsSharedIPH3Refs`**, **`TestResetTCPHTTPTransportClearsSharedIPH3Refs`**); **H3 CONNECT-stream:** контекст запроса = контекст диала (**`dialTCPStreamHTTP3`**); **`coreSession.Capabilities.Datagrams`** от **`currentUDPHTTPLayer`** (**`TestCoreSessionCapabilitiesDatagramsTracksHTTPLayerOverlay`**).

**Не по текущей итерации:** по §15 — профилировать высокоскоростной CONNECT-UDP/CONNECT-IP на стенде раннера только после явного запроса.

| Поле | Значение |
|------|----------|
| Последнее | **Сервер TCP CONNECT:** непустой **`:protocol`** только **`HTTP/2`** (пусто = совместимость с H3 CONNECT-stream) — **`TestServerHandleTCPConnectRequestRejectsMisusedExtendedProtocol`**. **`resetHopTemplates`:** общий **`ipHTTP`/`tcpHTTP`** на H3 — **`TestResetHopTemplatesClearsSharedIPH3Refs`**. **`resetTCPHTTPTransport`**, **H3 CONNECT-stream** — см. выше. |
| | сервер **`listen_port: 0`:** **`masqueDynPortBindAttempts`** **512** (TCP sibling bind на Windows excluded ranges после успешного UDP). |
| | **`IsMasqueHTTPLayerSwitchableFailure`:** префикс **`masque connect-ip h2:`** вместо **`masque connect-ip`** (**`TestIsMasqueHTTPLayerSwitchableFailure`**). |
| | **`maybeRecordHTTPLayerCacheSuccess`:** TTL **`http_layer:auto`** только на entry hop; inner hop после **`advanceHop`** не вызывает **`RecordMasqueHTTPLayerSuccess`** (**`TestMaybeRecordHTTPLayerCacheSuccessSkipsInnerHop`**). |
| | **`ClassifyMasqueFailure`:** подстрока **`extended connect not supported`** → **`h2_extended_connect_rfc8441`** (клиент **`x/net/http2`**, RFC 8441); **`warp_masque`** не помечает это как ротуемый по порту сбой (**`TestClassifyMasqueFailure`**). |
| | **CONNECT-IP (форк connect-ip-go), капсула DATAGRAM на потоке:** вместо **`io.ReadAll`** — потолок **`maxHTTPDatagramCapsulePayload`** и сброс хвоста капсулы при превышении (паритет с **H2 CONNECT-UDP**). |
| | **H2 CONNECT-UDP клиент (`h2ConnectUDPPacketConn.ReadFrom`):** ограничено чтение тела **DATAGRAM**-капсулы тем же потолком, что **`ServeH2ConnectUDP`** (`h2ConnectUDPMaxCapsulePayload`); при превышении остаток капсулы сбрасывается в `Discard` (контракт `ParseCapsule`), ошибка с явным текстом. Общая константа вместо дубля `h2ConnectUDPServerReadMTU`. Тесты: `TestH2ConnectUDPPacketConnRejectsOversizedDatagramCapsule`, `TestH2ConnectUDPPacketConnReadsBoundarySizedDatagramCapsule`. |
| | **`ServeH2ConnectUDP` (сервер):** раньше тело DATAGRAM читалось с `LimitReader(..., max)` без распознавания превышения — при peer с телом капсулы > max возможна поломка выравнивания потока капсул. Сейчас паритет с клиентом: лимит `max+1`, при перевыходе — `Discard` остатка тела капсулы и ошибка. Тест: `TestServeH2ConnectUDPRejectsOversizedDatagramCapsule`. |
| | **TTL-кэш `http_layer`:** при **Effective**/`warp_masque` ключ учитывает **фактический порт** (`rtPort` / параметр dial override) против entry-hop из chain (`TestMasqueHTTPLayerCacheDialPortOverrideSeparatesEntries`); запись успешного слоя — **`HTTPLayerSuccess(layer, HTTPLayerCacheDialIdentity)`** с edge из живого `coreSession` после `advanceHop` (поле «Последняя правка кода» в §13, `TestRecordMasqueHTTPLayerSuccessDoesNotAliasInnerHopToEntryKey`). |
| | **TTL-кэш `http_layer`:** просроченные ключи **удаляются** при lookup (`httpLayerProcessCache.get`), а не копятся в map до рестарта процесса (**`TestEffectiveMasqueHTTPLayerTTLExpiryDropsCacheEntry`**). |
| | **`validateMasqueOptions`:** положительный `http_layer_cache_ttl` при `http_layer` не `auto` → ошибка; тесты `TestValidateMasqueOptionsRejectsCacheTTLWithoutAutoHTTPLayer`, `TestValidateMasqueOptionsAllowsCacheTTLWithAutoHTTPLayer`. |
| | **`streamConn` / CONNECT-stream (H2 и H3):** в **`Read`** не подменять **`io.EOF`** читателя на `ErrTCPConnectStreamFailed`+`Cause(dial)` — иначе нормальное закрытие потока при уже отменённом короткоживущем контексте диала выглядит как сбой транспорта. Relay-контур, где продуманная отмена идёт **через тот же** `DialContext`, сохраняет прежнее оборачивание **`Write`**/не-EOF **`Read`** (регрессии ловят `TestDialTCPStreamInProcessHTTP3ProxyRelayPhase*`). Юнит: **`TestStreamConnReadKeepsEOFWhenDialCtxCanceled`**. |
| | **CONNECT-IP H2 (`connectip.DialHTTP2`):** контекст **краткоживущего** `OpenIPSession` нельзя вешать на `http.Request` — после `defer cancel()` net/http рвёт долгий CONNECT-стрим (`handling stream failed: context canceled`, лавина `session_write_packet` / `closed`). Запрос переведён на `context.WithoutCancel(ctx)` (плюс ранняя проверка `ctx.Done()`). Аналогично **CONNECT-UDP** и **CONNECT-stream** H2: `dialUDPOverHTTP2`, `dialTCPStreamH2`. **Раннер `masque_h2_connect_ip`** (1 MiB, hash) зелёный. |
|  | **Раннер `masque_h2_connect_ip`:** Compose-клиент `configs/masque-client-connect-ip-h2.json` (`transport_mode: connect_ip`, `http_layer: h2`); переиспользует тот же пайплайн `run_tcp_ip`, что и H3 CONNECT-IP, с отличным только клиентским JSON. |
|  | **`ListenPacket` (overlay H2):** same-hop первый повтор после неуспешного CONNECT-UDP сбрасывает **`h2UdpTransport`** (`resetH2UDPTransportLockedAssumeMu`), паритет с **`openIPSessionLocked`** после churn на H2. Тест-хук только для пакета: **`h2UDPConnectHook`** → **`TestListenPacketH2UDPTransportChurnBeforeHopPivot`**. |
|  | **MASQUE server `mode: server`:** параллельно с QUIC+H3 слушает **TLS+TCP+HTTP/2** на том же порту (тот же `ServeMux`): клиентский `http_layer: h2` и стендовый раннер получают реальный E2E. Для SETTINGS **ENABLE_CONNECT_PROTOCOL** (RFC 8441) процесс **`sing-box`/net/http требует** `GODEBUG=http2xconnect=1` — в compose для `masque-server-core` задано по умолчанию. Динамический `listen_port: 0`: TCP-биндинг синхронизируется по **реальному** порту после `ListenPacket`. Раннер H2: **`masque_h2_smoke`** (`configs/masque-client-h2.json`), **`masque_h2_connect_ip`** — CONNECT-IP по H2 / TUN UDP (`configs/masque-client-connect-ip-h2.json`). |
|  | **`openIPSessionLocked` (CONNECT-IP / H3):** parity с `ListenPacket`: после первой пары попыток **`dialConnectIPAttempt` + fallback** на том же hop сбрасывается кэш **`ipHTTP`/`ipHTTPConn`** (`resetIPH3TransportLockedAssumeMu`), второй ряд попыток (+ optional fallback) — иначе «Extended CONNECT» мог проявиться только после fresh `openHTTP3ClientConn`, минуя **`tryHTTPFallbackSwitch`**. Тот же блок в теле цикла **`advanceHop`**. Тест: `TestOpenIPSessionHTTPFallbackRunsAfterIPH3ReconnectDialSwitchableFailure` (хук `dialConnectIPAttemptHook`). |
|  | **`ListenPacket`:** после внутреннего same-hop ретрая (пересборка QUIC `udpClient`) добавлен вызов **`tryHTTPFallbackSwitch` + повторный dial** по тому же паттерну, что на первом входе и во внутреннем hop-цикле — иначе ошибка уровня handshake/Extended CONNECT, проявившаяся только после churn, вела сразу к `advanceHop` без попытки смены **H3↔H2**. `TestListenPacketHTTPFallbackRunsAfterReconnectDialSwitchableFailure`. |
|  | **`dialUDPAddr`:** хук **`udpDial`** (только юнит-тесты, в проде nil) раньше шёл первым и перехватывал **все** вызовы, в том числе при уже выбранном overlay **H2**, из‑за чего после `http_layer_fallback` тестовый стенд не выполнял реальный `dialUDPOverHTTP2`. Теперь при `udpHTTPLayer==h2` всегда **`dialUDPOverHTTP2`**, а `udpDial` — только для QUIC-ветки (**h3**). |
|  | **`openIPSessionLocked` (H2):** паритет с веткой `resetIPH3TransportLockedAssumeMu` — после неуспеха на overlay **H2** сбрасывается общий **`h2UdpTransport`** (`resetH2UDPTransportLockedAssumeMu`) и выполняется повторная попытка **CONNECT-IP** + опциональный `tryHTTPFallbackSwitch` на том же hop, до ухода в `advanceHop`. Тест: `TestOpenIPSessionH2TransportChurnBeforeHopPivot`; обновлён счётчик в `TestOpenIPSessionHTTPFallbackRunsAfterIPH3ReconnectDialSwitchableFailure` (доп. редиал H2 после churn). |
|  | **`dialTCPStream`:** при `http_layer_fallback: true` — после первой попытки `tryHTTPFallbackSwitch` добавлен блок как у `ListenPacket`: `resetTCPHTTPTransport` → повторная попытка CONNECT-stream → второй вызов `tryHTTPFallbackSwitch` до продвижения hop; чтобы не удваивать уже тройной внутренний ретрай H3 CONNECT-stream при **выключенном** fallback, churn выполняется **только** при `httpLayerFallback`. Тест: `TestDialTCPStreamHTTPFallbackRunsAfterReconnectRoundTripSwitchableFailure` (перехватчик `tcpRoundTripper` используется и для overlay H2). |
|  | *(ранее)* **`resetHopTemplates` больше не сбрасывает `udpHTTPLayer` на `MasqueEffectiveHTTPLayer`.** Hop-цикл UDP/IP: `tryHTTPFallbackSwitch` + `wireMasqueUDPClientForOverlayLocked`. **`dialTCPStream`:** продвижение `hopOrder` + `dialTCPStreamH2`/`ensureH2UDPTransport`/`isRetryableTCPStreamError`. |
|  | **`resetHopTemplates`:** при смене hop закрывается и обнуляется **`tcpNetstack`** (gVisor поверх CONNECT-IP), как при `tryHTTPFallbackSwitch` / `Close`; иначе после `ipConn` teardown оставался живой стек, привязанный к старой сессии. Тест: `TestResetHopTemplatesClearsTCPNetstack`. |
|  | **`IsMasqueHTTPLayerSwitchableFailure`:** в матчинг добавлены типичные тексты **HTTP/2** (`GOAWAY`, `RST_STREAM` / `rststream`, `stream error`), чтобы `http_layer_fallback` мог переключить оверлей при обрыве H2-потока/соединения, а не только при TLS/QUIC/substring `http2:`. Тест: `TestIsMasqueHTTPLayerSwitchableFailure`. |
|  | **`http_layer_fallback` + `httpFallbackConsumed`:** после **успешного** поднятия датаплейна (CONNECT-UDP / CONNECT-IP / CONNECT-stream) защёлка сбрасывается (`resetHTTPFallbackBudgetAfterSuccess`), иначе второй switchable сбой на **том же hop** не мог бы снова сменить H2↔H3 до `advanceHop`. Тест: `TestHTTPFallbackBudgetResetsAfterSuccessfulUDPDial`. |
|  | **`ClassifyMasqueFailure`:** отдельные ключи **`h2_masque_handshake`**, **`h2_extended_connect_rfc8441`** (строки ошибок H2 / RFC 8441 SETTINGS); для `IsRetryableWarpMasqueDataplanePort` они в одном классе с fixed-config отказами Extended CONNECT / datagrams. |
|  | **H2 CONNECT-UDP `PacketConn`:** реализованы **`SetDeadline` / `SetReadDeadline` / `SetWriteDeadline`** через общий `connDeadlines` (как у UDP-моста CONNECT-IP): проверка на входе **`ReadFrom` / `WriteTo`** и в цикле чтения капсул; при просроченном дедлайне — `os.ErrDeadlineExceeded`. Глубокая отмена блокирующего чтения из H2-тела без доработки обёртки читателя не делается. Тесты: `TestH2ConnectUDPPacketConnReadDeadlineElapsed`, `TestH2ConnectUDPPacketConnWriteDeadlineElapsed`. |
|  | **Сервер CONNECT-UDP по HTTP/2:** `qmasque.Proxy` заточен на **HTTP/3 QUIC** (`http3.HTTPStreamer` + `ReceiveDatagram`/`SendDatagram`). Для входящего **RFC 9297 DATAGRAM** на CONNECT-потоке добавлена ветка **`handleMasqueConnectUDP`** → **`TM.ServeH2ConnectUDP`** ([`transport/masque/h2_connect_udp_server.go`](hiddify-core/hiddify-sing-box/transport/masque/h2_connect_udp_server.go)): капсулы на теле запроса/ответа как у клиента `dialUDPOverHTTP2`. **Раннер `masque_h2_smoke`** (1 MiB) зелёный. |
|  | **`third_party/masque-go` — URI match:** помимо `:protocol` для H2 ([`request.go`](hiddify-core/hiddify-sing-box/third_party/masque-go/request.go)), `ParseRequest` сопоставляет шаблон с несколькими кандидатами (`URL`, path+query, `RequestURI`, `https://authority+RequestURI`), иначе 400 при пустых `target_*`. Паритет **CONNECT-stream** на сервере: расширен список кандидатов в **`parseTCPTargetFromRequest`** ([`endpoint_server.go`](hiddify-core/hiddify-sing-box/protocol/masque/endpoint_server.go)). |
|  | **Сервер CONNECT-IP по H2 (`masque_h2_connect_ip`):** обработчик не должен возвращаться сразу после `RoutePacketConnectionEx` — на QUIC/H3 поток переводится в *hijacked* через `http3.HTTPStreamer` и sing-box может завершить `ServeHTTP` без закрытия туннеля; на **HTTP/2** hijack нет → при раннем return `net/http` закрывает CONNECT-поток, релей продолжается уже на «мертвом» writer (**`closed`**, `bridge_write_err`/`session_write_packet`). Исправление: **`routeMasqueConnectIPBlocked`** держит handler до **`onClose`** (как `ServeH2ConnectUDP`). |
|  | **`dialUDPAddr` + тестовый `udpDial`:** при успешной подмене QUIC-диала (только юниты) вызывается **`HTTPLayerSuccess(h3)`** и сброс **`httpFallbackConsumed`**, как у пути **`client.DialAddr`** — иначе `http_layer: auto` + TTL-кэш не фиксировали выбранный слой при перехвате. |
|  | **MASQUE server (`listen_port: 0`):** на Windows (и подобных политиках) эфемерный UDP-порт иногда **нельзя** сразу занять TCP-listener’ом на том же номере — старт H2 рядом с QUIC ломал тесты/локальный старт. Цикл до **512** попыток «UDP + валидация + TCP» (с **`masqueTCPBindFailureRetryable`** до полного набора успешной пары UDP+TCP); фиксированный `listen_port` без ретраев. Юнит: **`TestMasqueTCPBindFailureRetryable`**. |

Ссылки на код: [`protocol/masque/endpoint_server.go`](hiddify-core/hiddify-sing-box/protocol/masque/endpoint_server.go), [`option/masque.go`](hiddify-core/hiddify-sing-box/option/masque.go), [`protocol/masque/http_layer_cache.go`](hiddify-core/hiddify-sing-box/protocol/masque/http_layer_cache.go), [`transport/masque/h2_connect_udp.go`](hiddify-core/hiddify-sing-box/transport/masque/h2_connect_udp.go), [`transport/masque/h2_connect_udp_server.go`](hiddify-core/hiddify-sing-box/transport/masque/h2_connect_udp_server.go), [`transport/masque/h2_connect_stream.go`](hiddify-core/hiddify-sing-box/transport/masque/h2_connect_stream.go), [`transport/masque/h2_connect_ip.go`](hiddify-core/hiddify-sing-box/transport/masque/h2_connect_ip.go).

---

## 14. Тестирование через раннер MASQUE (обязательный контур качества)

Проверка и регрессия для слоя **H2/H3**, **warp_masque** и смежной логики должны быть **реализованы и поддерживаются через** Python-раннер стенда [`masque_stand_runner.py`](experiments/router/stand/l3router/masque_stand_runner.py) и связанные сценарии/матрицы (Docker compose, см. runbook).

**Не допускать:** закрывать задачу «минимальной» реализацией только в юнит-тестах; объявлять соответствие RFC **поверхностно**, не покрывая критический путь (Extended CONNECT, капсулы/датаграм-плоскость, ошибки handshake, fallback, объёмы как в канонических сценариях раннера); бросать фазу **на полпути** без обновления раннера или без артефакта прогона **без секретов**.

Каждая завершённая фаза из §10 должна иметь **явный** след в автоматизации стенда: новый или доработанный сценарий, параметры конфига, пороги/пейсинг при необходимости — чтобы последующие изменения не ломали H2 и H3 незаметно.

---

## 15. Следующий горизонт после стабильного H2 (аудит и оптимизация эндпоинта)

Когда **все** целевые задания по H2 для **masque** и **warp_masque** выполнены, режим **несколько раз** перепроверен на **полноту** (контракт sing-box + нормативка MASQUE) и на **качество** (стабильность, ошибки, fallback, стенды), имеет смысл перейти к **обзорному** упорядочиванию **MASQUE endpoint в sing-box в целом**: искать узкие места и регрессии вне узкой задачи «только H2».

**Приоритетный класс проблем:** **CONNECT-UDP** и **CONNECT-IP** на **высоких скоростях** (goodput, обрывы «хвоста», очереди, размеры датаграмм/капсул, взаимодействие с буферами сокета и QUIC/H2 flow control). При обнаружении слабых мест — точечная правка или настройки по умолчанию (**без фанатизма**): каждое изменение должно быть **обосновано** замерами или минимально воспроизводимым сценарием на стенде.

**Ограничение охвата:** оптимизация должна затрагивать слои, которые реально покрывает **Docker-раннер** и смежные смоки; не расползаться в произвольный рефакторинг всего ядра. Итерации — как прежде: гипотеза → патч → прогон раннера → короткий отчёт **без секретов**.

---
