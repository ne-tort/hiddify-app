# AGENTS — MASQUE / WARP_MASQUE (операционный runbook)

## 1) Scope и цель

Цель: production-ready `masque` endpoint в sing-box ядре (`connect_stream`, `connect_ip`, `connect_udp`) по RFC-контрактам и архитектуре проекта, без регрессий legacy `warp` и без деградаций dataplane.

## 2) Непереговорные инварианты

- Legacy `warp` не ломать, без скрытых миграций/алиасов.
- Никаких «green за счет подкрутки» (таймаутов, отключения проверок, ослабления fail-fast).
- Любой hang/timeout считать дефектом реализации или тестового контракта.
- Ошибки и метрики держать прозрачными по классам: `policy/capability/transport/dial/lifecycle`.
- Любые fallback пути только явные и документированные (`direct_explicit`), без неявного bypass.

## 3) Канон режимов MASQUE

- TCP relay: только `tcp_transport=connect_stream` + `template_tcp`.
- TUN packet-plane: `transport_mode=connect_ip` + netstack (`OpenIPSession`), а не `tcp_transport=connect_ip`.
- `connect_udp` и `connect_ip` не смешивать в одном неявном пути.
- Клиентский `tcp_transport=connect_ip` и `tcp_transport=auto` для production профилей — fail-fast reject.
- `connectIPUDPPacketConn` (UDP bridge поверх CONNECT-IP) в текущем production-контракте считается IPv4-only; IPv6 destination bridge должен быть либо отдельной реализацией, либо явным fail-fast.
- Generic `masque` endpoint не должен включать Cloudflare/WARP control-plane специфику; она допустима только в `warp_masque`.
- Для `warp_masque` сохраняется parity с legacy `warp`: embedded registration/bootstrap остаются в endpoint path (без выноса в неявный внешний слой).

## 4) Критичные RFC-акценты (обязательно учитывать в каждой итерации)

