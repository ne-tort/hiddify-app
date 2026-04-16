# Правила для ассистентов (репозиторий `hiddify-app`)

## Цель по `l3router`

Довести `l3router` до WG-подобной peer-архитектуры (Variant B), где:

- dataplane идентифицирует ingress только через `PeerID`,
- сопоставление transport identity -> `PeerID` делается один раз на сессию в server endpoint,
- внутри `common/l3router` (ядро) нет зависимости от user/session модели transport-протоколов.

`l3router` остается L3-роутером без криптографии, а крипто-идентификация выполняется inbound-слоем sing-box.

## Архитектурные инварианты (обязательно)

- В hot path запрещены `SessionKey`/string identity и per-packet lookup по user/session.
- WG-подобная семантика маршрутизации: `AllowedSrc`/anti-spoof + `dst` LPM + no-loop.
- Базовый эксплуатационный режим — static-first (`routes` в JSON).
- Runtime route API допустим только как ops-инструмент, не как обязательный control-plane.
- Клиентский UX сохраняется: vanilla sing-box client (`tun+address`) работает без дополнительных клиентских изменений.

## KPI и регрессионные границы

- Основной KPI: сокращение single-thread hot path (`plain`, `x1`, `1280+`) относительно актуального baseline.
- Основной KPI-ориентир: сближение с `wg-go` по single-thread lookup (`BenchmarkAllowedIPsLookupSingleFlowLikeL3Router`) и `plain` e2e профилю.
- Anti-regression: parallel/fairness (`ManyFlowsOneOwnerParallel`, transport parallel) не деградируют сверх согласованных порогов.
- ACL off остается минимально дорогим путем.
- ACL on сохраняет корректность и предсказуемый overhead.
- Memory-gates: `allocs/op` и `B/op` не растут относительно последнего green baseline.

## Обязательные инженерные принципы

- Держать дисциплину `wg-go`: узкий hot path, read-mostly структуры, precompute на control path.
- Не нарушать packetization-контракт sing-box (`WritePacketBuffer`, headroom/rear-headroom, `PacketWriter`).
- Крупные изменения dataplane сопровождать измеримым baseline и откатом по git; отдельные «dual path» в коде не держать.
- Избегать лишних runtime зависимостей, если задача решается статической peer-моделью.

## Проверка по коду (важно)

`clashapi` умеет runtime upsert/remove маршрутов:

- `POST /proxies/{name}/routes` -> `UpsertRoute`,
- `DELETE /proxies/{name}/routes/{id}` -> `RemoveRoute`.

Это не отменяет static-first модель и не должно утяжелять dataplane.

## Сценарий тестирования (обязательный)

- Локальные синтетические бенчмарки (`x1`) для быстрого сравнения итераций.
- При существенных изменениях — повторяемые прогоны `x10` для стабилизации baseline/дельты.
- E2E на VPS: передача `>=100MB` с проверкой `sha256`.

### Синтаксис локальных performance-запусков

- `x1` для `l3router`:
`cd experiments/router/hiddify-sing-box && GOWORK=off go test -count=1 ./common/l3router -run "^$" -bench "^BenchmarkMemEngineHandleIngress$|^BenchmarkMemEngineHandleIngressACLEnabled$|^BenchmarkMemEngineHandleIngressWGAllowedIPs$|^BenchmarkMemEngineHandleIngressNoLoopDrop$|^BenchmarkMemEngineHandleIngressManyFlowsOneOwnerParallel$|^BenchmarkL3RouterEndToEndSyntheticTransport$|^BenchmarkL3RouterEndToEndSyntheticTransportParallel$|^BenchmarkL3RouterEndToEndSyntheticTransportManyFlowsOneOwnerParallel$|^BenchmarkSyntheticTransportOnly$" -benchmem`  
  (внутри e2e и `BenchmarkSyntheticTransportOnly` — под-бенчи `plain_l3router_baseline`, `vless_reality_vision_synthetic`, `hy2_synthetic`, `tuic_synthetic`, `mieru_synthetic`; сравнивать с wg-go по тем же именам.)

- `x1` для `wg-go`:
`cd experiments/router/hiddify-sing-box/replace/wireguard-go && GOWORK=off go test -count=1 ./device -run "^$" -bench "^BenchmarkWireGuardAllowedIPsEndToEndSyntheticTransport$|^BenchmarkWireGuardAllowedIPsEndToEndSyntheticTransportParallel$|^BenchmarkAllowedIPsLookupSingleFlowLikeL3Router$|^BenchmarkAllowedIPsLookupManyFlowsOneOwnerParallelLikeL3Router$" -benchmem`

## Короткий operational-runbook

- Сборка Linux-бинаря:
`cd experiments/router/hiddify-sing-box && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -tags with_gvisor,with_clash_api,with_utls,with_l3router -o sing-box-linux-amd64 ./cmd/sing-box`
- Интеграционный стенд (build / deploy / docker / SMB 100 MiB): `experiments/router/stand/l3router/` — `python run.py` (см. README там).
- Деплой конфига на VPS (как раньше): `experiments/router/stand/l3router/scripts/deploy_l3router_server_static.sh`
- Синтетические бенчи (PowerShell): `experiments/router/bench/collect_phase0_baseline.ps1`

## Документы истины

- Точка входа в документацию: `experiments/router/docs/README.md`.
- Оперативные задачи и трекинг: `experiments/router/AGENTS.md`.

## Текущий фокус работ

- CPU-first оптимизация dataplane против `wg-go`: single-thread `plain` и lookup hot path.
- Поиск и снятие тяжелых блокировщиков в `PeerID` path (retry/branching/map-lock overhead).
- Инкапсуляция session/transport bind только в `protocol/l3router` (эндпоинт sing-box), без user/session identity в `common/l3router`.
- Сужение hot path до WG-подобного уровня (lookup/branching/alloc pressure) без ослабления no-loop/ACL/LPM semantics.
- Сохранение стабильности и совместимости с packet path sing-box.
- Систематическая проверка fairness/parallel после каждой оптимизации.

## Требования к отчетности

Результаты публиковать только в формате:

- `Single-thread (x1)` с блоком `было -> стало`,
- `Parallel (x1)` с блоком `было -> стало`,
- `Engine-синтетика` с блоком `было -> стало`.
- В каждой строке обязательно указывать `l3router vs wg-go` (relative delta по `ns/op`, и при наличии `allocs/op`, `B/op`).
- Relative delta считать строго как: `((l3router_ns/op - wg_go_ns/op) / wg_go_ns/op) * 100%`.
- Для зачета single-thread улучшения на `x1` требуется минимум `>= 3%` (меньшие изменения считать шумом и не засчитывать).
- Если single-thread улучшение `>= 8%` по anchor-метрике, прогон `x10` обязателен; improvement считается подтвержденным при медиане `x10 >= 5%`.
- Семантический gate блокирующий: любой регресс по `ACL/no-loop/LPM/tie-break/drop reason` делает итерацию незачетной вне зависимости от CPU-дельты.

Другие форматы отчетов не использовать.
