# AGENTS — MASQUE / WARP_MASQUE

## 1) Mission

Цель: довести `masque` endpoint в `hiddify-core/hiddify-sing-box` до production-качества и RFC-сходимости (`connect_stream`, `connect_udp`, `connect_ip`) по измеримым артефактам CI/стенда (`masque-gates`, `experiments/router/stand/l3router`).
Основной фокус: объективно повышать скорость на Docker-стенде **без потерь/дрейфа хеша/регрессий** до реальной границы тракта; искать и устранять проблемы в hot path и соседних слоях MASQUE endpoint, делать его более прозрачным.

**Windows:** каноничный запуск стенда — через **Docker Desktop (WSL2)**; стенд и артефакты те же, что в CI. Нет прогона — не «запрет инфраструктуры», а незапущенный демон или пропуск шага.


Важно: вместо постоянного поддержания документации в "актуальном виде", приоритет следует отдавать реальным действиям с кодом.

## 2) Non-negotiables

- Цикл: сигнал → код → тест/стенд → фиксация артефакта → следующий шаг.
- Fail (`loss`, `timeout`, `budget_exceeded`, hash drift, `throughput_target_unmet`) — разбирать по boundary слоя, без fake-green.
- Не ослаблять пороги/таймауты/валидации ради PASS.
- Для Docker e2e MASQUE единственно верный путь проверки — compose-стенд + `masque_stand_runner.py`/`masque_runtime_*` по порядку из `masque-gates`; альтернативные «облегчённые» проверки считать вспомогательными и недостаточными для выводов о прод-качестве.
- Источник истины при расхождении: **код → `hiddify-core/.github/workflows/ci.yml` (job `masque-gates`) → `IDEAL-MASQUE-ARCHITECTURE.md` → `docs/masque/*` → этот файл** (`AGENTS.md` — дорожная карта и операторские якоря, не дубликат RFC).
- `masque_or_direct` только с `fallback_policy=direct_explicit`.
- Рефакторинг допустим агрессивно при сложной диагностике, если не ломает контракты/RFC и подтверждается тестами/стендом.
- Оптимизация hot path обязательна, когда узкое место очевидно и подтверждается наблюдаемостью (CPU/очереди/дропы/таймауты); «оптимизации на веру» запрещены.
- Submodule: правки ядра — коммит в `hiddify-core`; в родителе `hiddify-app` — обновление **SHA** submodule.

## 3) Read First (mandatory)

**Пути:** `IDEAL-MASQUE-ARCHITECTURE.md` и `docs/masque/*` — в **корне `hiddify-app`**; пути вида `hiddify-core/docs/...` — внутри submodule.

Перед первой правкой итерации:

1. `hiddify-core/.github/workflows/ci.yml` → job **`masque-gates`** (в Setup Go сейчас `go-version: ^1.25.6`).
2. `IDEAL-MASQUE-ARCHITECTURE.md` — режимы, MTU/TUN/datagram ceiling, носители CONNECT-UDP / stream / CONNECT-IP (глава 4), packet-plane (глава 5).
3. `docs/masque/AGENT-RFC-CI-CONTRACTS.md`
4. `docs/masque/AGENT-LAYER-SOURCE-OF-TRUTH.md` — **полная карта слоёв и расширенный triage** (peer-split, счётчики, порядок job).
5. `docs/masque/AGENT-TEST-AND-STAND-RUNBOOK.md`
6. `docs/masque/AGENT-CI-REPLAY-CHEATSHEET.md`
7. Пороги и имена артефактов: `hiddify-core/docs/masque-perf-gates.md`; lifecycle CONNECT-IP: `hiddify-core/docs/masque-connect-ip-staged-closure.md` (**TCP через netstack при `transport_mode=connect_ip`**, не `tcp_transport=connect_ip`).
8. Опционально: `docs/masque/AGENT-HANDOFF-TEMPLATE.md`

## 4) Working protocol (mandatory)

1. Взять сигнал: `experiments/router/stand/l3router/runtime/*.json`, `go test`, лог CI.
2. Boundary + гипотеза (см. раздел 6 ниже и `AGENT-LAYER-SOURCE-OF-TRUTH`).
3. Править **один** целевой слой за итерацию.
4. Прогон: релевантные `go test` + при необходимости стенд/validator (порядок как в `masque-gates`).
5. Обновить разделы **7** (текущий ход) и **8** (следующие шаги) этого файла; в §7 поле **`hiddify-core` `HEAD`** — вывод **`git -C hiddify-core rev-parse HEAD`** из корня монорепо **`hiddify-app`** (или **`git rev-parse HEAD`** из каталога submodule).
6. **Автономный цикл:** не блокировать работу запросами к оператору; при отсутствии внешнего сигнала брать задачу из **§8** или чеклист **§3** / быстрый `go test` из **§6**; итерацию закрыть перезаписью **§7–§8** без журнала правок в этом файле.

## 5) Definition of Done

- Нет регрессий mode/fallback/lifecycle/scoped-контрактов.
- Unit/race/integration по затронутым пакетам — PASS.
- Стенд/validator — PASS для целевой матрицы.
- Изменение поведения/RFC отражено в коде и тестах там, где нужно.

## 6) Architecture addendum (кратко)

**RFC (номера):** MASQUE **9298**, HTTP Datagrams **9297**, CONNECT-IP **9484**. Норматив реализации — **IDEAL-MASQUE-ARCHITECTURE.md** + код.