- RFC 9297: unknown capsule type на приемнике должен идти в silent-skip, не в закрытие потока.
- RFC 9297: observability по unknown capsule должна включать не только total, но и reason/type breakdown (для interop-диагностики).
- RFC 9297/9298: DATAGRAM отправлять только после подтвержденного `SETTINGS_H3_DATAGRAM`.
- RFC 9297/9298: malformed capsule/datagram varint/length parse ошибки должны обрабатываться fail-closed с детерминированной классификацией (`transport_init` или `capability` по boundary), без silent-ignore.
- RFC 9484: `Context ID=0` — IP payload; неизвестный context-id не ломает сессию (silent drop/ограниченная буферизация по политике).
- RFC 9484: unknown context-id handling должен иметь стабильную observability (`drop_total` + reason breakdown), чтобы interop-деградации не уходили в «тихий» loss.
- RFC 9484: policy по IPv6 должна учитывать extension chain до final upper-layer protocol.
- RFC 9484: если IPv6 extension chain неразбираем, нельзя молча деградировать к `Next Header` из fixed IPv6 header в policy-решении; нужен явный fail-closed/classified handling.
- RFC 9484: ROUTE_ADVERTISEMENT и address/route capsule semantics должны соблюдаться строго (replace semantics, strict start-order, non-overlap within family, abort on invalid advertisement).
- ROUTE_ADVERTISEMENT endpoint contract: server-side fail-fast на invalid advertisement должен логироваться с детерминированным `error_class` (`capability` для RFC-validation reject, `transport_init` для send/stream path fail) для pre-docker traceability.
- ROUTE_ADVERTISEMENT classifier robustness contract: endpoint-side классификация invalid advertisement должна опираться на typed/sentinel contract (`errors.Is(..., connectip.ErrInvalidRouteAdvertisement)`), без строковых match по тексту ошибки.
- ROUTE_ADVERTISEMENT peer contract: при peer-side invalid advertisement на receive path сессия должна abort/fail-closed с детерминированным lifecycle сигналом (`net.ErrClosed`, `CloseError.Remote=true`) и трассируемой классификацией на boundary runtime/endpoint.
- ROUTE_ADVERTISEMENT boundary parity contract: invalid advertisement должен давать deterministic dual-signal на boundary — RFC-validation reject классифицируется как `capability`, а последующий peer-close signal классифицируется как `lifecycle` (без деградации в `unknown` и без string-match).
- ROUTE_ADVERTISEMENT dual-signal artifact contract: для `negative_peer_invalid_route_advertisement` поле `error_class_consistent=true` означает соответствие ожидаемой паре (`actual_error_class=capability`, `result_error_class=lifecycle`), а не равенство классов между собой.
- ROUTE_ADVERTISEMENT dual-signal harness contract: `negative_peer_invalid_route_advertisement` в runner должен строиться из fast go harness artifact (`MASQUE_ROUTE_ADVERTISE_ARTIFACT_PATH`) с fail-fast на missing/invalid artifact (без synthetic probe fallback).
- ROUTE_ADVERTISEMENT dual-signal CI gate contract: workflow-level blocking assert для `runtime/route_advertise_dual_signal_runtime.json` обязателен отдельно от агрегированного validator (`ok=true`, `actual_error_class=capability`, `result_error_class=lifecycle`, `error_class_consistent=true`, `error_source=runtime`).
- Runtime harness error-source contract: `runtime/masque_runtime_contract_latest.json` обязан экспортировать `checks.runtime_artifacts_error_source` с per-row `peer_abort` и `route_advertise`; оба ряда должны иметь `ok=true` и `error_source=runtime` как отдельный PR-blocking assert в workflow.
- Runtime contract single-source CI contract: workflow не должен дублировать ad-hoc assert’ы для `route_advertise/peer_abort` вне `runtime/masque_runtime_contract_latest.json`; source-of-truth для этих проверок — агрегированный `checks.runtime_artifacts*`.
- Anti-bypass negative-control single-source CI contract: server-down проверки для `tcp_stream`/`udp`/`tcp_ip` должны исполняться через typed helper entrypoint (`masque_runtime_ci_gate_asserts.py --run-anti-bypass-negative-control ...`) с mode-level JSON-валидацией (`summary.ok=false`, `scenario.ok=false`, `error_class` classified), без shell-level дублирования orchestration в workflow.
- Anti-bypass typed artifact contract: helper `--run-anti-bypass-negative-control` обязан экспортировать `runtime/anti_bypass_latest.json` со stable schema (`schema=masque_anti_bypass_contract`, `schema_version`, `ok`, `modes[]`, `failures[]`), а workflow обязан выполнять отдельный blocking assert `--assert-anti-bypass-artifact` для fail-fast schema/shape drift detection.
- Anti-bypass cross-artifact parity contract: workflow-level blocking assert обязан валидировать typed parity между `runtime/anti_bypass_latest.json` и `runtime/masque_python_runner_summary.json` по `mode/scenario/error_class/error_source` через helper (`--assert-anti-bypass-cross-artifact`), без ad-hoc inline parsing в CI.
- Anti-bypass runtime-contract single-source CI contract: `runtime/masque_runtime_contract_latest.json::checks.anti_bypass_contract` обязан быть source-of-truth для PR-blocking anti-bypass (schema + required modes + classified error_class/source + non-zero runner exit + `summary_ok=false`), без отдельных ad-hoc anti-bypass assert шагов в workflow.
- Anti-bypass parity single-source contract: cross-artifact parity (`anti_bypass_latest.json` ↔ `masque_python_runner_summary.json`) должна валидироваться внутри `checks.anti_bypass_contract.parity_with_summary` агрегированного runtime contract; отдельные helper-level parity assert шаги в PR/nightly workflow не допускаются.
- Anti-bypass error-source normalization contract: для `anti_bypass_latest.json::modes[*].error_source` и `masque_python_runner_summary.json::results[*].error_source` пустые/unknown значения должны нормализоваться к `runtime` до parity-сравнения, чтобы исключить nondeterministic drift источника.
- Anti-bypass parity rows CI contract: workflow-level blocking assert обязан вызывать typed helper `masque_runtime_ci_gate_asserts.py --assert-anti-bypass-parity-rows` и проверять `checks.anti_bypass_contract.parity_with_summary.rows.{tcp_stream,udp,tcp_ip}.ok=true` без inline parsing.
- Lifecycle/scoped parity rows CI contract: workflow-level blocking assert обязан вызывать typed helper `masque_runtime_ci_gate_asserts.py --assert-scoped-parity-rows` и проверять per-row сигналы `checks.runtime_artifacts_error_source.rows.{peer_abort,route_advertise}.ok=true` и `checks.scoped_cross_artifact_parity.{negative_peer_abort_strict_ok,negative_peer_invalid_route_advertisement_strict_ok}=true` без inline parsing.
- ROUTE_ADVERTISEMENT/peer-abort scoped cross-artifact contract: `runtime/scoped_connect_ip_latest.json` (`negative_peer_invalid_route_advertisement`, `negative_peer_abort`) обязан быть типизированно согласован с `runtime/route_advertise_dual_signal_runtime.json` и `runtime/peer_abort_lifecycle_runtime.json` по `actual_error_class` + `result_error_class`; source parity допускает `scoped.error_source ∈ {runtime,compose_up}` при `runtime.error_source=runtime`.
- Scoped cross-artifact per-row CI gate contract: runtime contract и workflow обязаны валидировать strict per-row сигналы `checks.scoped_cross_artifact_parity.negative_peer_abort_strict_ok=true` и `checks.scoped_cross_artifact_parity.negative_peer_invalid_route_advertisement_strict_ok=true` отдельно от агрегированного `checks.scoped_cross_artifact_parity.ok`.
- Scoped boundary source normalization contract: для `negative_malformed_target`, `negative_peer_abort`, `negative_peer_invalid_route_advertisement` runner/typed helpers обязаны нормализовать `error_source` к стабильному enum `runtime|compose_up` (неизвестные/пустые значения -> `runtime`) до сериализации artifact.
- Peer-abort artifact contract: для negative peer-close path pre-docker summary/artifact обязаны экспортировать парные поля `actual_error_class` и `result_error_class` со значением `lifecycle` и `error_class_consistent=true` (fail-fast при `unknown`/рассинхроне).
- Peer-abort harness contract: `negative_peer_abort` в runner должен строиться из fast go runtime harness artifact (`MASQUE_PEER_ABORT_ARTIFACT_PATH`) с fail-fast на missing/invalid artifact (без synthetic probe fallback).
- Peer-abort CI gate contract: PR-blocking проверка `runtime/scoped_connect_ip_latest.json::negative_peer_abort` обязана валидировать `ok=true`, `actual_error_class=lifecycle`, `result_error_class=lifecycle`, `error_class_consistent=true`, `error_source ∈ {runtime,compose_up}`.
- RFC 9484/9298: scoped CONNECT-IP URI variables (`target`/`ipproto`) должны быть либо полноценно поддержаны, либо отвергнуты fail-fast как capability mismatch (без маскировки в generic 400).
- Scoped CONNECT-IP contract (client): `connect_ip_scope_*` применяются только при `transport_mode=connect_ip`; при отсутствии `{target}`/`{ipproto}` в `template_ip` — fail-fast reject.
- Scoped CONNECT-IP defaults: `target=0.0.0.0/0`, `ipproto=0` означают full-flow IPv4 wildcard; расширение до IPv6 только явным scoped target (без неявного dual-stack widening).
- Scoped CONNECT-IP negative observability: при malformed scope итоговая классификация ошибки должна быть консистентной между `rows[].actual_error_class` и вложенным `rows[].result.error_class` в `runtime/masque_python_runner_summary.json`.
- Scoped CONNECT-IP artifact contract: `runtime/scoped_connect_ip_latest.json` обязан экспортировать оба поля (`actual_error_class`, `result_error_class`) и булев `error_class_consistent=true` для negative malformed scoped path.
- Scoped CONNECT-IP artifact shape contract: `runtime/scoped_connect_ip_latest.json` для `negative_malformed_target` использует top-level object contract (без legacy `rows[]` lookup); CI/runtime validators обязаны проверять именно object shape.
- Scoped CONNECT-IP malformed harness contract: `negative_malformed_target` в runner должен строиться из fast go runtime harness artifact (`MASQUE_MALFORMED_SCOPED_ARTIFACT_PATH`) с fail-fast на missing/invalid artifact (без log string classification fallback).
- Scoped CONNECT-IP malformed boundary parity contract: pre-docker контракт обязан валидировать runtime/transport parity через отдельный fast transport artifact (`MASQUE_MALFORMED_SCOPED_TRANSPORT_ARTIFACT_PATH`) с совпадением `actual_error_class` + `result_error_class` и `error_class_consistent=true` на обоих слоях.
- Scoped CONNECT-IP CI gate contract: pre-docker/blocking проверка обязана валидировать одновременно `actual_error_class ∈ {capability,policy}`, `result_error_class ∈ {capability,policy}` и `error_class_consistent=true`.
- Scoped CONNECT-IP transport artifact CI contract: workflow-level assert для `runtime/malformed_scoped_transport_runtime.json` обязателен отдельно от агрегированного validator шага (fail-fast на missing/invalid schema drift).
- Scoped CONNECT-IP runtime/transport parity CI contract: workflow-level blocking assert обязан сравнивать `runtime/malformed_scoped_transport_runtime.json` c `runtime/scoped_connect_ip_latest.json::negative_malformed_target` по полям `actual_error_class`, `result_error_class/error_class`, `error_class_consistent` (fail-fast на любом drift между слоями).
- Scoped CONNECT-IP typed parity source contract: malformed scoped runtime/transport артефакты должны формироваться через общий typed helper (`ClassifyMalformedScopedTargetClassPair` + `BuildScopedErrorArtifact`) без дублирования string-based сериализации по слоям.
- Scoped CONNECT-IP server parse contract: malformed `target`/`ipproto` -> HTTP 400, unsupported flow-forwarding variables -> HTTP 501 (fail-closed и диагностируемо).
- Scoped CONNECT-IP server parse observability contract: parse reject (`400/501`) должен логироваться с детерминированным `error_class=capability` для fast gate traceability.
- Scoped CONNECT-IP client parse contract: ошибки `connect_ip_scope_*` (malformed target / unsupported template vars / scope-without-vars) должны стабильно классифицироваться как `capability` (без ухода в `unknown`).
- CONNECT-IP client status contract: non-2xx на establish path должны классифицироваться детерминированно (`400/501 -> capability`, `401 -> auth`, `403 -> policy`, прочие -> `transport_init`) без деградации в `unknown`.
- RFC 9484: ICMP feedback на policy/forwarding reject должен быть реализован и измерим.
- RFC 9484: ICMP policy-drop observability должен включать reason breakdown минимум `src_not_allowed` / `dst_not_allowed` / `proto_not_allowed`.
- Runtime observability contract: в `connect_ip_policy_drop_icmp_reason_total` ключи `src_not_allowed` / `dst_not_allowed` / `proto_not_allowed` должны присутствовать всегда (включая `0`) для стабильного artifact/schema сравнения.
- CONNECT-IP egress policy symmetry contract: policy-checks на egress (`composeDatagram`) должны оставаться fail-closed и типизированно наблюдаемыми наравне с ingress (без тихого bypass при src/dst/proto drift).
- Runtime contract artifact schema: `runtime/masque_runtime_contract_latest.json` обязан экспортировать версионированные поля `schema=masque_runtime_contract`, `schema_version` и стабильный top-level shape (`ok`, `runtime_dir`, `checks`, `failures`); validator обязан fail-fast при несовместимой schema drift.
- Runtime contract schema CI gate contract: workflow-level blocking assert для `runtime/masque_runtime_contract_latest.json` (exists + valid json + `schema=masque_runtime_contract` + integer `schema_version` + required top-level fields `ok/runtime_dir/checks/failures`) обязателен отдельно от агрегированного validator.
- Runtime contract schema single-source contract: schema/top-level shape assert должен исполняться через typed helper (`masque_runtime_ci_gate_asserts.py --assert-schema`) без inline Python в `ci.yml`.
- Runtime policy contract: быстрые go integration тесты обязаны валидировать связку `ClassifyError(...) == policy` для policy-reject path и отсутствие регрессии reason counters в observability snapshot.
- Runtime degraded contract: при `StateDegraded` ошибки `DialContext`/`ListenPacket` через `runtime is not ready` (errors.Join) обязаны сохранять первопричину в `LastError()` и стабильный `ClassifyError(...)` (без ухода в `unknown`).
- Runtime degraded coverage contract: быстрые go integration тесты обязаны отдельно покрывать как минимум классы `transport_init` и `tcp_dial` для not-ready path (`DialContext` + `ListenPacket`) с проверкой `errors.Is(..., cause)` и `ClassifyError(...)`.
- Runtime closed contract: при `StateClosed` пути `DialContext`/`ListenPacket` обязаны возвращать чистую lifecycle-ошибку `runtime is closed` без подмешивания stale cause из `LastError()`.
- Runtime lifecycle observability contract: `IsReady()` должен быть строго эквивалентен `LifecycleState()==Ready`; при `Degraded/Connecting/Reconnecting` `LastError()` обязан сохранять первопричину и оставаться диагностируемым через `errors.Join` в not-ready путях.
- Runtime peer-close classification contract: peer-induced close (`net.ErrClosed`, включая `CloseError.Remote=true` boundary) должен детерминированно классифицироваться как `lifecycle` (без деградации в `unknown`) в `ClassifyError(...)` и not-ready runtime путях.
- Lifecycle classifier parity contract: быстрые go-тесты должны фиксировать `ClassifyError(...) == lifecycle` для `CloseError.Remote=true` на runtime и transport слоях (pre-docker, без e2e/docker).
- CONNECT-STREAM server policy/auth contract: в server `template_tcp` обработчике отрицательные пути должны оставаться детерминированными (`401 -> auth`, `403 -> policy`) и покрываться быстрым go harness без зависимости от docker/H3 e2e.
- CONNECT-STREAM client status contract: в client `dialTCPStream` коды `401/403` обязаны мапиться в `ErrAuthFailed`/`ErrorClassAuth`, а прочие non-2xx — в `ErrTCPConnectStreamFailed`/`tcp_dial` (без смешения классов).
- CONNECT-STREAM client retry contract: retry допустим только для transient transport ошибок (`timeout`, `no recent network activity`, `idle timeout`, `application error`) с детерминированным budget (3 попытки); итоговая классификация после исчерпания retry — `ErrTCPConnectStreamFailed` / `tcp_dial`.
- CONNECT-STREAM client cancel contract: при `ctx.Done()` во время retry/backoff повторные попытки должны немедленно прекращаться без лишних dial/roundtrip; итоговая ошибка должна оставаться детерминированной и классифицируемой (`tcp_dial` либо `lifecycle` по первопричине).
- CONNECT-STREAM relay-phase deadline contract: после успешного `200 CONNECT` ошибки `context.DeadlineExceeded`/`context.Canceled` на stream read/write должны сохранять первопричину и детерминированно классифицироваться через `ErrTCPConnectStreamFailed` как `tcp_dial` (без `unknown`).
- Server lifecycle goroutine-safety contract: после успешного `Start` конкурентный `Close` не должен вызывать panic/data-race из-за обращения к зануленному `e.server`; serve-loop обязан работать с захваченным локальным указателем server instance.
- Server lifecycle restart contract: после успешного повторного `Start` `IsReady()` не должен оставаться `false` из-за stale `startErr`; ошибка старта должна очищаться на успешном запуске и не должна загрязняться non-start стадиями.
- Server lifecycle shutdown contract: штатный `Close()` не должен записывать fatal `startErr` из `Serve()` shutdown-ошибок (`net.ErrClosed`/`server closed`); но нештатный `Serve` fail вне shutdown обязан оставаться диагностируемым через `lastStartError()`.
- Runtime start cancellation contract: generic `masque` обязан передавать router start context в `Runtime.Start`, чтобы отмена старта детерминированно прерывала in-flight session dial/backoff (без фоновых «зомби»-подключений).
- Start-path cancellation observability: чистая отмена `context.Canceled` на пути `Runtime.Start` (retry/backoff, `NewSession`, `OpenIPSession`) должна экспортироваться как `errors.Join(ErrLifecycleClosed, cause)` чтобы `ClassifyError` давал `lifecycle` без глобального правила «любой context.Canceled → lifecycle» (иначе ломается join с `ErrTCPConnectStreamFailed` на TCP dial).
- `warp_masque` async startup error contract: при асинхронном старте ошибки control-plane/bootstrap должны детерминированно всплывать в первом operational call (`DialContext`/`ListenPacket`) и не маскироваться как `unknown`.
- `warp_masque` startup lifecycle classification contract: пока async startup не завершен или завершился ошибкой, первый operational call обязан возвращать typed ошибку с `errors.Is(err, transport/masque.ErrTransportInit)` и сохранением первопричины.

