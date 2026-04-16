# AGENTS — операционный контекст `experiments/router`

## Назначение файла

Операционный файл для текущей итерации `l3router`.  
Держать только актуальное состояние, активный план и следующий шаг (без истории и длинных отчетов).

Глобальные инварианты и KPI: `AGENTS.md` в корне репозитория.

Документация (архитектура, слои, бенчмарки): [`docs/README.md`](docs/README.md).

Интеграционный стенд (VPS + Docker + SMB): [`stand/l3router/README.md`](stand/l3router/README.md).

## Текущее состояние

- Рабочий контур: server hub-and-spoke, static-first (`peers` в JSON), совместимость с vanilla sing-box client (`tun+address`) обязательна.
- Packetization-контракт sing-box остается неизменным (`WritePacketBuffer` canonical path в endpoint).
- Production dataplane в overlay peer-first: `protocol/l3router` вызывает `HandleIngressPeer(packet, ingressPeerID)`, а egress-доставка делает `EgressPeerID -> SessionKey -> queue/worker`.
- `common/l3router` — ядро маршрутизации (peer-only); session-привязки и I/O — в `protocol/l3router` (эндпоинт).
- Build-tag `with_l3router` добавлен и включен в default tags.
- Текущий CPU-gap к `wg-go` остается в single-thread `plain`/lookup path; parallel/fairness path в целом стабильны.

## Активный план

1. Снять baseline `l3router x1` и `wg-go x1`, ранжировать hotspot’ы по влиянию на `single-thread plain`.
2. Применить одну целевую оптимизацию top hotspot на итерацию (без смешивания 2+ независимых оптимизаций).
3. Перепроверить semantics (`ACL/no-loop/LPM/tie-break/drop reasons`) и regression/race.
4. Снять повторный `x1`, зафиксировать `было -> стало` и `l3router vs wg-go`.
5. При улучшении single-thread не допустить регрессий parallel/fairness сверх gate-порогов.

## Benchmark matrix (обязательный)

- `l3router x1`:  
`cd experiments/router/hiddify-sing-box && GOWORK=off go test -count=1 ./common/l3router -run "^$" -bench "^BenchmarkMemEngineHandleIngress$|^BenchmarkMemEngineHandleIngressACLEnabled$|^BenchmarkMemEngineHandleIngressWGAllowedIPs$|^BenchmarkMemEngineHandleIngressNoLoopDrop$|^BenchmarkMemEngineHandleIngressManyFlowsOneOwnerParallel$|^BenchmarkL3RouterEndToEndSyntheticTransport$|^BenchmarkL3RouterEndToEndSyntheticTransportParallel$|^BenchmarkL3RouterEndToEndSyntheticTransportManyFlowsOneOwnerParallel$|^BenchmarkSyntheticTransportOnly$" -benchmem`
- `wg-go x1`:  
`cd experiments/router/hiddify-sing-box/replace/wireguard-go && GOWORK=off go test -count=1 ./device -run "^$" -bench "^BenchmarkWireGuardAllowedIPsEndToEndSyntheticTransport$|^BenchmarkWireGuardAllowedIPsEndToEndSyntheticTransportParallel$|^BenchmarkAllowedIPsLookupSingleFlowLikeL3Router$|^BenchmarkAllowedIPsLookupManyFlowsOneOwnerParallelLikeL3Router$" -benchmem`
- regression:  
`cd experiments/router/hiddify-sing-box && GOWORK=off go test ./common/l3router ./protocol/l3router && GOWORK=off go test -race ./protocol/l3router`

## KPI / gates (итерация CPU)

- `Single-thread plain`: дельта к `wg-go plain` должна уменьшаться относительно предыдущего baseline.
- `Lookup gate`: дельта `BenchmarkMemEngineHandleIngress` к `BenchmarkAllowedIPsLookupSingleFlowLikeL3Router` должна уменьшаться.
- `Parallel/fairness`: `ManyFlowsOneOwnerParallel` не деградирует сверх 10% от baseline; `drop/op` и `error/op` остаются без роста.
- `Memory gate`: `allocs/op` и `B/op` не растут на `plain`/engine-synthetic профилях.
- Для отчета «зачет итерации» нужен минимум `x1`; для стабилизации ключевого улучшения — подтверждение `x10`.
- `x1 pass threshold`: single-thread anchor улучшен минимум на `>=3%`; меньше — считать шумом.
- `x10 mandatory`: при `x1 >=8%` по anchor-метрике обязателен `x10`, зачет только при медиане `>=5%`.
- `Semantics blocker`: любой регресс по `ACL/no-loop/LPM/tie-break/drop reason` автоматически делает итерацию незачетной.

## Iteration acceptance (go / no-go)

- Anchor-1 (lookup): `BenchmarkMemEngineHandleIngress` vs `BenchmarkAllowedIPsLookupSingleFlowLikeL3Router`.
- Anchor-2 (plain): `BenchmarkL3RouterEndToEndSyntheticTransport/plain_l3router_baseline` vs `wg-go plain`.
- Relative formula: `((l3router_ns/op - wg_go_ns/op) / wg_go_ns/op) * 100%`.
- `Go`: single-thread delta по минимум одному anchor улучшена выше pass threshold, memory/parallel gates green, semantics green.
- `No-go`: single-thread improvement ниже threshold, либо любой blocker-gate красный.

## Следующий шаг

Для полного e2e: `cd experiments/router/stand/l3router && python run.py all` (см. README стенда). Для CPU-итераций — матрица бенчмарков в этом файле.