**Быстрый поиск по коду (границы):** клиент QUIC/H3, `coreSession`, фабрики сессий — **`transport/masque/transport.go`**; HTTP/3 CONNECT/stream/UDP/CONNECT-IP на **сервере** — **`protocol/masque/endpoint_server.go`**; распаковка/валидация опций после JSON — **`option/masque.go`** и **`protocol/masque/endpoint.go`** (`validateMasqueOptions`, lifecycle outbound).

**Навигация `protocol/masque`:** outbound, lifecycle клиента и `validateMasqueOptions` — **`endpoint.go`** / **`endpoint_warp_masque.go`**; входящий HTTP/3 (CONNECT/stream/UDP/CONNECT-IP) — **`endpoint_server.go`**; регистрация типов endpoint — **`register.go`**; WARP/control surface по сравнению с generic **`masque`** — **`warp_control_adapter.go`**.

**Новые/изменённые поля JSON конфига MASQUE:** править симметрично **`option/masque.go`** (структуры, теги, defaults) и **`protocol/masque/endpoint.go`** (`validateMasqueOptions`, lifecycle, отказ «не поддерживается»); иначе дрейф между распаковкой и рантаймом без компиляторной ошибки.

**Корень sing-box:** `hiddify-core/hiddify-sing-box/` (сборка/тесты только отсюда; `go.mod` — модуль `github.com/sagernet/sing-box`). Patched QUIC: `replace/quic-go-patched/`. **Монорепо `hiddify-app`:** если нет **`hiddify-core/hiddify-sing-box/go.mod`**, выполнить **`git submodule update --init hiddify-core`** — иначе **`go test`**, сборка артефакта и compose-стенд не запускаются. **Относительный путь к стенду/артефактам:** из **`hiddify-sing-box`** каталог экспериментов — **`../../experiments/router/stand/l3router/...`** (ровно два **`..`** до корня **`hiddify-app`**; три **`../`** выходят за монорепо и ломают скрипты на Windows/Mac). То же правило для ручного **`sing-box check -c`**: cwd **`hiddify-sing-box`**, до конфигов — **`../../experiments/...`**; при ошибке «config not found» сначала пересчитать **`..`**, затем при необходимости использовать абсолютный путь внутри **`hiddify-app/experiments/...`**.

**`sing-box check`:** читает конфиг и строит инстанс через **`box.New`** — отрабатывают распаковка **`option`** и создание endpoint (в т.ч. **`validateMasqueOptions`**), без **`run`** и без Docker; нужен бинарь, собранный с **`with_masque`**, иначе типы MASQUE не зарегистрированы. **Нативный бинарь достаточен:** `check` не требует Linux-артефакта; **`GOOS=linux`** нужен для compose/Docker и паритета с CI-сборкой, а не для валидации JSON. **Полный wiring после `check`:** реализация **`box.New`** — **`hiddify-core/hiddify-sing-box/box.go`** (пакет **`box`**): создание **`endpoints[]`**, inbound/outbound/service и **`route.NewRouter`**; при «`check` OK, трафика нет» смотреть **`route`/теги/outbound**, а не только MASQUE-валидацию. Тип **`adapter.Endpoint`** (в т.ч. MASQUE как endpoint-outbound): **`adapter/endpoint.go`** — **`Lifecycle`** + **`Outbound`** + `Type()`/`Tag()`; **`Outbound`** — **`adapter/outbound.go`** (в т.ч. **`N.Dialer`**, **`Network()`**, **`IsReady()`**). Вход в dataplane MASQUE с маршрута: **`DialContext`** / **`ListenPacket`** в **`protocol/masque/endpoint.go`** → **`common/masque` (`Runtime`)** → **`transport/masque`**.

**Жизненный цикл wiring:** конфиг распаковывается в `option` (`endpoints[]` через `option/endpoint.go` + **`EndpointOptionsRegistry`** из сборки с `include` и тегом **`with_masque`**). Endpoint создаётся фабрикой из **`protocol/masque/register.go`**; общий контур sing-box вызывает **`adapter.Endpoint`** — `Network()`, `Start(adapter.StartStage)`, `Close()`. Отдельного inbound-типа `masque` нет: трафик — через **`endpoints[]` + `route` по tag** (`route.rules[].outbound`, `route.final` в том же пространстве имён tag, что и `endpoints[].tag`). В **`box.New`** (`hiddify-core/hiddify-sing-box/box.go`) после DNS-router сначала создаются **все** `endpoints[]`, затем inbounds и outbounds; при создании endpoint с непустым `tag` в контекст подмешивается **`adapter.InboundContext{Outbound: tag}`** — для фабрик/логирования это тот же идентификатор, что в правилах маршрута. Потребитель dataplane — правила **`route`/DNS/route rules** без подразумеваемого «обхода» MASQUE; подозрение на обход закрывается негативными шагами **`masque-gates`** (**`masque_runtime_ci_gate_asserts.py`**), см. ниже про parity validator.

**Регистрация типов:** `include/masque.go` (+ `with_masque`) → `masque.RegisterEndpoint` / `RegisterWarpMasqueEndpoint`; без тега — `include/masque_stub.go` (MASQUE endpoint types не линкуются — локальные прогоны без **`-tags with_masque`** не паритетны **`masque-gates`**). Фабрики: **`NewEndpoint`** (`endpoint.go`), **`NewWarpEndpoint`** (`endpoint_warp_masque.go`). Опции: **`option/masque.go`**; generic **`masque`** vs **`warp_masque`** — см. `warp_control_adapter.go`, ADR `hiddify-core/docs/masque-warp-architecture.md`. **JSON `endpoints[].type`:** литералы **`masque`** / **`warp_masque`** (константы **`C.TypeMasque`** / **`C.TypeWarpMasque`**, **`constant/proxy.go`**; привязка к опциям — **`protocol/masque/register.go`**).

