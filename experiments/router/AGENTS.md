# AGENTS — операционный контекст `experiments/router`

## Назначение файла

Этот файл — **операционный** для текущих задач в `experiments/router`.
Корневой `AGENTS.md` — отправная точка и глобальные правила.

ИИ обязан держать этот файл в **лаконичном актуальном** состоянии:

- обновлять после каждого значимого шага,
- хранить только текущее состояние, текущий план и следующий шаг,
- **не** накапливать историю изменений и длинные отчеты.

## Текущее состояние

- Единственный штатный контур: `l3router` hub-and-spoke, static-first (`routes` в JSON), стенд `static-no-control-plane/`.
- Legacy/fallback не поддерживаются. `clashapi` route API — только ops, не базовый dataplane.
- Static-only E2E подтвержден: smoke + `>=100MB` (`sha256_match=true`, `runtime_route_api_used=false`) в `static-no-control-plane/runtime/`.
- Phase 0 baseline зафиксирован и автоматизирован: `scripts/collect_phase0_baseline.ps1` (raw bench + JSON summary в `runtime/`).
- Покрытие по корректности высокое (owner churn / route churn / loop-avoid / race в `experimental/l3router` и `protocol/l3router`), но отдельного целевого сценария fairness для **многих параллельных потоков на одного owner** пока нет.
- Текущий hot path: `MemEngine.HandleIngress` (snapshot + fast IPv4 path), egress — session queue + batch writer.
- JSON policy-переключатели добавлены: `acl_enabled` (default off), `fragment_policy`, `overflow_policy`, `telemetry_level`.
- По результатам последних замеров (`1280+`): `l3router` и `wg-go` близки в transport-профилях, но `plain` single-thread у `l3router` все еще дороже.

## Текущее задание

Снизить CPU `l3router` в single-thread и удержать стабильность/корректность маршрутизации.

### Текущая опорная точка (для следующей итерации)

- `x1`, `1280+`, single-thread:
  - `l3router plain`: ~`51 ns/op`
  - `wg-go plain`: ~`20 ns/op`
- `x1`, `1280+`, parallel:
  - `l3router plain_parallel`: лучше `wg-go`
  - transport-профили (`vless/hy2/tuic/mieru`): близко, возможны флуктуации между прогонами.

## Чеклист на многоитерационную задачу CPU-оптимизации

- Снять `x1` baseline для `l3router` и `wg-go` одинаковыми бенчами.
- Внести точечную оптимизацию одного узкого места (не смешивать 3+ изменения за шаг).
- Прогнать `go test ./experimental/l3router ./protocol/l3router`.
- Прогнать `go test -race ./protocol/l3router`.
- Снова снять `x1` для `l3router` и `wg-go`, зафиксировать дельту.
- Проверить, что семантика не нарушена (ACL/no-loop/LPM/tie-break/drop reasons).
- Только после локальной стабилизации обновлять стендовые сценарии.

## Следующий шаг

Найти и убрать следующую CPU-дорогую стадию в fast-path (`engine` или `endpoint`) с обязательной валидацией `x1` и сравнением против `wg-go`.

## Рабочие ссылки

- Код: `experiments/router/hiddify-sing-box`
- Архитектура: `experiments/router/docs/router-architecture.md`
- Стенд static-only (чеклист, Docker из форка, скрипты тестов): `tmp/universal-singlehop-31.56.211.60/static-no-control-plane/README.md`

## Команды тестирования (эталон синтаксиса)

- `l3router x1`:
`cd experiments/router/hiddify-sing-box && GOWORK=off go test -count=1 ./experimental/l3router -run "^$" -bench "^BenchmarkMemEngineHandleIngress$|^BenchmarkMemEngineHandleIngressManyFlowsOneOwnerParallel$|^BenchmarkL3RouterEndToEndSyntheticTransport$|^BenchmarkL3RouterEndToEndSyntheticTransportParallel$" -benchmem`
- `wg-go x1`:
`cd experiments/router/hiddify-sing-box/replace/wireguard-go && GOWORK=off go test -count=1 ./device -run "^$" -bench "^BenchmarkWireGuardAllowedIPsEndToEndSyntheticTransport$|^BenchmarkWireGuardAllowedIPsEndToEndSyntheticTransportParallel$" -benchmem`
- `x10` использовать только для предварительной валидации результата; рабочий цикл — `x1`.