## 5) Обязательное чтение перед кодом

- `IDEAL-MASQUE-ARCHITECTURE.md`
- `MASQUE-ARCHITECTURE-GAP-CHECKLIST.md` — секции A–E закрыты; новые пункты только из воспроизводимых perf/e2e регрессий, не «на выдумку».
- `hiddify-core/docs/masque-warp-architecture.md`
- `hiddify-core/docs/masque-connect-ip-staged-closure.md`
- `hiddify-core/docs/masque-perf-gates.md`

## 6) Source-of-truth по слоям

**Вертикаль данных (сверху вниз, без обходов):** конфиг/router → `adapter/endpoint/manager` → `protocol/masque` (валидация, outbound `Endpoint`/`WarpEndpoint`, inbound `endpoint_server`) → `common/masque.Runtime` (lifecycle, retry до 3, кэш `OpenIPSession`) → `transport/masque` (QUIC/H3, `dialTCPStream`, netstack, CONNECT-UDP split) → vendor `third_party/masque-go`, `third_party/connect-ip-go` и HTTP/3 в `replace/quic-go-patched`. Прямых вызовов vendor из router/box быть не должно. Детальная нормативная таблица режимов и MTU — `IDEAL-MASQUE-ARCHITECTURE.md` §1–4.

- **Терминология TCP в TUN:** TCP через IP-туннель — `transport_mode=connect_ip` + netstack + `OpenIPSession`; поле `tcp_transport=connect_ip` в клиентском профиле этим путём не включается и отвергается в `validateMasqueOptions` (`protocol/masque/endpoint.go`). Краткая сводка — вводный блок `IDEAL-MASQUE-ARCHITECTURE.md`.