**Точка связки типа конфига с опциями и конструктором:** **`protocol/masque/register.go`** — **`endpoint.Register[option.MasqueEndpointOptions](registry, C.TypeMasque, NewEndpoint)`** и симметрично **`[option.WarpMasqueEndpointOptions]`** / **`C.TypeWarpMasque`** / **`NewWarpEndpoint`**; при смене строки типа или Go-структуры опций без синхронного обновления здесь возможен рассинхрон до рантайма (частично ловится **`sing-box check`** через распаковку **`endpoints[]`**).

**`tcp_stream` / `connect_udp` vs CONNECT-IP:** отдельные ветки в **`transport/masque/transport.go`** и relay/stream в **`endpoint_server.go`** (H3 CONNECT / UDP datagram); не смешивать с **`connectIPNetPacketConn`**. Матрица носителей — **IDEAL**, глава 4 (таблица три семантики). **Guards в `coreSession`:** **`ListenPacket`** переводит в CONNECT-IP (`openIPSessionLocked` / IP plane) только при **`transport_mode=connect_ip`** (нормализация строки на границе transport); при **`auto`/`connect_udp`** — CONNECT-UDP и **`template_udp`**, без IP plane. **`DialContext`:** релейный TCP — **`tcp_transport=connect_stream`** → **`dialTCPStream`**; **`tcp_transport=connect_ip`** на клиенте отсекается в **`endpoint.go`**, до core не доходит. **CONNECT-UDP (клиент):** слишком большой **`WriteTo`** нарезается через **`masqueUDPDatagramSplitConn`** до **`masqueUDPWriteMax`** (связь с **`datagramCeiling`**) — см. **IDEAL** §1 контракт MTU/payload.

**`fallback_policy` / TCP mode vs выбор носителя:** в **`coreSession`** поля **`fallback_policy`** и **`MasqueTCPMode` сами по себе не переключают** CONNECT-UDP ↔ CONNECT-IP; это задаётся **`transport_mode`**, валидацией и слоем выше (**route/outbound**, consumer). Связки **`masque_or_direct`**, chain и изоляция **`warp_masque`** — **`hiddify-core/docs/masque-warp-architecture.md`**, **IDEAL** глава 1.

**Вертикаль клиента (CONNECT-IP):** `protocol/masque/endpoint.go` (валидация `validateMasqueOptions`, lifecycle) → `common/masque/runtime.go` (`Runtime`/ретраи, без router) → `transport/masque/transport.go` (**`coreSession`**: QUIC/H3, `ListenPacket` / `DialContext` / `OpenIPSession`, ветки CONNECT-UDP vs `connect_ip`) → `third_party/connect-ip-go` + `third_party/masque-go` (RFC bootstrap CONNECT/stream/UDP). **TCP в TUN-only:** netstack **`transport/masque/netstack_adapter.go`** при **`transport_mode=connect_ip`**; клиент **`tcp_transport=connect_ip`** запрещён.

**Вертикаль сервера CONNECT-IP (packet-only):** `protocol/masque/endpoint_server.go` — HTTP/3 CONNECT-IP после **`Proxy`**: **`AssignAddresses`** → **`AdvertiseRoute`** (контекст с коротким таймаутом в коде), затем **`connectIPNetPacketConn`** → **`routePacketConnectionExBypassTunnelWrapper`** (**`Router.RoutePacketConnectionEx`**; без TCP-bridge для CONNECT-IP на сервере). На sink из packet plane — **UDP payload**, отдельно от stream **`handleTCPConnectRequest`**. **`connectIPNetPacketConn.ReadPacket`** (`*buf.Buffer`): после **`parseIPDestinationAndPayload`** срез полезной нагрузки — через **`Advance`/`Truncate`** (без **`copy`/memmove** всего UDP payload на каждый пакет). Сервер должен эмитить CONNECT-IP observability (**`CONNECT_IP_OBS`** / `MaybeEmitConnectIPActiveSnapshot` из packet-path), иначе при **peer-split** **`delta_server`** может быть «пустым». В **`CONNECT_IP_OBS`** поле **`connect_ip_server_parse_drop_total`** отражает молчащие отбросы при **`parseIPDestinationAndPayload`** (см. **`connectIPServerParseDropTotal`**, init-регистрация в **`protocol/masque/connect_ip_obs_register.go`**; тело **`RegisterConnectIPServerParseDropSupplier`** и вставка ключа в **`ConnectIPObservabilitySnapshot`** — **`transport/masque/transport.go`**). Без линковки **`protocol/masque`** поле в клиентском процессе может отсутствовать — нормально для peer-split-триажа.

**Фабрики transport:** только **`CoreClientFactory`** / **`DirectClientFactory`** (`transport/masque/transport.go`) — без legacy alias.

**Граница `common/masque`:** не тянет **router** sing-box — только runtime/factory abstraction; см. **`IDEAL-MASQUE-ARCHITECTURE.md`**, глава 3 (снижает циклы зависимостей).

