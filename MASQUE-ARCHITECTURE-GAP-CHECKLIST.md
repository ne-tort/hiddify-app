# MASQUE Architecture Gap Checklist

Документ фиксирует расхождения между текущей реализацией и `IDEAL-MASQUE-ARCHITECTURE.md`, а также пошаговый чеклист закрытия.  
Цель: перед кодовой реализацией зафиксировать **что именно исправляем**, **почему это важно**, и **каким сигналом считаем пункт закрытым**.

## Scope

- Клиент/сервер MASQUE dataplane для `connect_udp`, `connect_stream`, `connect_ip`.
- CONNECT-IP packet-plane (RFC 9484) и HTTP Datagram/Capsule semantics (RFC 9297/9298).
- Тестовые/CI-gates для TUN-only CONNECT-IP.

## P0: Критичные расхождения

### 1) Unknown capsule handling (RFC 9297)
- **Проблема:** неизвестный тип capsule завершает поток, вместо silent skip.
- **Где:** `hiddify-core/hiddify-sing-box/third_party/connect-ip-go/conn.go`.
- **Риск:** несовместимость с расширениями и межвендорный interop-break.
- **Направление фикса:** unknown capsule -> skip/continue + отдельный счётчик причины.

### 2) Factory drift (legacy alias vs реальная фабрика)
- **Проблема:** в историческом коде endpoint использовал legacy alias-фабрику вместо прямой `CoreClientFactory`/`DirectClientFactory` стратегии.
- **Где:** `hiddify-core/hiddify-sing-box/protocol/masque/endpoint.go`, `hiddify-core/hiddify-sing-box/transport/masque/transport.go`.
- **Риск:** архитектурный дрейф, риск build/runtime рассинхрона, ошибки handoff.
- **Направление фикса:** унифицировать фабрику в endpoint и документации.

### 3) CONNECT-IP policy-drop без ICMP feedback
- **Проблема:** TODO на ICMP при policy reject (src/dst/proto).
- **Где:** `hiddify-core/hiddify-sing-box/third_party/connect-ip-go/conn.go`.
- **Риск:** плохая диагностируемость/восстановление, сложнее ловить bulk деградации.
- **Направление фикса:** генерировать ICMP unreachable/administratively prohibited + метрики.

### 4) QUIC DATAGRAM readiness (SETTINGS gate)
- **Проблема:** в чеклистах/тестах не зафиксирован явный gate на отправку DATAGRAM только после подтверждённой поддержки (H3 SETTINGS).
- **Где:** RFC 9297/9298 ожидания, `transport/masque` runtime path.
- **Риск:** interop-нестабильность и протокольные ошибки на раннем старте сессии.
- **Направление фикса:** ввести явную readiness-проверку и тест-кейс на раннюю отправку.

## P1: Важные расхождения

### 5) IPv6 extension chain для policy matching
- **Проблема:** `ipproto` проверяется без обхода цепочки IPv6 extension headers.
- **Где:** `hiddify-core/hiddify-sing-box/third_party/connect-ip-go/conn.go` (TODO к RFC 9484 §4.8).
- **Риск:** неверное решение policy для IPv6 трафика.
- **Направление фикса:** реализовать parser цепочки до final upper-layer protocol.

### 6) Рассинхрон test entrypoint между docs и CI
- **Проблема:** часть доков/CI ссылалась на shell-wrapper, которого нет в дереве; канон проекта — Python entrypoint.
- **Где:** `AGENTS.md`, `hiddify-core/docs/masque-perf-gates.md`, `hiddify-core/docs/masque-connect-ip-staged-closure.md`, `hiddify-core/.github/workflows/ci.yml`, `experiments/router/stand/l3router`.
- **Риск:** невалидные gates, ложное ощущение покрытия.
- **Направление фикса:** единый entrypoint (`masque_stand_runner.py`) и явная политика для CI wrappers.

### 7) PR-гейты не закрывают strict bulk acceptance
- **Проблема:** PR smoke есть, но strict bulk (`10/20/50MB`) не блокирует регресс.
- **Где:** `AGENTS.md` (цель), `hiddify-core/.github/workflows/ci.yml`.
- **Риск:** деградация bulk проходит в main.
- **Направление фикса:** добавить обязательный bulk gate или эквивалентный blocking contract.