- **Фабрика сессии (канон transport):** только **`CoreClientFactory`** и **`DirectClientFactory`** (`transport/masque/transport.go`); wiring endpoint/runtime не должен обходить эту пару legacy-alias слоем (`IDEAL-MASQUE-ARCHITECTURE.md` §4; gap-checklist P0.2).

- Runtime: `hiddify-core/hiddify-sing-box/common/masque/runtime.go`
- Protocol: `hiddify-core/hiddify-sing-box/protocol/masque/endpoint.go`, `endpoint_server.go`
- Transport: `hiddify-core/hiddify-sing-box/transport/masque/transport.go`, `netstack_adapter.go`
- CONNECT-IP core: `hiddify-core/hiddify-sing-box/third_party/connect-ip-go/conn.go`
- **CONNECT-IP `mtu` vs TUN vs QUIC:** outbound `mtu` — верхний предел размера **полной IP-датаграммы** в CONNECT-IP plane (кламп в `transport/masque` при `CoreClientFactory.NewSession`, типичный потолок по умолчанию 1500 для interop с QUIC path); **`tun_mtu`** — локальный MTU интерфейса TUN в ОС (отдельный контур); **`quic_initial_packet_size`** — стартовый размер QUIC-пакета сессии. Лабораторный подъём потолка датаграммы: **`HIDDIFY_MASQUE_DATAGRAM_CEILING_MAX`** (`1280..65535`). Нормативная таблица и запрет смешения с payload sink — `IDEAL-MASQUE-ARCHITECTURE.md` §1 и §6.
- TCP-over-CONNECT-IP netstack: `transport/masque/netstack_adapter.go` — TCP dial через gVisor привязан к жизненному циклу `readLoop`: при ошибке приёма IP-датаграммы `DialContext` надо отменять вместе с закрытием `done`, иначе параллельный handshake может висеть без deadline на стороне вызывающего кода. Симметрично: фатальный исходящий путь (`WriteNotify` → `failWithError` → `session.Close`) обязан завершать `readLoop` и закрывать `done`, иначе тот же класс зависаний возможен при блокировке `DialContextTCP` на `context.Background()`.
- E2E entrypoint: `experiments/router/stand/l3router/masque_stand_runner.py`
- E2E compose/конфиги стенда: `experiments/router/stand/l3router/docker-compose.masque-e2e.yml` + `experiments/router/stand/l3router/configs/*.json` (клиент/сервер sing-box для сценариев); раннер оркестрирует bake/артефакты и env typed harness (см. `MASQUE_*_ARTIFACT_PATH` в `masque_stand_runner.py`).
- E2E binary (compose): образ ожидает `experiments/router/stand/l3router/artifacts/sing-box-linux-amd64`; при прогоне раннер собирает его из `hiddify-core/hiddify-sing-box` (`go build -tags with_masque`, target linux/amd64 для контейнера). Несогласованный бинарь → пересборка/digest mismatch в bake.
- **Относительные пути CI ↔ стенд:** в `hiddify-core/.github/workflows/ci.yml` сборка пишет бинарь в `../../experiments/router/stand/l3router/artifacts/...` из каталога `hiddify-sing-box`, а шаги раннера делают `cd experiments/router/stand/l3router` от **корня checkout**. В layout `hiddify-app/hiddify-core/hiddify-sing-box` это соответствует суперрепо (`../../` из sing-box → корень `hiddify-app`). Клон **только** submodule `hiddify-core` без родительского дерева с `experiments/` на ожидаемом уровне сломает эти пути — держать дерево как в CI или править `cwd`/out path осознанно.
- **Привязка `masque_stand_runner.py` к Go-модулю:** `CORE_DIR` = четыре уровня вверх от `experiments/router/stand/l3router` + `hiddify-core/hiddify-sing-box` (ожидается суперрепо вроде `hiddify-app`). Переезд стенда без синхронизации этого пути ломает `compile_singbox()`; в upstream, где `experiments/` лежит в том же репозитории, что и `hiddify-sing-box`, layout должен давать тот же эффективный путь к модулю или правится `CORE_DIR`.
- **Путь к Go-модулю в монорепо:** команды `go test` / `go build` для ядра — из `<корень hiddify-app>/hiddify-core/hiddify-sing-box` (не из соседнего каталога `hiddify-core` вне суперрепозитория, если submodule не отдельно клонирован).
- **Scoped CONNECT-IP (конфиг-пайплайн):** `option/masque.go` (`connect_ip_scope_target` / `connect_ip_scope_ipproto`) → `protocol/masque` (валидация шаблона + wiring в session) → `common/masque` (`Runtime`/опции сессии) → `transport/masque` + `third_party/connect-ip-go` (expansion URI / establish).
- CI gate source-of-truth: `hiddify-core/.github/workflows/ci.yml`, job **`masque-gates`** (pre-docker blocking): полная матрица `go test` по пакетам/`-run`, `third_party/connect-ip-go` IPv6-extension тест, opt-in `MASQUE_EXPERIMENTAL_QUIC=1` gate, сборка `sing-box` под Linux/amd64, Python-стенд и typed asserts. Локально держать одну «широкую» команду см. §8; детальный перечень не дублировать в этом файле.
- **Где крутится CI:** workflow лежит в репозитории `hiddify-core` (submodule). Суперрепозиторий `hiddify-app` отдельного `masque-gates` обычно не содержит — коммиты только в суперрепо без обновления gitlink submodule могут не запускать ядерные гейты.
- Агрегатор pre-docker контракта: `experiments/router/stand/l3router/masque_runtime_contract_validator.py` → `runtime/masque_runtime_contract_latest.json`; typed schema/row asserts — `experiments/router/stand/l3router/masque_runtime_ci_gate_asserts.py` (запуск с `cwd` как в CI: `experiments/router/stand/l3router`).
- Конфиг sing-box (option DSL для outbound/inbound полей): `hiddify-core/hiddify-sing-box/option/masque.go`
- Регистрация типов в движке: `include/registry.go::EndpointRegistry` всегда вызывает `registerMasqueEndpoint` → реализация в `include/masque.go` (`with_masque`, далее `protocol/masque/register.go`: `NewEndpoint` / `NewWarpEndpoint`) либо `include/masque_stub.go` (`!with_masque`, явная ошибка). Любой `go test`/`go build` по MASQUE-слоям и бинарь для стенда — с `-tags with_masque` (канон см. §8 и `masque_stand_runner.py`).
- **Box → endpoint wiring:** `box.go` и `experimental/libbox/config.go` прокидывают в контекст `include.EndpointRegistry()`; из JSON поднимаются конкретные endpoint-инстансы через `adapter/endpoint/manager.go` (`Manager`), а не напрямую из `protocol/masque` при старте процесса — отладка «вижу опцию, но не вызывается MASQUE» обычно между config parse и `Manager`.
- **Маршрут до outbound:** трафик доходит до `DialContext`/`ListenPacket`/`OpenIPSession` только если `route`/`rules` направляют сессию на outbound с тем же `tag`, что и у endpoint в конфиге; иначе MASQUE в логах «молчит», хотя inbound/outbound распарсены.
- Сервер/inbound CONNECT relay и packet sink: `hiddify-core/hiddify-sing-box/protocol/masque/endpoint_server.go`
- Быстрые тесты CONNECT-STREAM relay на сервере (без H3): `hiddify-core/hiddify-sing-box/protocol/masque/endpoint_server_relay_test.go`
- `warp_masque` + WARP control-plane интеграция: `hiddify-core/hiddify-sing-box/protocol/masque/endpoint_warp_masque.go`, `hiddify-core/hiddify-sing-box/protocol/masque/warp_control_adapter.go`