**Runtime API (диагностика):** помимо **`IsReady()`** — **`LifecycleState()`** / **`LastError()`**; при **`Connecting`/`Degraded`/`Reconnecting`** ошибки старта не «глотаются»: dial/listen отдают контекст через **`errors.Join`** — для triage «runtime is not ready» сначала **`LastError()`** (канон — **IDEAL**, глава 3).

**Runtime bootstrap (`common/masque/runtime.go`):** до **3** попыток **`NewSession`** с коротким backoff; при старте новой сессии согласованно сбрасываются старая сессия и **`ipPlane`** (**`transport_mode=connect_ip`**). Граф состояний: **`Init`→`Connecting`→`Ready`/`Degraded`/`Reconnecting`→`Closed`**.

**Владение CONNECT-IP / reuse:** закрытие обёртки **`connectIPPacketSession`** не разрывает общий **`connectip.Conn`** — только teardown **`coreSession`**; повторные **`OpenIPSession`** переиспользуют один **`Conn`** (**`openIPSessionLocked`**). Иначе гонки с netstack-пакетной плоскостью (см. **IDEAL**, глава 3).

**Лаб-env потолка датаграммы:** **`HIDDIFY_MASQUE_DATAGRAM_CEILING_MAX`** (**1280..65535**) — поднять верхний кламп эффективного **`datagramCeiling`** в **`CoreClientFactory.NewSession`** выше прод-дефолта (**1500** path); без переменной поведение прода не меняется (**IDEAL**, глава 1).

**MTU / путь QUIC (triage bulk, три уровня):** **`tun_mtu`** — локальный MTU интерфейса ОС (**IDEAL**, глава 1); **`masque_datagram_ceiling`** / **`datagramCeiling`** — потолок полной IP-датаграммы в CONNECT-IP; **`quic_initial_packet_size`** и Path MTU в **`quic-go`** (`newMasqueQUICConfig`) — отдельный контур (**IDEAL**, главы 1 и 5). Смешение уровней при отладке даёт ложные loss / PTB / hash drift.

**CONNECT-IP на wire (HTTP Datagram):** **Context ID = 0** — полная IP-датаграмма; капсулы (`ADDRESS_ASSIGN`, `ROUTE_ADVERTISEMENT`, …) и проверки назначенных префиксов/маршрутов — в **`third_party/connect-ip-go`** и ветках **`connectIP*`** transport/protocol. Рассинхрон капсул с фактическими префиксами на приёме даёт отброс или fail-fast по политике, а не «тихую» потерю bulk (**IDEAL**, глава 4).

**UDP-мост vs сырой IP (CONNECT-IP):** два пути на одной **`IPPacketSession`** — **`connectIPUDPPacketConn`** (IPv4 UDP-оболочка, фрагментация по PMTU) и сырой IP через **`netstack_adapter`**; в продукте UDP-мост **IPv4-only** (IPv6 bridge не в PASS-контуре CI, IDEAL §5). На серверном sink после **`connectIPNetPacketConn`** в маршрут уходит **UDP payload**, не сырой IP — иначе дрейф bulk/hash.

**TTL/Hop Limit (CONNECT-IP, connect-ip-go):** один декремент в **`composeDatagram`** на одну попытку записи; повторная отправка **того же** буфера снова уменьшит TTL/Hop Limit — типичный паттерн выше **`Conn`** (например **`netstack_adapter.writePacketWithRetry`**) — **копия буфера на каждую попытку**.

**ICMP / PTB на сервере:** ретрансляция ICMP feedback в **`connectIPNetPacketConn.writeOutgoingWithICMPRelay`** (`endpoint_server.go`) ограничена **`connectIPMaxICMPRelay`** (**8**) — при подозрении на PTB-loop сверять этот контур вместе с **`connect_ip_ptb_*`**.

**Семантика `connect_ip_engine_effective_udp_payload` (клиент, снимок OBS):** это **максимальный размер UDP payload** внутри **IPv4+UDP датаграммы**, которую собирает **`connectIPUDPPacketConn`** (`transport/masque/transport.go`, **`newConnectIPUDPPacketConn`**), верхняя граница **`datagramCeiling − 28`** (IPv4 20 + UDP 8); при минимальном **`datagramCeiling` 1280** получается **1252** — не путать с чистым размером кадра HTTP/3 DATAGRAM на wire (там добавляются оболочки CONNECT-IP/QUIC).

**UDP-мост `WriteTo` (bridge egress):** периодически вызывать только **`yieldFn`** (по умолчанию **`runtime.Gosched`**), без **wall-clock `Sleep`**, завязанного на нулевой **`engine_ingress`/`bridge_read_exit`**. Односторонний **`tcp_ip` bulk** легально не даёт ingress на клиентском bridge — искусственный backoff по этим счётчикам не коррелирует с перегрузкой QUIC и на грубом таймере ОС режет goodput. **OBS `maybeEmitConnectIPActiveSnapshot`** на мостовом egress — только из **`connectIPPacketSession.WritePacket`**; второй вызов из **`connectIPUDPPacketConn.WriteTo`** давал лишний **`time.Now`/CAS на каждую датаграмму.

**Локальный источник UDP-мост / netstack:** синтетический IPv4/IPv6 до прихода **`ADDRESS_ASSIGN`** должен совпадать с тем, что сервер реально назначает peer (в **`protocol/masque/endpoint_server.go`** по умолчанию **198.18.0.1/32**, **fd00::1/128**). Иначе входящие IP-датаграммы отбрасываются в **`third_party/connect-ip-go`** (`handleIncomingProxiedPacket`, dst ∉ assigned). Ожидание **`LocalPrefixes`** при создании моста/netstack не должно цепляться за уже отменённый **`ctx`** вызывающего **`ListenPacket`** — иначе префикс не подтягивается и остаётся fallback.

