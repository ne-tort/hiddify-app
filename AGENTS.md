# AGENTS — MASQUE / WARP_MASQUE

## 1) Mission

Цель: довести `masque` endpoint в `hiddify-core/hiddify-sing-box` до production-качества и RFC-сходимости (`connect_stream`, `connect_udp`, `connect_ip`) по измеримым артефактам CI/стенда (`masque-gates`, `experiments/router/stand/l3router`).

**Windows:** Docker для MASQUE e2e доступен через **Docker Desktop** (в т.ч. WSL2 backend); стенд тот же, что в CI. Нет прогона — не «запрет инфраструктуры», а незапущенный демон или пропуск шага.

## 2) Non-negotiables

- Цикл: сигнал → код → тест/стенд → фиксация артефакта → следующий шаг.
- Fail (`loss`, `timeout`, `budget_exceeded`, hash drift, `throughput_target_unmet`) — разбирать по boundary слоя, без fake-green.
- Не ослаблять пороги/таймауты/валидации ради PASS.
- Источник истины при расхождении: **код → `hiddify-core/.github/workflows/ci.yml` (job `masque-gates`) → `IDEAL-MASQUE-ARCHITECTURE.md` → `docs/masque/*` → этот файл** (`AGENTS.md` — дорожная карта и операторские якоря, не дубликат RFC).
- `masque_or_direct` только с `fallback_policy=direct_explicit`.
- Рефакторинг допустим агрессивно при сложной диагностике, если не ломает контракты/RFC и подтверждается тестами/стендом.
- Submodule: правки ядра — коммит в `hiddify-core`; в родителе `hiddify-app` — обновление **SHA** submodule.

## 3) Read First (mandatory)

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
5. Обновить разделы **7** (текущий ход) и **8** (следующие шаги) этого файла.

## 5) Definition of Done

- Нет регрессий mode/fallback/lifecycle/scoped-контрактов.
- Unit/race/integration по затронутым пакетам — PASS.
- Стенд/validator — PASS для целевой матрицы.
- Изменение поведения/RFC отражено в коде и тестах там, где нужно.

## 6) Architecture addendum (кратко)

**RFC (номера):** MASQUE **9298**, HTTP Datagrams **9297**, CONNECT-IP **9484**. Норматив реализации — **IDEAL-MASQUE-ARCHITECTURE.md** + код.

**Корень sing-box:** `hiddify-core/hiddify-sing-box/` (сборка/тесты только отсюда; `go.mod` — модуль `github.com/sagernet/sing-box`). Patched QUIC: `replace/quic-go-patched/`.

**Жизненный цикл wiring:** конфиг распаковывается в `option` (`endpoints[]` через `option/endpoint.go` + **`EndpointOptionsRegistry`** из сборки с `include` и тегом **`with_masque`**). Endpoint создаётся фабрикой из **`protocol/masque/register.go`**; общий контур sing-box вызывает **`adapter.Endpoint`** — `Network()`, `Start(adapter.StartStage)`, `Close()`. Отдельного inbound-типа `masque` нет: трафик — через **`endpoints[]` + `route` по tag** (`route.rules[].outbound`, `route.final` в том же пространстве имён tag, что и `endpoints[].tag`).

**Регистрация типов:** `include/masque.go` (+ `with_masque`) → `masque.RegisterEndpoint` / `RegisterWarpMasqueEndpoint`; без тега — `include/masque_stub.go`. Фабрики: **`NewEndpoint`** (`endpoint.go`), **`NewWarpEndpoint`** (`endpoint_warp_masque.go`). Опции: **`option/masque.go`**; generic **`masque`** vs **`warp_masque`** — см. `warp_control_adapter.go`, ADR `hiddify-core/docs/masque-warp-architecture.md`.

**`tcp_stream` / `connect_udp` vs CONNECT-IP:** отдельные ветки в **`transport/masque/transport.go`** и relay/stream в **`endpoint_server.go`** (H3 CONNECT / UDP datagram); не смешивать с **`connectIPNetPacketConn`**. Матрица носителей — **IDEAL**, глава 4.