### 8) CONNECT-IP bridge ограничен IPv4
- **Проблема:** `connectIPUDPPacketConn` принимает/строит только IPv4 UDP (`parseIPv4UDPPacket`, reject IPv6).
- **Где:** `hiddify-core/hiddify-sing-box/transport/masque/transport.go`.
- **Риск:** неполный функционал packet-plane и неожиданные отказы IPv6 сценариев.
- **Направление фикса:** явно зафиксировать ограничение как контракт или добавить IPv6 bridge path.
- **Статус закрытия (контракт):** в runner/CI добавлен обязательный artifact-assert (`tcp_ip.connect_ip_udp_bridge_contract=ipv4_only`, `connect_ip_udp_bridge_ipv6_supported=false`), docs синхронизированы; реализация IPv6 bridge остаётся отдельным feature-track.

### 9) MTU contract drift (`endpoint` vs `transport` clamp)
- **Проблема:** endpoint принимает `mtu` до 65535, но transport clamp’ит effective ceiling до фиксированного верха (по умолчанию 1500).
- **Где:** `protocol/masque/endpoint.go`, `transport/masque/transport.go`, `IDEAL-MASQUE-ARCHITECTURE.md` (контракт `tun_mtu` / `masque_datagram_ceiling`).
- **Риск:** скрытое отличие ожиданий пользователя от фактического поведения.
- **Статус закрытия (частично):** документирован развод `tun_mtu` vs `masque_datagram_ceiling`; верх клампа вынесен в **`HIDDIFY_MASQUE_DATAGRAM_CEILING_MAX`** (лабораторный jumbo); PTB при `DatagramTooLarge` берёт MTU из **`MaxDatagramPayloadSize`** (`third_party/connect-ip-go/conn.go`); **`warp_masque`** пробрасывает `ConnectIPDatagramCeiling` как generic `masque`. Полный jumbo end-to-end остаётся за interop/CI-политикой.

### 10) CONNECT-IP flow scoping частично реализован (`target`/`ipproto` в URI template)
- **Проблема:** отсутствует поддержка scoped CONNECT-IP URI variables (flow forwarding), интероп ограничен базовым режимом.
- **Где:** `hiddify-core/hiddify-sing-box/third_party/connect-ip-go/client.go`, `hiddify-core/hiddify-sing-box/third_party/connect-ip-go/request.go`.
- **Риск:** неполная совместимость с MASQUE серверами, использующими scoped policy.
- **Направление фикса:** добавить поддержку/валидацию flow forwarding template variables или явно fail-fast с отдельным классом capability.
- **Статус закрытия:** 
  - template-level support добавлен в `connect-ip-go`: серверный parse/validation `target`/`ipproto` + клиентский template expansion (default full-flow scope `target=0.0.0.0/0`, `ipproto=0`);
  - runtime/config wiring добавлен в sing-box слои (`option` -> `protocol` -> `common/runtime` -> `transport`): `connect_ip_scope_target` / `connect_ip_scope_ipproto`, fail-fast при некорректном режиме/шаблоне и observability-экспорт scope-полей;
  - e2e/docs контракт зафиксирован: positive scope observability (`10.200.0.2/32`, `17`) + malformed negative class/source (`capability|policy`, `runtime|compose_up`);
  - CI closure: `hiddify-core/.github/workflows/ci.yml` теперь запускает blocking `--scenario tcp_ip_scoped` и валидирует `runtime/scoped_connect_ip_latest.json` как обязательный PR artifact;
  - локальная валидация closure подтверждена: `python experiments/router/stand/l3router/masque_stand_runner.py --scenario tcp_ip_scoped` и `--scenario all` -> `summary.ok=true` и scoped contract green.