**HTTP/3 DATAGRAM → stream:** в **`replace/quic-go-patched/http3/conn.go`** `receiveDatagrams` кладёт payload в очередь только если **`streams[streamID]`** найден; иначе счётчик **`stream_not_found`** (`http3_datagram_dispatch_path_total`) и **тихий drop** — типичный сигнал рассинхрона quarter-stream-id и зарегистрированного **`TrackStream`** CONNECT-IP потока.

**HTTP/3 per-stream DATAGRAM backlog:** очередь в **`replace/quic-go-patched/http3/state_tracking_stream.go`** (`streamDatagramQueueLen`). При заполнении `enqueueDatagram` **молча дропает** → **`http3_stream_datagram_queue_drop_total`**; QUIC приём буферизует до **`maxDatagramRcvQueueLen`** (**`replace/quic-go-patched/datagram_queue.go`**). **Триаж:** при **`drop_total`** > **0** и bulk-loss — главный подозреваемый; при **`drop_total`** = **0** не списывать stall на этот слой (**см.`delta_client`/`merged`**).

**Исходящий HTTP/3 DATAGRAM (патч QUIC):** капсула собирается в буфер из **`AcquireHTTP3DatagramBuffer`**, в очередь ставится **`(*Conn).EnqueuePooledHTTPDatagram`** — без второго `make+copy` внутри старого **`SendDatagram`**. У **`wire.DatagramFrame`** поле **`OutgoingPayloadRelease`**: пул освобождается после сериализации в packer, при discard «не влезло в пакет» и при drain send-queue в **`CloseWithError`**. Внешний **`SendDatagram([]byte)`** по-прежнему копирует данные в пул и шарит тот же контур.

**OBS peer-split (`delta_client` / `delta_server`):** merged/`observability_delta` частично суммирует процессы; **`connect_ip_receive_datagram_*`**, **`http3_datagram_dispatch_path_total`** и QUIC ingress на **клиенте** vs **сервере** различать явно (**`observability_peer_split`** в JSON раннера). Для uni-directional bulk на sink не путать серверный **`enqueue_ok`** с клиентским приёмом.

**Резерв OBS `connect_ip_bypass_listenpacket_total`:** поле в снимке CONNECT-IP для bypass/`ListenPacket`; инкремента может не быть, пока bypass-путь не включён — нулевое значение **не** доказывает «нет обхода» без сверки с кодом (**IDEAL**, глава 7).

**Счётчики UDP-моста (не смешивать смысл):** в **`transport/masque/transport.go`**, тип **`connectIPUDPPacketConn`**: **`connect_ip_engine_classified_total`** инкрементируется после успешного **`WritePacket`** в **`WriteTo`**, параллельно с **`connect_ip_bridge_write_ok_*`** — это не входящий **`ReadFrom`/ingress**. Рост **`connect_ip_engine_ingress_total`** только после успешного разбора пакета в **`ReadFrom`** (после **`session.ReadPacket`**). Если **`connect_ip_bridge_readpacket_enter_total` ≥ 1**, а **`connect_ip_bridge_readpacket_return_total`** = 0 до снимка — вызов залип внутри `session.ReadPacket`. Для triage write-path дополнительно держать в snapshot `connect_ip_bridge_udp_tx_attempt_total` / `connect_ip_bridge_build_total` / `connect_ip_bridge_write_enter_total` / `connect_ip_bridge_write_chunk_total` / `connect_ip_bridge_write_err_reason_total`.

**Cadence эмита CONNECT-IP OBS:** `maybeEmitConnectIPActiveSnapshot` троттлит по `lastActiveEmitUnixMilli` (окно ~1s). Для коротких bulk-window (`send_elapsed` < 1s) write-path может дать только один delta-снимок, даже при полном объёме доставки; это диагностический артефакт эмита, а не автоматическое доказательство single-packet dataplane.

**`bridge_boundary_stall` (раннер):** **`masque_stand_runner.py`**, классификация при источнике **`runtime_snapshot_log_marker`**, неполном **`bytes_received`**: попытки UDP-моста, **`connect_ip_packet_rx_total`** = 0 и **`connect_ip_engine_ingress_total`** = 0 при подтверждённом tx; узкий случай **`post_send_frame_visibility_absent`** отделяется по нулевому **`contains_datagram_frame`**/`pre_ingress` на merged delta при ненулевом **`sendmsg_ok`**.

**Стенд `MASQUE_UDP_RATE_BPS` / `--udp-send-bps`:** в `_TCP_IP_SEND_UDP_PACED` пауза считается как **`sent / RATE_BPS`** — фактически **байт/с** (наследованное имя). Сценарий **`tcp_ip` `bulk_single_flow`** пробрасывает **`--udp-send-bps`** в этот env; **`timeout`** вокруг отправителя расширяется до **`ceil(byte_count/rate)+slack`**, иначе пейсинг бессмысленен при малом **`strict_budget`**. Без пейса на Windows Desktop клиент успевает сгенерировать **~250–850** QUIC DATAGRAM, тогда как с пейсом — тысячи; при этом **`quic_datagram_ingress` `enqueue_ok`** на merged delta может оставаться **~1** — смотреть **`delta_server`** и доставку на sink.