**Вертикаль клиента (CONNECT-IP):** `protocol/masque/endpoint.go` (валидация `validateMasqueOptions`, lifecycle) → `common/masque/runtime.go` (`Runtime`/ретраи, без router) → `transport/masque/transport.go` (**`coreSession`**: QUIC/H3, `ListenPacket` / `DialContext` / `OpenIPSession`, ветки CONNECT-UDP vs `connect_ip`) → `third_party/connect-ip-go` + `third_party/masque-go` (RFC bootstrap CONNECT/stream/UDP). **TCP в TUN-only:** netstack **`transport/masque/netstack_adapter.go`** при **`transport_mode=connect_ip`**; клиент **`tcp_transport=connect_ip`** запрещён.

**Вертикаль сервера CONNECT-IP (packet-only):** `protocol/masque/endpoint_server.go` — HTTP/3, **`connectIPNetPacketConn`** → `router.RoutePacketConnectionEx` (UDP payload к маршрутизатору, не смешивать с stream TCP CONNECT). Сервер должен эмитить CONNECT-IP observability (**`CONNECT_IP_OBS`** / `MaybeEmitConnectIPActiveSnapshot` из packet-path), иначе при **peer-split** **`delta_server`** может быть «пустым». В **`CONNECT_IP_OBS`** поле **`connect_ip_server_parse_drop_total`** отражает молчащие отбросы при **`parseIPDestinationAndPayload`** (см. **`connectIPServerParseDropTotal`**, регистрация через **`RegisterConnectIPServerParseDropSupplier`** в **`protocol/masque/connect_ip_obs_register.go`**).

**Фабрики transport:** только **`CoreClientFactory`** / **`DirectClientFactory`** (`transport/masque/transport.go`) — без legacy alias.

**Граница `common/masque`:** не тянет **router** sing-box — только runtime/factory abstraction; см. **`IDEAL-MASQUE-ARCHITECTURE.md`**, глава 3 (снижает циклы зависимостей).

**CONNECT-IP на wire (HTTP Datagram):** **Context ID = 0** — полная IP-датаграмма; капсулы управления и проверки префиксов — в **`third_party/connect-ip-go`** и ветках **`connectIP*`** transport/protocol (матрица носителей — IDEAL, глава 4).

**UDP-мост vs сырой IP (CONNECT-IP):** два пути на одной **`IPPacketSession`** — **`connectIPUDPPacketConn`** (IPv4 UDP-оболочка, фрагментация по PMTU) и сырой IP через **`netstack_adapter`**; в продукте UDP-мост **IPv4-only** (IPv6 bridge не в PASS-контуре CI, IDEAL §5). На серверном sink после **`connectIPNetPacketConn`** в маршрут уходит **UDP payload**, не сырой IP — иначе дрейф bulk/hash.

**TTL/Hop Limit (CONNECT-IP, connect-ip-go):** один декремент в **`composeDatagram`** на одну попытку записи; повторная отправка **того же** буфера снова уменьшит TTL/Hop Limit — типичный паттерн выше **`Conn`** (например **`netstack_adapter.writePacketWithRetry`**) — **копия буфера на каждую попытку**.

**ICMP / PTB на сервере:** ретрансляция ICMP feedback в **`connectIPNetPacketConn.writeOutgoingWithICMPRelay`** (`endpoint_server.go`) ограничена **`connectIPMaxICMPRelay`** (**8**) — при подозрении на PTB-loop сверять этот контур вместе с **`connect_ip_ptb_*`**.