**Роли в терминах sing-box:** `outbound` типа `masque` / `warp_masque` — клиентская сторона (dial, `ListenPacket`, `Runtime` в `common/masque`); `inbound` типа `masque` — сервер HTTP/3 MASQUE (`endpoint_server.go`: CONNECT-UDP, CONNECT stream, CONNECT-IP relay/sink). Путаница inbound/outbound при правках чаще всего ведёт к изменению «не того» слоя.

**Ориентир по `protocol/masque` (не дублирует карту слоёв выше, только точки входа):** `endpoint.go` — outbound wiring, `validateMasqueOptions`, generic `masque`; `endpoint_server.go` — inbound HTTP/3 MASQUE (CONNECT-UDP/stream/IP relay + packet sink); `endpoint_warp_masque.go` + `warp_control_adapter.go` — только `warp_masque`; relay harness без QUIC — `endpoint_server_relay_test.go`.

**Три режима данных (не смешивать контракт):** CONNECT-UDP (UDP payload + привязка к ресурсу), CONNECT stream (byte stream по `template_tcp`), CONNECT-IP (IP-датаграмма CID=0 + capsules на управляющем потоке). Таблица и границы ответственности — `IDEAL-MASQUE-ARCHITECTURE.md` §4.

**Мнемоника outbound data-path (куда смотреть при регрессии):** CONNECT-UDP — `Runtime.ListenPacket` → UDP client (`third_party/masque-go`) → QUIC HTTP DATAGRAM после SETTINGS; CONNECT-stream — TCP `DialContext` (режим не `connect_ip`) → `dialTCPStream` / CONNECT по `template_tcp`; CONNECT-IP (TUN/packet-plane) — `OpenIPSession` → gVisor netstack (`netstack_adapter`) → `connectip.Conn` (`third_party/connect-ip-go`). Серверное зеркало тех же режимов — `endpoint_server.go` (relay/sink), не смешивать с outbound wiring в `endpoint.go`.

**CONNECT-IP server → UDP sink:** после декапсуляции к бэкенду в UDP-путь уходит **только UDP payload** (`connectIPNetPacketConn` в `endpoint_server.go`), не сырой IPv4/IPv6+UDP кадр; strict bulk/hash и байт-учёт завязаны на этот слой (см. `IDEAL-MASQUE-ARCHITECTURE.md` §6).

**Сервер CONNECT-IP, ICMP relay:** цикл «исходящая запись → ICMP feedback → повтор» в `connectIPNetPacketConn` ограничен `connectIPMaxICMPRelay` (защита от PTB/ICMP loop); детали — `IDEAL-MASQUE-ARCHITECTURE.md` §6.

**Контекст старта (generic client `masque`):** `NewBox` передаёт корневой `ctx` в `endpoint.Manager.Create` → конструктор `protocol/masque.NewEndpoint` сохраняет его в `baseCtx` → `Endpoint.Start(PostStart)` вызывает `runtime.Start(baseCtx)`. Таким образом отмена жизненного цикла box/router доходит в `NewSession`/`OpenIPSession` и в backoff между попытками `Runtime.Start`. `warp_masque`: `WarpEndpoint.Start` делает `context.WithCancel(baseCtx)` и в горутине вызывает `Runtime.Start` с этим производным `ctx` (отмена через `Close` / cancel).

**Классификация ошибок:** sentinel-ошибки и публичные константы классов — `transport/masque/errors.go`; агрегация в `transport/masque.ClassifyError` (runtime и Python-артефакты завязаны на эту карту). Новые негативные пути: добавить sentinel + ветку в `ClassifyError` и зеркало HTTP-статусов на inbound (`endpoint_server.go`), без string-match в endpoint.

**Outbound ownership:** `protocol/masque.Endpoint` / `WarpEndpoint` держит `common/masque.Runtime`; рабочий dataplane всегда `Runtime` → `transport/masque` (QUIC/H3, netstack, обёртки вокруг `connect-ip-go` / `masque-go`). Путь «router/box напрямую в vendor» для MASQUE отсутствует — при правках классификации ошибок или lifecycle сверять тройку `endpoint`/`runtime`/`transport` и зеркало HTTP статусов на inbound в `endpoint_server.go`.

**Граница `common/masque`:** слой runtime/фабрик не должен импортировать внутренности sing-box **router** — только абстракции factory/runtime; это фиксирует ядро MASQUE отдельно от маршрутизации (подробнее `IDEAL-MASQUE-ARCHITECTURE.md` §2–3).

**`hop_policy=chain`:** у каждого hop обязательны `server` и `server_port`, уникальные `tag`; граф проходит через `CM.BuildChain` в `common/masque`; hop’ы не смешивают QUIC-потоки разных расширенных CONNECT (канон формулировки — `IDEAL-MASQUE-ARCHITECTURE.md` §1).

**Git layout:** канонический код sing-box / `connect-ip-go` живёт под `hiddify-core/` (в монорепо часто git submodule): коммиты ядра делаются **внутри** `hiddify-core`, суперрепозиторий хранит только gitlink; после `git submodule update` сверять ожидаемый коммит submodule. Правки ядра и `go test` — только из cwd `hiddify-core/hiddify-sing-box` (не путать с возможным дублирующим `experiments/router/hiddify-sing-box`, если он присутствует в дереве). Слой Python-стенда и `runtime/*.json` — `experiments/router/stand/l3router/` (артефакты в `runtime/` относительно этого cwd).

**Pre-docker harness env (typed artifacts):** раннер выставляет пути для fast go harness: `MASQUE_MALFORMED_SCOPED_ARTIFACT_PATH`, `MASQUE_MALFORMED_SCOPED_TRANSPORT_ARTIFACT_PATH`, `MASQUE_ROUTE_ADVERTISE_ARTIFACT_PATH`, `MASQUE_PEER_ABORT_ARTIFACT_PATH` — см. `masque_stand_runner.py` (без synthetic fallback при missing artifact).

