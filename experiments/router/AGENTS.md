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
- Текущий hot path: `MemEngine.HandleIngress` с `RWMutex` и линейным LPM-сканом по `routes`; egress — `chan` + single worker на session, с drop-oldest при overflow.

## Текущее задание

Миграция `l3router` к production-ready static-first контуру (без изменения целевой модели):

- Phase 0 (baseline): зафиксировать измерения текущего dataplane (pps/latency/drop/queueOverflow/writeTimeout) на synthetic + static-no-control-plane.
- Phase 1 (fairness): добавить тест/бенч профили для **many-flows-per-owner** и проверить отсутствие нежелательной сериализации/голодания.
- Phase 2 (engine hot path): при подтверждении узкого места подготовить безопасную оптимизацию lookup (индексация FIB/snapshot, снижение lock contention) без изменения semantics.
- Phase 3 (endpoint queues): при необходимости уточнить дисциплину очередей egress (burst handling, drop policy, наблюдаемость per-owner/per-session).
- Phase 4 (rollout): повторить VPS smoke + `>=100MB` hash + многопоточный сценарий, обновить runbook/архитектурные заметки.

## Следующий шаг

Добавить fairness baseline для сценария many-flows-per-owner (отдельный тест/бенч + метрики), затем решить по фактам: нужны ли правки в `MemEngine` lookup или в egress queue policy.

## Рабочие ссылки

- Код: `experiments/router/hiddify-sing-box`
- Архитектура: `experiments/router/docs/router-architecture.md`
- Стенд static-only (чеклист, Docker из форка, скрипты тестов): `tmp/universal-singlehop-31.56.211.60/static-no-control-plane/README.md`