### 11) Валидация ROUTE_ADVERTISEMENT не enforce'ит полный RFC-контракт
- **Проблема:** порядок/overlap-ограничения и abort semantics реализованы не полностью.
- **Где:** `hiddify-core/hiddify-sing-box/third_party/connect-ip-go/capsule.go`.
- **Риск:** policy bypass/расхождения маршрутизации при межвендорном interop.
- **Направление фикса:** полноценная RFC-валидация route ranges + негативные тесты на malformed ads.
- **Статус закрытия:** введен единый validator `validateRouteAdvertisementRanges` (strict start-order + non-overlap в одной family) и подключен как на parse path, так и на `AdvertiseRoute`; добавлены негативные тесты на unordered/overlap, invalid advertisement приводит к fail-fast (abort contract). Дополнительно введен typed sentinel `ErrInvalidRouteAdvertisement`, endpoint-классификатор переведен на `errors.Is` без string-match.

## P2: Низкий приоритет / hygiene

### 12) Дрейф формулировок по `tcp_transport=connect_ip`
- **Проблема:** исторические формулировки могли конфликтовать с текущей валидацией endpoint.
- **Где:** `hiddify-core/docs/masque-connect-ip-staged-closure.md`, `hiddify-core/hiddify-sing-box/protocol/masque/endpoint.go`.
- **Риск:** неверные ожидания интеграторов.
- **Направление фикса:** держать единый канон: TUN-only TCP-over-CONNECT-IP = `transport_mode=connect_ip` + netstack.

### 13) Непрозрачность fallback/tcp_mode ownership
- **Проблема:** поля валидируются на endpoint, но логика fallback частично живёт выше transport; это не всегда очевидно.
- **Где:** `protocol/masque/endpoint.go`, `common/masque/runtime.go`, `transport/masque/transport.go`.
- **Риск:** ошибочные правки “не в том слое”.
- **Направление фикса:** закрепить ownership в код-комментариях/доках и telemetry hooks.

### 14) Egress policy checks в `composeDatagram` не завершены
- **Проблема:** в egress path есть `TODO` на src/dst/ipproto checks; policy симметрична не полностью.
- **Где:** `hiddify-core/hiddify-sing-box/third_party/connect-ip-go/conn.go`.
- **Риск:** возможный policy drift между ingress и egress.
- **Направление фикса:** реализовать или явно задокументировать ownership этих проверок на другом слое.

---

## Чекбокс-план исправления

### A. Protocol compliance
- [x] Реализовать silent-skip для unknown capsule type в `connect-ip-go` path.
- [x] Добавить счётчик `connect_ip_capsule_unknown_total` и reason breakdown.
- [x] Реализовать IPv6 extension header walk для `ipproto` policy match (RFC 9484 §4.8).
- [x] Реализовать ICMP feedback на policy-drop (минимум для src/dst/proto reject путей) + observability reason breakdown (`src_not_allowed` / `dst_not_allowed` / `proto_not_allowed`).
- [x] Добавить gate на readiness QUIC DATAGRAM (после H3 SETTINGS) и тест ранней отправки.
- [x] Закрыть TODO в `composeDatagram` по egress checks (или зафиксировать ownership на другом слое).

### B. Architecture consistency
- [x] Убрать/заменить `TM.M2ClientFactory{}` на реальную factory strategy (`CoreClientFactory`/selector).
- [x] Синхронизировать названия фабрики в `IDEAL`, `AGENTS`, коде и тестах.
- [x] Зафиксировать ownership `fallback_policy`/`tcp_mode` (валидация vs runtime behavior) в одном месте документации.