**Synthetic netstack tests (`transport/masque/*_test.go`):** in-process harness моделирует только границу сессии (`ReadPacket`/`WritePacket`, pipe/fatal-write stubs). Полный HTTP/3+QUIC+datagram dataplane и согласованность с `runtime/*.json` остаются за `masque_stand_runner.py` (docker/compose) и typed CI-артефактами; регрессии ядра на этой границе — быстрые go-тесты, регрессии стека — стенд.

**Агрегированный runtime contract:** `masque_runtime_contract_validator.py` валидирует **полный** набор входных `runtime/*.json` (в т.ч. `anti_bypass_latest.json`, malformed scoped transport/runtime). Запуск валидатора без свежего прогона раннера или с урезанным `runtime/` даёт ожидаемые `failures` — не трактовать как дефект ядра, пока не воспроизведён каноничный прогон CI (`masque_stand_runner.py --scenario all` и нужные negative-режимы).

**Поведение `--scenario all`:** по умолчанию размер 10 KiB; прогоняет `udp`, `tcp_stream`, `tcp_ip`, `tcp_ip_icmp`. При `--megabytes` большем 10 KiB в этом режиме выполняется только `tcp_ip` (см. вывод раннера). Полный PR-набор стенда и merged summary для валидатора — как в `hiddify-core/.github/workflows/ci.yml` (`all` → strict bulk `tcp_ip` 10/20/50 MiB → `tcp_ip_scoped` → anti-bypass helper → `masque_runtime_contract_validator.py`).

**Anti-bypass негативный контроль (стенд):** нельзя «ронять» только `compose stop` над `masque-server-core` — на том же контейнере слушает backend (socat); и нельзя каждый раз вызывать `compose_up` в раннере до сценария, иначе MASQUE снова поднимается. Канон: отключить только публичную сеть Docker `*masque-public` у `masque-server-core` (`docker network disconnect -f …`), раннер запускать с `MASQUE_STAND_SKIP_COMPOSE_UP=1` (без повторного compose), с `MASQUE_STAND_SKIP_SMOKE_CONTRACT_FILES=1` чтобы не затирать `smoke_*_latest.json` негативными 10 KiB прогонами; typed helper после режимов **мержит** `masque_python_runner_summary.json` (зелёный `tcp_ip` из backup или из `tcp_ip_scoped` positive + хвост из negative `tcp_stream`/`udp`/`tcp_ip` для parity).

**Fallback/tcp_mode ownership:** `validateMasqueOptions` / endpoint фиксирует конфиг-инварианты; `Runtime`/`coreSession` сами по себе не «переключают» CONNECT-UDP ↔ CONNECT-IP — матрица `masque_or_direct` / маршрутизация выше, см. `hiddify-core/docs/masque-warp-architecture.md` (не править не тот слой). В коде: комментарии у `validateMasqueOptions` (`protocol/masque/endpoint.go`) и у `runtimeImpl.DialContext` (`common/masque/runtime.go`).

**Perf/nightly контракт:** матрица real/soak, ожидаемые ключи JSON (`metrics`/`thresholds`/`result`/`error_class`) и команды вида `--scenario real`, `tcp_ip_iperf` — в `hiddify-core/docs/masque-perf-gates.md` (PR smoke там же перечислен).

**CONNECT-IP TTL / повтор отправки:** единственный декремент IPv4 TTL / IPv6 Hop Limit выполняется в `third_party/connect-ip-go` при `composeDatagram`. Повторная запись того же буфера снова уменьшает TTL; код выше `Conn` (retry/PMTB, см. `netstack_adapter.writePacketWithRetry`) обязан копировать слайс на каждую попытку — иначе bulk и ICMP-loop пути получают скрытый двойной декремент. Детали: `IDEAL-MASQUE-ARCHITECTURE.md` раздел 5.

**CONNECT-UDP (tunnel, не CONNECT-IP):** `ListenPacket` при режиме **не** `connect_ip` вызывает `udpClient.DialAddr` по `templateUDP`; исходящий большой приложенческий UDP режется в `masqueUDPDatagramSplitConn` до `masqueUDPWriteMax`, чтобы фрагменты соответствовали ожидаемому размеру QUIC HTTP DATAGRAM снизу. Gate «не слать DATAGRAM до SETTINGS» находится на уровне HTTP/3+QUIC-стека (`masque-go`/quic-go), не в этой обёртке — здесь только размер приложенческой записи. Вендорный CONNECT-UDP relay (HTTP/3 + stream mapping) — `hiddify-core/hiddify-sing-box/third_party/masque-go/`; inbound HTTP/3 сервер MASQUE — `protocol/masque/endpoint_server.go`.

**HTTP/3 DATAGRAM и SETTINGS (карта слоя):** разбор и валидация `SETTINGS_H3_DATAGRAM` — в патче HTTP/3 поверх QUIC (`hiddify-core/hiddify-sing-box/replace/quic-go-patched/http3`). Клиентский CONNECT-слой вендора проверяет, что после обмена настройками включены datagrams (`third_party/masque-go/client.go`, `settings.EnableDatagrams`), и поднимает `http3.Transport{EnableDatagrams: true}` вместе с `quic.Config{EnableDatagrams: true}`. Изменять порядок/условия отправки application datagram нужно там или в смежном HTTP/3, а не добавлять «тихие» обходы в sing-box UDP split-обёртках.

**CONNECT-UDP инъекция транспорта:** `coreSession.newUDPClient()` и HTTP/3 пути CONNECT-IP/TCP пробрасывают `ClientOptions.QUICDial` в `qmasque.Client` / `http3.Transport.Dial`, чтобы подменять `quic.Dial*` в тестах или экспериментах без docker. Контур по умолчанию — `quic.DialAddr`. In-process CONNECT-UDP без compose: `transport/masque/connect_udp_harness_test.go` — echo, split `WriteTo` (> `masqueUDPWriteMax`, дефолт до 1152 байт на фрагмент при стандартном ceiling), отказ по HTTP 403 до `Proxy`; шаблон совпадает с `/masque/udp/{target_host}/{target_port}` в `endpoint_server.go`.

**CONNECT-STREAM без docker (разделение слоёв):** серверный `template_tcp` (успешный relay + ответы `401/403` и согласованность с `transport/masque.ClassifyError`) — `protocol/masque/endpoint_server_relay_test.go` через `httptest`/локальный TCP-target, без QUIC. Клиентский `dialTCPStream` (retry budget, relay-phase, маппинг non-2xx) — по-прежнему **stub** `http.RoundTripper` в `transport/masque/transport_test.go` (`TestDialTCPStream*`). Полный in-process HTTP/3 CONNECT-stream byte path client↔server (паритет с `connect_udp_harness_test.go`) — отдельный track при необходимости.

**Владение CONNECT-IP на runtime/transport границе:** закрытие обёртки `connectIPPacketSession` не должно разрывать общий `connectip.Conn`; полный teardown только при закрытии `coreSession`. Иначе возможны гонки packet-plane/netstack с переиспользуемой сессией (`IDEAL-MASQUE-ARCHITECTURE.md` §3 Runtime).

