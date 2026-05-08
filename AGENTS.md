# AGENTS — MASQUE / WARP_MASQUE

## 1) Mission

Довести `masque` endpoint в `hiddify-core/hiddify-sing-box` до production-качества и RFC-сходимости (`connect_stream`, `connect_udp`, `connect_ip`) по артефактам CI и Docker-стенда.

Текущий приоритет: объективно повышать скорость без потерь/hash drift и без регрессий контрактов. Искать и фиксить bottleneck в hot path только по подтверждённой наблюдаемости.

**Windows policy:** каноничный e2e-запуск стенда — из **WSL** с backend **Docker Desktop (WSL2)**.

## 2) Non-negotiables

- Цикл: сигнал -> код -> тест/стенд -> артефакт -> следующий шаг.
- Fail (`loss`, `timeout`, `budget_exceeded`, `throughput_target_unmet`, hash drift) не маскировать; triage по boundary слоя.
- Не ослаблять пороги/таймауты/валидации ради PASS.
- Для Docker e2e единственный валидный путь: compose-стенд + `masque_stand_runner.py`/`masque_runtime_*` в порядке `masque-gates`.
- `masque_or_direct` только с `fallback_policy=direct_explicit`.
- Оптимизации hot path только по метрикам/логам/контрактам, не "на веру".
- Правки ядра коммитить в `hiddify-core`; в `hiddify-app` фиксировать новый SHA сабмодуля.

## 3) Source Of Truth

При расхождениях приоритет:
1. Код
2. `hiddify-core/.github/workflows/ci.yml` (job `masque-gates`)
3. `IDEAL-MASQUE-ARCHITECTURE.md`
4. `docs/masque/*`
5. Этот файл

## 4) Read First (links)

Обязательный минимум перед итерацией:

1. `hiddify-core/.github/workflows/ci.yml` (`masque-gates`)
2. `IDEAL-MASQUE-ARCHITECTURE.md`
3. `docs/masque/AGENT-RFC-CI-CONTRACTS.md`
4. `docs/masque/AGENT-LAYER-SOURCE-OF-TRUTH.md`
5. `docs/masque/AGENT-TEST-AND-STAND-RUNBOOK.md`
6. `docs/masque/AGENT-CI-REPLAY-CHEATSHEET.md`
7. `hiddify-core/docs/masque-perf-gates.md`
8. `hiddify-core/docs/masque-connect-ip-staged-closure.md`
9. `docs/masque/AGENT-MASQUE-DEGRADATION-GAPS.md` (риски потерь, стенд-факты, пробелы observability)

## 5) Working Protocol

0. Перед стартом итерации обязательно опереться на **последний** записанный результат Docker-стенда (`experiments/router/stand/l3router/runtime/*.json`, в т.ч. `masque_python_runner_summary.json`) как отправную точку и на **незавершённые** пункты чеклиста (§8).
1. Взять сигнал из runtime/CI/`go test` и выбрать задачу из известных проблем ядра в `docs/masque/AGENT-MASQUE-DEGRADATION-GAPS.md` (приоритет: `[IN PROGRESS]` -> следующий критичный риск).
2. Сформулировать boundary + гипотезу (детальный triage в `AGENT-LAYER-SOURCE-OF-TRUTH.md`).
3. Править один целевой слой за итерацию (фокус: `hiddify-core/hiddify-sing-box`, не раннер, кроме observability-минимума).
4. Прогнать релевантные тесты + стенд в CI-порядке.
5. Обновить §7 и §8 этого файла и статус в `docs/masque/AGENT-MASQUE-DEGRADATION-GAPS.md` (`[DONE]`/`[IN PROGRESS]`/`[TODO]`) с коротким фактом по артефакту.
6. Если в известных проблемах и §8 нет активных задач: выполнить целевой анализ кода ядра на потери/таймауты, записать новые гипотезы в отдельный файл `docs/masque/AGENT-MASQUE-CORE-ISSUES.md`, добавить их в §8 и попытаться закрыть первую критичную в той же итерации.

## 6) Definition of Done

- Нет регрессий mode/fallback/lifecycle/scoped-контрактов.
- PASS у релевантных unit/race/integration.
- PASS у целевой стендовой матрицы.
- Изменения поведения отражены в коде + тестах.

## 7) Current Autonomous Cycle (overwrite each iteration)

- **Дата:** 2026-05-08
- **`hiddify-core` HEAD:** `f78b8248ede99608c009742e474ef4390c795468` — **connect-ip-go** (`third_party/connect-ip-go`): на hot path приёма/передачи IP‑датаграмм политика маршрутов/адресов читается из **неизменяемого снимка `connRouteView`** (`atomic.Pointer`), публикуемого под `sync.Mutex` только при `AssignAddresses`, `AdvertiseRoute` и приёме address‑assign capsule, вместо **`RWMutex.RLock` на каждый пакет**. Исправление **`ReadPacket`**: при буфере короче полезной нагрузки возвращается **ошибка**, а не молчаливая усечённая запись.
- **Стенд:** в этом цикле **не перезапускался**; предыдущее наблюдение по флапу `last_pass` CONNECT‑IP (`AGENTS`/runtime JSON) остаётся в силе до нового прогона на **фиксированном** `sing-box-linux-amd64` после `f78b8248ede99608c009742e474ef4390c795468`.
- **Сводка рисков:** `docs/masque/AGENT-MASQUE-DEGRADATION-GAPS.md`
- **Код (эта итерация):** `hiddify-sing-box/third_party/connect-ip-go/conn.go`
- **Локальная верификация:** `go test -count=1 ./transport/masque/... ./protocol/masque/...` — PASS. Пакет `connect-ip-go` локально: интеграционные тесты **падают по таймауту/flake на Windows** (`TestTTLs` и др. — до правок уже воспроизводилось).

## 8) Next Iteration Tasks (single-thread)

1. Собрать `sing-box-linux-amd64`, прогнать **`degrade_matrix`** (полная лестница или якорь 130m/140m) в **WSL2 + Docker** или **≥3** прогона на одном бинарнике; обновить `experiments/router/stand/l3router/runtime/connect_ip_udp_degrade_matrix.json` и baseline §1 `AGENT-MASQUE-DEGRADATION-GAPS.md` только при согласованных peer-split и счётчиках.

## 9) Where Heavy Details Live

Чтобы не раздувать `AGENTS.md`, подробности держать в профильных файлах:

- Архитектура и wire semantics: `IDEAL-MASQUE-ARCHITECTURE.md`
- Layer/boundary triage и observability-карты: `docs/masque/AGENT-LAYER-SOURCE-OF-TRUTH.md`
- Команды и порядок локального replay: `docs/masque/AGENT-TEST-AND-STAND-RUNBOOK.md`, `docs/masque/AGENT-CI-REPLAY-CHEATSHEET.md`
- RFC/CI контракты: `docs/masque/AGENT-RFC-CI-CONTRACTS.md`
- История прогонов и числовые факты: `experiments/router/stand/l3router/runtime/*.json`
- Риски деградации MASQUE / CONNECT-IP (чеклист): `docs/masque/AGENT-MASQUE-DEGRADATION-GAPS.md`
- Отдельный backlog новых проблем ядра (если текущий чеклист пуст): `docs/masque/AGENT-MASQUE-CORE-ISSUES.md`