**Quic stack (наблюдаемость):** при расследовании залипания отправки смотреть **`quic_datagram_send_write_path_total`**: расхождение **`write_attempt` vs `write_ok`** вместе с **`send_loop_enter`** подсказывает, застряла ли упаковка/отправка относительно **`sendmsg_ok`**.

**Классы ошибок / observability:** **policy \| capability \| transport \| dial \| lifecycle**; ключи счётчиков CONNECT-IP и perf JSON — **`IDEAL-MASQUE-ARCHITECTURE.md`**, глава 7, и **`hiddify-core/docs/masque-perf-gates.md`**.

**Стенд / артефакты:** Compose `experiments/router/stand/l3router/docker-compose.masque-e2e.yml`; раннер **`masque_stand_runner.py`** из каталога **`experiments/router/stand/l3router`** (от корня `hiddify-app`); **исходники JSON, монтируемые в контейнеры:** **`experiments/router/stand/l3router/configs/`** (`masque-server.json`, `masque-client-connect-ip.json` и варианты — согласовать с **`validateMasqueOptions`**, это не произвольный «внешний» профиль). **Канон для аудита MASQUE в этом каталоге:** только файлы вида **`masque-*.json`**; прочие (`hiddify-minexp/`, `client.reality-*.json`, …) — смежные l3router-профили, не смешивать с проверкой полей эндпоинта и compose-матрицей без отдельной задачи. После прогона — **`docker compose -f docker-compose.masque-e2e.yml down -v`** как в CI; образ **`sing-box-masque-e2e:local`** подхватывает **`artifacts/sing-box-linux-amd64`**. Если слой Dockerfile с `COPY` закэширован, а бинарь менялся — пересборка с **`SINGBOX_ARTIFACT_STAMP`** (compose `build.args`) или **`--no-cache`**. Gates: `masque_runtime_ci_gate_asserts.py`, итог `masque_runtime_contract_validator.py`. Сборка бинаря как в CI: cwd **`hiddify-core/hiddify-sing-box`**, `GOOS=linux GOARCH=amd64`, `CGO_ENABLED=0`, **`-tags with_masque`**, вывод **`../../experiments/router/stand/l3router/artifacts/sing-box-linux-amd64`** (**`../../`** из sing-box-поддиректории попадает в корень монорепо **`hiddify-app`**). Точка входа: `./cmd/sing-box`. **Правило скоростной матрицы:** следующий прогон строить как **`max_pass` + `next_boundary`** (первый уровень выше текущего `max_pass`, где ранее был `fail`) и фиксировать оба значения в runtime-артефакте.

**Стенд, Python без Compose:** после правок **`masque_stand_runner.py`** / **`masque_runtime_contract_validator.py`** / **`masque_runtime_ci_gate_asserts.py`** — cwd **`experiments/router/stand/l3router`**, быстрый контрактный прогон: **`python -m unittest test_masque_runtime_contract_validator test_masque_runtime_ci_gate_asserts test_masque_stand_runner_smoke_contract`** (соседние модули на **`sys.path`**; не замена e2e).

**Пути и `ci.yml`:** в **`hiddify-core/.github/workflows/ci.yml`** job **`masque-gates`** задаёт **`cd hiddify-sing-box`** и **`../../experiments/...`** от корня **одного** репозитория `hiddify-core`; в монорепо **`hiddify-app`** те же команды выполняются с cwd **`hiddify-core/hiddify-sing-box`** и **`experiments/router/stand/l3router`** (эквивалентная геометрия путей).

**Топология compose e2e (якорь):** **`masque-server-core`** — listener **`:8443`**, **10.200.0.3** на сети **`masque-backend`** (плюс **`masque-public`**); **`masque-client-core`** — **`masque-public`**; **`iperf-server`** (sink/load) — **10.200.0.2** на **`masque-backend`**. Дефолтные конфиги в volume: **`configs/masque-server.json`**, **`configs/masque-client-connect-ip.json`**.

**Имена job в CI (не путать):** блокирующий PR/push пайплайн **`masque-gates`** (`hiddify-core/.github/workflows/ci.yml`); загрузка артефактов ран-тайма с него исторически называется **`masque-gates-runtime`** (ключ `upload-artifact`). Ночной perf-матрикс **`--stress`/`iperf`/`--assert-nightly-perf-thresholds`** живёт в отдельном job **`masque-nightly-perf`** (ветка **`schedule`**), не в **`masque-gates`**.

**Локальный `masque_runtime_contract_validator` (`--assert-schema`, `--assert-*`):** строки **`anti_bypass`/parity с summary** зависят от артефактов после шага **`masque_runtime_ci_gate_asserts.py --run-anti-bypass-negative-control ...`** (порядок как в **`masque-gates`**). После только **`masque_stand_runner.py --scenario all`** без negatives — ожидаемый FAILED parity до прогона anti-bypass.

**In-process HTTP/3 CONNECT proxy в тестах** (`transport/masque/transport_test.go`, **`startInProcessTCPConnectProxy`**): один **`t.Cleanup`** — **`http3.Server.Close()`** → **`WaitGroup.Wait()`** на выходе **`Serve(PacketConn)`** → **`UDPConn.Close()`**. Два независимых cleanup (закрытие UDP до завершения `Serve`) давали гонку с **`-race`** на Windows (**`0xc0000005`**, не data race); паритет с Linux CI не отменяет корректный порядок shutdown в harness.