**Vendor / replace layout (навигация):** форк/патчи QUIC+HTTP/3 — `hiddify-core/hiddify-sing-box/replace/quic-go-patched/`; CONNECT-UDP/MASQUE клиент — `third_party/masque-go/`; CONNECT-IP ядро — `third_party/connect-ip-go/`. Интеграция в sing-box только через `transport/masque`, `protocol/masque`, `common/masque` (не дублировать обходные вызовы vendor из router/box).

## 7) Автономный цикл итерации (без интерактива)

1. Взять 1-2 незакрытых пунктов из `MASQUE-ARCHITECTURE-GAP-CHECKLIST.md` (приоритет A → C → D). Если все чекбоксы секций A–E уже закрыты (см. конец чеклиста), работать по §10 (dataplane/perf), §8.1 и сигналам из `runtime/*summary*.json` / nightly, не выдумывая синтетические пункты чеклиста.
2. Внести минимально необходимые изменения строго в целевом слое.
3. Прогнать unit/race по затронутым пакетам.
4. Прогнать стендовый smoke (`--scenario all`) и нужные локальные сценарии, только если были выполнены значительные изменения в коде, которые закрывают слой полностью, с целесообразной тратой времени на тест.
5. Обновить этот файл: только актуальный handoff (без длинной истории), что закрыто, что осталось, следующая задача.

## 8) Тестирование и anti-hang протокол

- Локально (Windows/PowerShell): цепочка через `;`, не `&&`. Быстрый канонический pre-docker слой совпадает с первым шагом CI: `Set-Location …\hiddify-core\hiddify-sing-box; go test ./protocol/masque ./transport/masque ./common/masque ./include -tags with_masque` и `go test -race ./protocol/masque ./transport/masque -tags with_masque`. Точечные `-run`/доп. пакеты — только как в job `masque-gates`.
- **Windows + `-race`:** `./transport/masque` под race может нестабильно падать внутри `crypto/tls`+QUIC (fault в рантайме), при том что `./protocol/masque -race` обычно зелёный; эталон для race-матрицы — job `masque-gates` на **Linux**. При локальном краше — не трактовать как регрессию ядра без подтверждения на Linux/CI.
- **Windows и Docker Desktop:** если в среде доступны `docker` и `docker compose` (типично через Docker Desktop), агент **не пропускает** полноценное стендовое тестирование dataplane только из-за того, что хост Windows. Полный контур `masque_stand_runner.py` + `docker-compose.masque-e2e.yml` — тот же путь воспроизведения, что и в CI; отсутствие Docker нужно явно зафиксировать в handoff, а не подменять только pre-docker Go-тестами без пометки.
- **Windows Docker Desktop и strict bulk:** сценарий `tcp_ip --megabytes 10/20/50` иногда даёт неполный drain на приёмнике (`budget_exceeded`, рост `loss_pct`) из‑за сети/буферов VM и хвоста `socat`, при том что smoke `all` зелёный. Не раздувать бюджеты раннера ради «зелени» на хосте: эталон strict bulk — Linux job `masque-gates` (`ubuntu-latest`); локальный fail на Windows трактовать как сигнал окружения до воспроизведения на Linux.
- **Windows и WSL (снимаем блокер «просто Windows»):** на типичном Windows-хосте **доступен WSL** (обычно WSL2 с дистрибутивом вроде Ubuntu). Если есть **любые сомнения** в релевантности результатов на нативном Win32 + Docker Desktop (race/QUIC, strict bulk, iperf/socat цепочки), агент **обязан** рассмотреть прогон в **Linux-среде внутри WSL**: там же дерево репозитория (`git clone` в `~` или монтирование из Windows через путь вида `/mnt/<drive>/…`), там же `docker`/`docker compose` (встроенный в WSL или backend Docker Desktop для WSL) и те же команды `go test` + `python …/masque_stand_runner.py`. Это выравнивает локальный контур к **Linux** без отдельной физической машины и не даёт использовать «мы на Windows» как оправдание пропуска стенда или смягчения выводов, когда WSL настроить реально.
- Базовый smoke: `python experiments/router/stand/l3router/masque_stand_runner.py --scenario all`
- Для `third_party/connect-ip-go` это **вложенный** `go.mod` (`module github.com/quic-go/connect-ip-go`): из корня `hiddify-sing-box` использовать `go test -C third_party/connect-ip-go ./...` либо `Set-Location third_party/connect-ip-go` и `go test ./...` (путь `go test ./third_party/connect-ip-go` из родительского модуля неработоспособен). На **Windows** интеграционные `proxy_test` (QUIC loopback, IPv6 TTL) иногда дают таймаут/другую цепочку ошибок; эталон — **Linux** (job `masque-gates` или WSL), локальный fail win32 без Linux-репрода не считать блокирующим сигналом регрессии ядра.
- Для packet-plane изменений: строгий bulk `tcp_ip` (`10/20/50MB`) обязателен.
- Для dataplane-готовности обязательны реальные high-speed сценарии (`--scenario real`, `tcp_ip_iperf`, `tun_rule_masque_perf`) с machine-readable артефактами.
- Любой стендовый прогон с wall-clock guard; timeout = FAIL.
- При timeout сохранить `runtime/*.json` + docker-логи и разбирать первопричину (без слепого увеличения лимитов).
- Для anti-bypass negative control (server down) обязательно проверять не только non-zero exit, но и JSON-контракт: `runtime/masque_python_runner_summary.json` с `summary.ok=false`, `tcp_ip.ok=false`, классифицированным `error_class`.
- Для CONNECT-IP observability в CI/артефактах обязательно проверять map `connect_ip_policy_drop_icmp_reason_total` с ключами `src_not_allowed` / `dst_not_allowed` / `proto_not_allowed` и неотрицательными значениями.
- **Локальная ловушка parity / порядок артефактов:** `masque_python_runner_summary.json` перезаписывается каждым прогоном раннера. Канон для агрегированного контракта — порядок job `masque-gates` в `hiddify-core/.github/workflows/ci.yml`: `all` → strict bulk `tcp_ip` (10/20/50 MiB) → `tcp_ip_scoped` → `masque_runtime_ci_gate_asserts.py` с тройным `--run-anti-bypass-negative-control` → `masque_runtime_contract_validator.py` + typed asserts. После только `all` validator увидит расхождение с уже записанным `anti_bypass_latest.json`, пока не выполнен anti-bypass-блок (мерж). Запуск изолированного `tcp_ip_scoped` **после** уже смерженного summary снова перезапишет файл и сломает parity — либо соблюдать порядок CI, либо повторить `all`/цепочку до anti-bypass.

## 8.1) Реальные результаты стенда как показатель успеха