**Семантика `connect_ip_engine_effective_udp_payload` (клиент, снимок OBS):** это **максимальный размер UDP payload** внутри **IPv4+UDP датаграммы**, которую собирает **`connectIPUDPPacketConn`** (`transport/masque/transport.go`, **`newConnectIPUDPPacketConn`**), верхняя граница **`datagramCeiling − 28`** (IPv4 20 + UDP 8); при минимальном **`datagramCeiling` 1280** получается **1252** — не путать с чистым размером кадра HTTP/3 DATAGRAM на wire (там добавляются оболочки CONNECT-IP/QUIC).

**UDP-мост `WriteTo` (bridge egress):** периодически вызывать только **`yieldFn`** (по умолчанию **`runtime.Gosched`**), без **wall-clock `Sleep`**, завязанного на нулевой **`engine_ingress`/`bridge_read_exit`**. Односторонний **`tcp_ip` bulk** легально не даёт ingress на клиентском bridge — искусственный backoff по этим счётчикам не коррелирует с перегрузкой QUIC и на грубом таймере ОС режет goodput.

**Локальный источник UDP-мост / netstack:** синтетический IPv4/IPv6 до прихода **`ADDRESS_ASSIGN`** должен совпадать с тем, что сервер реально назначает peer (в **`protocol/masque/endpoint_server.go`** по умолчанию **198.18.0.1/32**, **fd00::1/128**). Иначе входящие IP-датаграммы отбрасываются в **`third_party/connect-ip-go`** (`handleIncomingProxiedPacket`, dst ∉ assigned). Ожидание **`LocalPrefixes`** при создании моста/netstack не должно цепляться за уже отменённый **`ctx`** вызывающего **`ListenPacket`** — иначе префикс не подтягивается и остаётся fallback.

**HTTP/3 DATAGRAM → stream:** в **`replace/quic-go-patched/http3/conn.go`** `receiveDatagrams` кладёт payload в очередь только если **`streams[streamID]`** найден; иначе счётчик **`stream_not_found`** (`http3_datagram_dispatch_path_total`) и **тихий drop** — типичный сигнал рассинхрона quarter-stream-id и зарегистрированного **`TrackStream`** CONNECT-IP потока.

**HTTP/3 per-stream DATAGRAM backlog:** очередь в **`replace/quic-go-patched/http3/state_tracking_stream.go`** (`streamDatagramQueueLen`). При заполнении `enqueueDatagram` **молча дропает** → **`http3_stream_datagram_queue_drop_total`**; QUIC приём буферизует до **`maxDatagramRcvQueueLen`** (**`replace/quic-go-patched/datagram_queue.go`**). **Триаж:** при **`drop_total`** > **0** и bulk-loss — главный подозреваемый; при **`drop_total`** = **0** не списывать stall на этот слой (**см.`delta_client`/`merged`**).

**OBS peer-split (`delta_client` / `delta_server`):** merged/`observability_delta` частично суммирует процессы; **`connect_ip_receive_datagram_*`**, **`http3_datagram_dispatch_path_total`** и QUIC ingress на **клиенте** vs **сервере** различать явно (**`observability_peer_split`** в JSON раннера). Для uni-directional bulk на sink не путать серверный **`enqueue_ok`** с клиентским приёмом.

**Счётчики UDP-моста (не смешивать смысл):** в **`transport/masque/transport.go`**, тип **`connectIPUDPPacketConn`**: **`connect_ip_engine_classified_total`** инкрементируется после успешного **`WritePacket`** в **`WriteTo`**, параллельно с **`connect_ip_bridge_write_ok_*`** — это не входящий **`ReadFrom`/ingress**. Рост **`connect_ip_engine_ingress_total`** только после успешного разбора пакета в **`ReadFrom`** (после **`session.ReadPacket`**). Если **`connect_ip_bridge_readpacket_enter_total` ≥ 1**, а **`connect_ip_bridge_readpacket_return_total`** = 0 до снимка — вызов залип внутри `session.ReadPacket`.