**Порядок `masque-gates`:** только по **`ci.yml`**; **точная** последовательность шагов — по полю **`name:`** у **`steps`** в job **`masque-gates`** (в логах GitHub Actions заголовки шагов совпадают с этими именами). Кратко для навигации: unit/race → fast regression → fast integration → non-auth → lifecycle → build linux artifact → smoke → strict `tcp_ip` (объёмы) → `tcp_ip_scoped` → anti-bypass negatives → contract validator + typed asserts. Локальный быстрый срез (не замена CI):  
`go test -count=1 -short -tags with_masque ./protocol/masque/... ./transport/masque/...`  
parity с unit gate CI: добавить `./common/masque/... ./include/...`; race/integration — см. workflow.

**Локальный полный replay `masque-gates` (вне GitHub Actions):** повторять шаги job в порядке **`name:`** из **`hiddify-core/.github/workflows/ci.yml`**; шаг **CONNECT-STREAM non-auth matrix** запускается из **`hiddify-sing-box`** с путём **`../../experiments/router/stand/l3router/masque_runtime_ci_gate_asserts.py`**. Шаг **fast integration** включает **`go test -C third_party/connect-ip-go ...`** — у **`go test`** флаг **`-C`** должен быть **первым** среди аргументов **`go test`** (иначе ошибка «`-C flag must be first`»). **`go test -race`** для паритета с Ubuntu — по возможности на Linux/WSL; на Windows см. осторожности в §6.

**Triage-сводка (без расшифровки каждого счётчика):** merged **`observability_delta`** может быть **max** по процессам; для forward DATAGRAM **client→server** смотреть **`delta_server`** (`post_decrypt`, `ingress`), для исходящего клиента — **`delta_client`** (`sendmsg_ok`, очередь send). Разрыв **`sendmsg_ok` ≫ server `short_unpack_ok`** при нулевых `quic_packet_receive_drop_*`: сверять **`transport_read_packet_total`** на сервере vs send; затем wire/pcap до `:8443` vs хост Docker. Классы стоп-причин раннера (`bridge_boundary_stall`, `post_send_frame_visibility_absent`, …) — в Python-раннере/validator; детальные сигнатуры starvation, http3/quic queues, frame-mix, **co-pack DATAGRAM с ACK-only** (`packet_packer`) — **`AGENT-LAYER-SOURCE-OF-TRUTH.md`** и связанные runbooks.

**Локально Windows:** `go test -race` — **`CGO_ENABLED=1`**; после cross-build Linux сбросить **`GOOS`/`GOARCH`**. PowerShell: цепочки через **`;`**, не `&&`. WSL bash: использовать **`docker`**, не `docker.exe`. Для **`go test -C <dir>`** (как в **`masque-gates`** для **`third_party/connect-ip-go`**): следующий после **`go test`** аргумент — **`-C`**, затем путь директории, затем остальное (**`-count`**, **`-run`**, пакеты); вызовы вида **`go test -count=1 -C …`** в одной строке **невалидны** («`-C flag must be first`» — флаг не считается первым). Пример: **`go test -C third_party/connect-ip-go -count=1 -run '…' .`**. Harness in-process HTTP/3 proxy в **`transport_test`** после фикса сериализации shutdown проходит **`-race`** на Windows; иной **access violation** — смотреть двойное закрытие **`PacketConn`** / фоновые горутины quic. Комбинация **`-race -short`** с подмешиванием TLS/QUIC на этом хосте иногда даёт **`0xc0000005`** в **`crypto/tls`** — для паритета с **job `masque-gates`** использовать **`-race`** без **`-short`** на затронутых пакетах.

**Python-раннер, host `go test`:** сценарий **`tcp_ip_scoped`** (и смежные harness) вызывает **`go test`** с **`cwd=hiddify-core/hiddify-sing-box`**; родительские **`GOOS`/`GOARCH`** (типично после **`GOOS=linux go build`** на Windows/Mac) давали ELF `*.test` и ошибку **`%1 is not a valid Win32 application`**. Обход: **`_env_for_host_go_test()`** в **`masque_stand_runner.py`** перед harness сбрасывает **`GOOS`/`GOARCH`**; оператор может вручную **`Remove-Item Env:GOOS, Env:GOARCH`** перед раннером.

**CONNECT-IP / netstack MTU:** MTU виртуальной NIC netstack (**`connectIPTCPNetstackFactory`**) выравнивается с **`datagramCeiling`** — это отдельный контур от **`tun_mtu`** и от QUIC Path MTU; см. **IDEAL** главу 5.

**Lifecycle CONNECT-IP (staged closure):** в стабильных успешных прогонах типичный **`ErrTCPStackInit`** не является нормой; чеклист и текст — **`hiddify-core/docs/masque-connect-ip-staged-closure.md`** + **IDEAL** глава 8 (**канон против устаревшего «`tcp_transport=connect_ip` enabled»** там же).

**Валидация конфигурации (якоря, не дубль IDEAL):** для TCP в проде **`tcp_transport=auto` запрещён** (нужен явный режим, обычно **`connect_stream`**); **`quic_experimental`** допускается только при **`MASQUE_EXPERIMENTAL_QUIC=1`**. Цепочка: **`hop_policy=chain`** требует **`hops[]`** с уникальными **`tag`** и согласованный граф через **`CM.BuildChain`** (`common/masque`) — полное правило в **`IDEAL-MASQUE-ARCHITECTURE.md`** §1–3 и **`protocol/masque/endpoint.go`**. Скрытые миграции с legacy **`warp`** или неформализованными старыми путями конфига запрещены (**IDEAL**, глава 1).