- Реальные стендовые результаты — **основной** показатель успеха итерации по dataplane, а не вторичная проверка после структурных правок.
- После существенных изменений ожидаются измеримые результаты по `connect_stream`, `connect_ip`, `connect_udp` (throughput/loss/hash/settle/error_class) в runtime-артефактах.
- Если на высоких скоростях остаются `iperf exit`, hang, `budget_exceeded`, hash/loss drift — слой dataplane считается незавершенным.
- Рефакторинг, RFC-согласование и CI-контракты считаются промежуточными шагами; целевой результат — рабочий dataplane, подтвержденный реальными high-speed прогонами.
- **Зависания:** реальные сценарии и тяжёлые bulk могут зависнуть из-за дефекта транспорта, раннера или окружения; это нужно **жёстко контролировать** (wall-clock лимиты, без бесконечного ожидания), при сбое сохранять `runtime/*.json` и docker-логи и искать первопричину.
- **Ограничения harness:** сам стенд (`masque_stand_runner.py`, таймауты socat/iperf, матрицы) может быть тяжёлым, устаревшим или неоптимально оркестрованным — **возможны ложные срабатывания**; их отличать от реальной деградации dataplane по повторяемости и по физике пути (loss/hash, стабильность на упрощённых сценариях).
- **Когда гонять:** тяжёлые perf/real прогоны не обязательны на каждом микродиффе, но после правок dataplane их **нужно** запускать целенаправленно и **фиксировать артефакты** как baseline для сравнения регрессий; «не гоняем, потому что долго» без записи последнего известного good — недопустимо для ветки perf.
- **Главный критерий по объёму и скорости:** bulk **10–20 MiB** по всем релевантным сценариям стенда (`udp`, `tcp_stream`, `tcp_ip`; плюс scoped/real-matrix по контракту `masque-perf-gates.md`) должны проходить на **полной скорости отправки** без искусственных замедлений (rate limit, паузы, урезание chunk ниже оптимальной для реальных сценариев) **без особой причины**, задокументированной как диагностика; цель — **без потерь** и в рамках порогов артефактов. Потери или «зелень» только на низкой скорости — проблема **dataplane**, а не оправдание тестом. Замеделение допустимы для датаграмм-сценариев на адекватных величинах скорости (выше 500 мбит\с), после которых результат упирается в буферы - это допустимо, но также требует расследования размера буферов, количества воркеров и тд.
- **Проектирование нагрузки:** транспорт и путь данных должны выдерживать **значительную** нагрузку (высокий суммарный битрейт, один поток и несколько потоков/сессий там, где сценарий это моделирует). **50 Мбит/с считать низкой, несущественной планкой** для оценки готовности; ориентиры по конкретным rate/size — в `hiddify-core/docs/masque-perf-gates.md`.

## 9) Definition of Done (итерация)

- Не нарушены mode/fallback контракты.
- Нет зависаний, все таймауты детерминированны.
- Unit/race по затронутым пакетам — PASS.
- Smoke `--scenario all` — PASS.
- Для packet-plane: strict bulk `tcp_ip` (`10/20/50MB`) — PASS (budget/hash/loss).
- Для dataplane production readiness: реальные high-speed сценарии (`tcp_ip_iperf` и релевантные `tun_rule_masque_perf`) — PASS без деградации и зависаний.
- Для всех релевантных сценариев стенда: bulk **10–20 MiB** на полной скорости без искусственного троттлинга (кроме явной диагностики) — PASS по loss/hash/settle; ориентиры скорости — не ниже смыслового минимума из `masque-perf-gates.md` (50 Мбит/с как «достаточно» не принимается).
- Обязательные smoke-артефакты валидны и обновлены.

## 10) Handoff для следующей итерации (обновлять каждый проход)

- Фокус: **полная цепочка PR `masque-gates`** (`hiddify-core/.github/workflows/ci.yml`): strict bulk `tcp_ip`, `tcp_ip_scoped`, anti-bypass + `masque_runtime_contract_validator.py`; затем **dataplane/perf** по `masque-perf-gates.md`. Коммиты только в суперрепо без обновления gitlink `hiddify-core` не запускают ядерный workflow в submodule.
- Сделано (этот проход): `MASQUE-ARCHITECTURE-GAP-CHECKLIST.md` секции A–E помечены закрытыми — новые пункты только из воспроизводимых perf/e2e регрессий (§5). Pre-docker (`hiddify-core/hiddify-sing-box`, Windows): `go test ./protocol/masque ./transport/masque ./common/masque ./include -tags with_masque -count=1` **PASS**; `go test -race ./protocol/masque ./transport/masque -tags with_masque -count=1` **PASS**. Docker smoke: `python experiments/router/stand/l3router/masque_stand_runner.py --scenario all` **PASS** (Docker Desktop / Win, `summary.ok=true`). В §6 добавлена строка про **`CORE_DIR` / layout** раннера относительно суперрепо.
- Следующее: как в CI после `all` — strict bulk `tcp_ip` (`--megabytes 10`, `20`, `50`) → `tcp_ip_scoped` → `masque_runtime_ci_gate_asserts.py` (triple `--run-anti-bypass-negative-control` + `--assert-schema` + parity rows) → `masque_runtime_contract_validator.py`; на Linux — `go test -C third_party/connect-ip-go ./...` (Win proxy_test флап — см. §8). Затем high-speed матрица (`real`, `tcp_ip_iperf`, `tun_rule_masque_perf`) и сверка с `runtime/nightly_*`. Tech-debt: in-process HTTP/3 CONNECT-stream client или `QUICDial`-изоляция вместо RoundTripper stub (`transport/masque/transport_test.go`, §13).

### 10.1) Дополнительный архитектурный guard (кратко)

- Single-source CI guard: любые новые anti-bypass/scoped/lifecycle blocking-assert’ы должны добавляться только как поля в `runtime/masque_runtime_contract_latest.json::checks.*`; расширение `masque_runtime_ci_gate_asserts.py` допускается только для schema-gate и typed row-level чтения уже агрегированных checks, без прямых helper-level cross-artifact assert команд.

## 11) Общие требования качества реализации

- Реализация путей TCP/UDP/ICMP должна быть полной и профессиональной, без «временных упрощений» в production hot path.
- Разрешен агрессивный рефакторинг, если он уменьшает перегрузку hot path и улучшает соответствие RFC/архитектуре.
- Любые staged/legacy оговорки должны быть явными, ограниченными и проверяемыми тестами/документами. Если остается легаси\тестовый код, то он постепенно и жестко должен удаляться без каких либо упоминаний и обратной совместимости, включая тесты, чтобы увеличить прозрачность общей структуры.

## 12) Важно не зацикливаться на тестировании, а доводить реальный код до необходимого профессионального уровня, чтобы он соответствовал паттернам, rfc стандартам, sing box совместимости и кастомизации через конфиг необходимых и важных параметров, закрывал в полной мере необходимые пути, следовал необходимым паттернам, был достаточно оптимизирован и тд. Тестировать в полной мере мере следует тогда, когда измененный слой будет полностью готов и отрефакторен(если надо - агрессивно) в нужной мере и признан готовым к тестированию.
Когда будут готовы go тесты со сценариями клиент-сервер, они будут предпочтительнее для промежуточного тестирования полному e2e тестированию с докер стендом.

## 13) Go тесты. Расширять интеграционные сценарии client↔server в Go (без docker). CONNECT-UDP end-to-end: `transport/masque/connect_udp_harness_test.go`. CONNECT-STREAM server relay + коды отказа: `protocol/masque/endpoint_server_relay_test.go`. CONNECT-STREAM client контракт (ретраи, relay-phase): `transport/masque/transport_test.go` (`TestDialTCPStream*`). Следующий опциональный слой — полный HTTP/3 client path для TCP в transport-слое (не только RoundTripper stub) либо `QUICDial`-изоляция, где это снижает flakiness.