### C. Test and CI gates
- [x] Привести CI к единому entrypoint (`masque_stand_runner.py`) либо добавить проверку существования shell-wrapper до запуска.
- [x] Добавить PR-blocking CONNECT-IP negative control (сервер down -> ожидаемый fail + artifact assertion `summary.ok=false`, `tcp_ip.ok=false`, `error_class!=none`).
- [x] Добавить PR-blocking strict bulk gate (`10MB`, `20MB`, `50MB`) или эквивалентный blocking contract.
- [x] Вынести CONNECT-IP observability contract в явную проверку артефакта (`metrics/thresholds/error_class/result` + delta counters).
- [x] Добавить preflight-проверку существования всех вызываемых CI скриптов/entrypoints (fail-fast при отсутствии файла).
- [x] Добавить проверку `runtime/masque_python_runner_summary.json` в PR artifact contract.
- [x] Обновить CI trigger policy: изменения в `AGENTS.md`/`MASQUE-ARCHITECTURE-GAP-CHECKLIST.md`/`hiddify-core/docs/masque-*.md` должны запускать `masque-gates`.
- [x] Зафиксировать P1#8 как явный IPv4-only artifact contract для `connectIPUDPPacketConn` (до отдельной реализации IPv6 bridge path).
- [x] Расширить быстрые runtime go integration контракты на degraded not-ready path для классов `policy`/`transport_init`/`tcp_dial` с проверкой `DialContext` и `ListenPacket` (`errors.Is` на первопричину + стабильный `ClassifyError` + сохранение `LastError`).
- [x] Добавить runtime integration контракт для `StateClosed`: `DialContext`/`ListenPacket` возвращают `runtime is closed` без join со stale `LastError` из degraded-состояния.
- [x] Добавить быстрый `connect_stream` client-server go harness (in-memory/httptest) для TCP relay: success bidirectional path + negative auth/policy statuses (`401/403`) и привязка к error-class контракту (`auth`/`policy`) без docker.
- [x] Добавить fast endpoint-level lifecycle harness для peer-side abort path (`net.ErrClosed` / `CloseError.Remote`) и детерминированный `error_class=lifecycle` в `protocol/masque` (без деградации в `unknown`).
- [x] Добавить pre-docker runner contract row для peer-abort (`negative_peer_abort`) в `runtime/masque_python_runner_summary.json` и `runtime/scoped_connect_ip_latest.json` с проверкой `actual_error_class=lifecycle`, `result_error_class=lifecycle`, `error_class_consistent=true`.
- [x] Добавить PR-blocking CI assert для `runtime/scoped_connect_ip_latest.json::negative_peer_abort` (`ok=true`, `actual_error_class=lifecycle`, `result_error_class=lifecycle`, `error_class_consistent=true`, `error_source in {runtime,compose_up}`).
- [x] Добавить runtime+transport lifecycle parity harness для peer-close boundary (`CloseError.Remote=true`): not-ready `DialContext`/`ListenPacket` сохраняют `errors.Is(..., net.ErrClosed)` и `ClassifyError(...) == lifecycle` в pre-docker go тестах.
- [x] Вынести lifecycle parity harness в отдельный явный CI run-target (pre-docker blocking): `go test ./common/masque -run TestRuntimePeerRemoteCloseNotReadyClassifiedAsLifecycle` + `go test ./transport/masque -run TestClassifyError` + `go test ./protocol/masque -run TestConnectIPRouteAdvertisePeerCloseLifecycleParity` с `-tags with_masque`.

### E. Incident closure: strict bulk regression (May 2026)
- [x] Устранить server-side packet contract drift: UDP путь в `connectIPNetPacketConn` передаёт payload вместо raw IP frame.
- [x] Устранить нестабильность bulk генератора: перейти с `head|pv|socat` на paced python UDP sender в `masque_stand_runner.py`.
- [x] Переподтвердить acceptance: `tcp_ip` strict bulk `10/20/50MB` + `--scenario all` (hash/loss/budget/observability green).

### D. Documentation closure
- [x] Синхронизировать `README`/`MASQUE_STAND_RESULTS.md`/docs с фактическими entrypoint и флагами.
- [x] Удалить устаревшие упоминания legacy/staged env-флагов, не используемых в текущем каноне.
- [x] Добавить в handoff ссылку на этот чеклист как обязательный трекер закрытия.

---

## Критерии закрытия документа

- Все пункты секции **A** закрыты и подтверждены unit/integration тестами (включая unknown capsule, IPv6 ext chain, ICMP policy-drop, DATAGRAM readiness).
- Все пункты секции **C** отражены в CI с воспроизводимыми JSON-артефактами и preflight-проверкой entrypoint/script availability.
- `AGENTS.md` и `IDEAL-MASQUE-ARCHITECTURE.md` не содержат противоречий по transport/factory/test-entrypoint.
- В nightly/perf нет необъяснённых регрессов по strict bulk относительно последнего baseline.