**Конфиг / отказ записи CONNECT-IP:** поля **`udp_timeout`**, **`workers`** не поддерживаются — ошибка валидации в **`endpoint.go`** (**IDEAL**, глава 1). Запись IP-пакета с **`len > datagramCeiling`** режется до QUIC как **`ceiling_reject`** / событие **`packet_write_fail_ceiling`** (**`connectIPPacketSession.WritePacket`**); не смешивать с дропами HTTP/3 backlog и циклом PTB (**IDEAL**, главы 1 и 4).

**Third-party RFC-слои (где искать wire):** **`third_party/masque-go`** — bootstrap CONNECT/stream/UDP (MASQUE/H3); **`third_party/connect-ip-go`** — IP-датаграммы и TTL/PTB на **`Conn`**. Из **`hiddify-sing-box`** для tuple/IPv6: **`go test -C third_party/connect-ip-go -run TestPacketTupleRejectsAmbiguousIPv6ExtensionChain .`** (как в **`masque-gates`**).

**Hot path packet-builder (CONNECT-IP UDP bridge):** в `transport/masque/transport.go` для IPv4 fast-path избегать `netip.Addr.AsSlice()` в per-packet цикле (`WriteTo`/builder) — предпочитать `As4()` + inplace-буфер, иначе лишние аллокации/копии ухудшают стабильность на верхней границе `tcp_ip_threshold`.

## 7) Current autonomous cycle (overwrite each iteration)

- **Матрица скорости (обязательно обновлять в каждой итерации):**
  1. **Отправная точка (current baseline):** текущий диапазон/точка, от которой стартует новая матрица.
  2. **Предыдущая PASS-точка:** последний подтверждённый устойчивый уровень (например, `80-100 Mbps` или конкретный `max_pass`).
  3. **Последний прогон матрицы:** фактические результаты последнего запуска (`max_pass`, `next_boundary`, `first_fail`/`no_fail`, ключевые `loss_pct` и `throughput_target_met`).
  Формат записи должен быть числовым и воспроизводимым по runtime-артефактам (`experiments/router/stand/l3router/runtime/*.json`), без общих формулировок.

- **Дата:** 2026-05-07  
- **Старт итерации / текущий фокус:** CONNECT-IP **gVisor netstack** — **`addStackAddress`** / **`convertToFullAddr`** переведены на **`tcpip.AddrFrom4` / `tcpip.AddrFrom16`** вместо **`AddrFromSlice(addr.AsSlice())`**. **UDP-мост CONNECT-IP:** один раз выделяется **`localBind`** (`*net.UDPAddr`) для **`LocalAddr()`**, без повторных **`net.IPv4`** на запрос. **Compose:** `python masque_stand_runner.py --scenario all` на Docker Desktop — **PASS** (~48 s), затем `docker compose … down -v`. Матрица **`--udp-send-bps`** (130M/140M) в этой итерации **не снималась**.
- **`hiddify-core` `HEAD`:** **`75e525a682e334e9927f8983d9e8bc8451321640`**.
- **Отправная точка (current baseline):** `20 MiB`, `tcp_ip`, `bulk_single_flow`, `--udp-send-bps` (байт/с).
- **Строгие тайминги (без `MASQUE_STAND_SLOW_DOCKER`):** последний зафиксированный артефакт — `110000000` на Docker Desktop (Windows): узкий промах по дедлайну (`near_full_loss_under_cadence`); пересмотр после прогона на Linux/WSL.
- **Лестница с `MASQUE_STAND_SLOW_DOCKER=1`:** опорная лестница прежней сессии: **`max_pass=130000000`**, **`next_boundary=140000000`** (`experiments/router/stand/l3router/runtime/masque_python_runner_summary.json`); новый sweep не делался.
- **Предыдущая PASS-точка:** `130000000` (slow-docker профиль).
- **Контрольные прогоны после правок:** `go test -count=1 -short -tags with_masque ./transport/masque/... ./protocol/masque/... ./common/masque/...` — PASS; `python -m unittest test_masque_runtime_ci_gate_asserts test_masque_stand_runner_smoke_contract test_masque_runtime_contract_validator` — PASS; **`masque_stand_runner.py --scenario all`** — PASS.
- **Источник истины по шагам CI:** **`hiddify-core/.github/workflows/ci.yml`**, job **`masque-gates`**.

## 8) Next iteration tasks (single-thread, code-first)

1. **Подтвердить границу `130000000` / `140000000` на Linux CI или WSL** без `MASQUE_STAND_SLOW_DOCKER`; узкий sweep при необходимости; зафиксировать `max_pass` / `runtime/*.json`.
2. **`connect_udp` на потолке:** `udp`, `20 MiB`, высокие `--udp-send-bps`; сравнить с CONNECT-IP — общий ли предел QUIC DATAGRAM.
3. **Следующий слой копирования на wire:** `wire.DatagramFrame.Append` (`append(b, f.Data...)`) в plaintext до Seal; трогать только при сигнале со стенда/профиля.
4. **`hiddify-app`:** обновить указатель submodule **`hiddify-core`** на **`75e525a682e334e9927f8983d9e8bc8451321640`** (или новее после push) и закоммитить монорепо.