**`bridge_boundary_stall` (раннер):** **`masque_stand_runner.py`**, классификация при источнике **`runtime_snapshot_log_marker`**, неполном **`bytes_received`**: попытки UDP-моста, **`connect_ip_packet_rx_total`** = 0 и **`connect_ip_engine_ingress_total`** = 0 при подтверждённом tx; узкий случай **`post_send_frame_visibility_absent`** отделяется по нулевому **`contains_datagram_frame`**/`pre_ingress` на merged delta при ненулевом **`sendmsg_ok`**.

**Стенд `MASQUE_UDP_RATE_BPS` / `--udp-send-bps`:** в `_TCP_IP_SEND_UDP_PACED` пауза считается как **`sent / RATE_BPS`** — фактически **байт/с** (наследованное имя). Сценарий **`tcp_ip` `bulk_single_flow`** пробрасывает **`--udp-send-bps`** в этот env; **`timeout`** вокруг отправителя расширяется до **`ceil(byte_count/rate)+slack`**, иначе пейсинг бессмысленен при малом **`strict_budget`**. Без пейса на Windows Desktop клиент успевает сгенерировать **~250–850** QUIC DATAGRAM, тогда как с пейсом — тысячи; при этом **`quic_datagram_ingress` `enqueue_ok`** на merged delta может оставаться **~1** — смотреть **`delta_server`** и доставку на sink.

**Quic stack (наблюдаемость):** при расследовании залипания отправки смотреть **`quic_datagram_send_write_path_total`**: расхождение **`write_attempt` vs `write_ok`** вместе с **`send_loop_enter`** подсказывает, застряла ли упаковка/отправка относительно **`sendmsg_ok`**.

**Классы ошибок / observability:** **policy \| capability \| transport \| dial \| lifecycle**; ключи счётчиков CONNECT-IP и perf JSON — **`IDEAL-MASQUE-ARCHITECTURE.md`**, глава 7, и **`hiddify-core/docs/masque-perf-gates.md`**.

**Стенд / артефакты:** Compose `experiments/router/stand/l3router/docker-compose.masque-e2e.yml`; раннер `masque_stand_runner.py`; gates `masque_runtime_ci_gate_asserts.py`, итог `masque_runtime_contract_validator.py`. Сборка бинаря как в CI: из **`hiddify-sing-box`**, `GOOS=linux GOARCH=amd64`, `CGO_ENABLED=0`, **`-tags with_masque`**, вывод **`../../experiments/router/stand/l3router/artifacts/sing-box-linux-amd64`** (два уровня вверх от `hiddify-sing-box` до корня монорепо `hiddify-app`). Точка входа процесса: `./cmd/sing-box`.

**Локальный `masque_runtime_contract_validator` (`--assert-schema`, `--assert-*`):** строки **`anti_bypass`/parity с summary** зависят от артефактов после шага **`masque_runtime_ci_gate_asserts.py --run-anti-bypass-negative-control ...`** (порядок как в **`masque-gates`**). После только **`masque_stand_runner.py --scenario all`** без negatives — ожидаемый FAILED parity до прогона anti-bypass.

**In-process HTTP/3 CONNECT proxy в тестах** (`transport/masque/transport_test.go`, **`startInProcessTCPConnectProxy`**): один **`t.Cleanup`** — **`http3.Server.Close()`** → **`WaitGroup.Wait()`** на выходе **`Serve(PacketConn)`** → **`UDPConn.Close()`**. Два независимых cleanup (закрытие UDP до завершения `Serve`) давали гонку с **`-race`** на Windows (**`0xc0000005`**, не data race); паритет с Linux CI не отменяет корректный порядок shutdown в harness.

**Порядок `masque-gates`:** только по **`ci.yml`**: unit/race → fast regression → fast integration → non-auth → lifecycle → build linux artifact → smoke → strict `tcp_ip` (объёмы) → `tcp_ip_scoped` → anti-bypass negatives → contract validator + typed asserts. Локальный быстрый срез (не замена CI):  
`go test -count=1 -short -tags with_masque ./protocol/masque/... ./transport/masque/...`  
parity с unit gate CI: добавить `./common/masque/... ./include/...`; race/integration — см. workflow.

**Triage-сводка (без расшифровки каждого счётчика):** merged **`observability_delta`** может быть **max** по процессам; для forward DATAGRAM **client→server** смотреть **`delta_server`** (`post_decrypt`, `ingress`), для исходящего клиента — **`delta_client`** (`sendmsg_ok`, очередь send). Разрыв **`sendmsg_ok` ≫ server `short_unpack_ok`** при нулевых `quic_packet_receive_drop_*`: сверять **`transport_read_packet_total`** на сервере vs send; затем wire/pcap до `:8443` vs хост Docker. Классы стоп-причин раннера (`bridge_boundary_stall`, `post_send_frame_visibility_absent`, …) — в Python-раннере/validator; детальные сигнатуры starvation, http3/quic queues, frame-mix, **co-pack DATAGRAM с ACK-only** (`packet_packer`) — **`AGENT-LAYER-SOURCE-OF-TRUTH.md`** и связанные runbooks.

**Локально Windows:** `go test -race` — **`CGO_ENABLED=1`**; после cross-build Linux сбросить **`GOOS`/`GOARCH`**. PowerShell: цепочки через **`;`**, не `&&`. WSL bash: использовать **`docker`**, не `docker.exe`. Harness in-process HTTP/3 proxy в **`transport_test`** после фикса сериализации shutdown проходит **`-race`** на Windows; иной **access violation** — смотреть двойное закрытие **`PacketConn`** / фоновые горутины quic.

## 7) Current autonomous cycle (overwrite each iteration)

- **Дата:** 2026-05-07  
- **Слой / правка:** harness тестов **`hiddify-core/hiddify-sing-box/transport/masque/transport_test.go`** — **`startInProcessTCPConnectProxy`**: единый cleanup, ожидание выхода **`Serve`** перед **`UDPConn.Close()` (устраняет **`0xc0000005`** при **`go test -race ./transport/masque`** на Windows). Коммит в **`hiddify-core`**: **`88b8332b`**.  
- **`go test -count=1 -short -tags with_masque`** `./protocol/masque/... ./transport/masque/... ./common/masque/... ./include/...`, **`CGO_ENABLED=0`** — **PASS**.  
- **`go test -count=1 -race -tags with_masque ./protocol/masque`** и **`./transport/masque`**, **`CGO_ENABLED=1`** — **PASS** (Windows).  
- **Стенд:** cross-build **`sing-box-linux-amd64`**, **`masque_stand_runner.py --scenario all`** (Docker Desktop) — **PASS**; **`masque_runtime_contract_validator` + `--assert-schema`** — **PASS** после прогона **`--run-anti-bypass-negative-control`** для **tcp_stream / udp / tcp_ip** (как в CI).  
- **Не сделано в этой сессии (CI parity):** strict **`tcp_ip`** 10/20/50 MiB, **`tcp_ip_scoped`**, полный набор typed **`--assert-*`** по контракту.

## 8) Next iteration tasks (single-thread, code-first)

1. **Полный локальный replay `masque-gates` (после docker-build):** **`tcp_ip --megabytes 10/20/50`**, **`tcp_ip_scoped`**, затем все **`masque_runtime_ci_gate_asserts.py --assert-*`** из **`ci.yml`**.  
2. Остаточный diff в **`hiddify-core`** (вне **`88b8332b`**) — довести до review/коммитов по одному слою; в **`hiddify-app`** — bump **SHA** submodule после приёмки ядра.  
3. Регресс **`connect_ip` / H3 / QUIC** на стенде: triage по **`AGENT-LAYER-SOURCE-OF-TRUTH.md`** при **`bridge_boundary_stall`**, **`post_send_frame_visibility_absent`**, **`connect_ip_server_parse_drop_total`**.  
4. Не смешивать в один коммит несвязанные правки **`transport_test.go`** с крупными WIP: перед **`git add`** сверять **`git diff --stat`**.
