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
- **`hiddify-core` HEAD:** `c51e62bf593ba8a62b20a98df738474ae4f730f0` — **`MasqueHTTPServerQUICConfig()`**: серверный `http3.Server.QUICConfig` приведён к тем же базовым параметрам packet‑plane клиента (**`EnableDatagrams` + `InitialPacketSize`=1350 + idle/keepalive** через существующий `masquePacketPlaneQUICConfig`). Раньше на сервере задавались только часть полей, без `InitialPacketSize`. Дополнительно: **`ReadFrom`/`ReadPacket`** CONNECT‑IP (клиент `connectIPUDPPacketConn`, сервер `connectIPNetPacketConn`) проверяют **read deadline** внутри циклов parse‑drop, чтобы не блокироваться без `DeadlineExceeded`.
- **Стенд (Docker Desktop Win):** Пересборка `artifacts/sing-box-linux-amd64` + полный **`degrade_matrix`** с `MASQUE_DEGRADE_TCP_IP_RATES=100m,120m,130m,140m,150m` и согласованными UDP BPS выполнены **два прогона** на разных сборках текущего цикла. Артефакт: `experiments/router/stand/l3router/runtime/connect_ip_udp_degrade_matrix.json`. **Наблюдение:** `last_pass` / `first_fail` по CONNECT‑IP **не стабильны** между прогонами при нулевых `delta_*` (пример первого прогона: последний чистый PASS **140m** при FAIL **120m/130m**; после правки серверного QUIC: **120m** последний PASS при **130m** first_fail и малых потерях на **130–140m**). CONNECT‑UDP ветка матрицы на этих прогонах — **все точки PASS** (хеш без дрейфа). Для воспроизводимости границ AGENTS‑политика остаётся: **каноничный замер из WSL2 + Docker**.
- **Сводка рисков:** `docs/masque/AGENT-MASQUE-DEGRADATION-GAPS.md`
- **Код (эта итерация):** `hiddify-sing-box/transport/masque/transport.go` (`MasqueHTTPServerQUICConfig`, дедлайн в `ReadFrom`), `protocol/masque/endpoint_server.go` (то же для серверного CONNECT‑IP).
- **Локальная верификация:** `go test -count=1 ./transport/masque/... ./protocol/masque/...` — PASS (после сброса `GOOS`/`GOARCH` с кросс‑сборки).

## 8) Next Iteration Tasks (single-thread)

1. Повторить **`degrade_matrix`** (та же лестница или «якорь» 130m/140m) в **WSL2** и/или усреднить **≥3 прогона** на одном артефакте, затем скорректировать baseline §1 раздела `AGENT-MASQUE-DEGRADATION-GAPS.md` только если картина счётчиков + peer‑split стабильна при нулевых `delta_quic_*` / `delta_http3_*` — иначе triage **`sink_writer_boundary`** (цепочка router → UDP sink), не затушёвывая малую потерю как PASS.

## 9) Where Heavy Details Live

Чтобы не раздувать `AGENTS.md`, подробности держать в профильных файлах:

- Архитектура и wire semantics: `IDEAL-MASQUE-ARCHITECTURE.md`
- Layer/boundary triage и observability-карты: `docs/masque/AGENT-LAYER-SOURCE-OF-TRUTH.md`
- Команды и порядок локального replay: `docs/masque/AGENT-TEST-AND-STAND-RUNBOOK.md`, `docs/masque/AGENT-CI-REPLAY-CHEATSHEET.md`
- RFC/CI контракты: `docs/masque/AGENT-RFC-CI-CONTRACTS.md`
- История прогонов и числовые факты: `experiments/router/stand/l3router/runtime/*.json`
- Риски деградации MASQUE / CONNECT-IP (чеклист): `docs/masque/AGENT-MASQUE-DEGRADATION-GAPS.md`
- Отдельный backlog новых проблем ядра (если текущий чеклист пуст): `docs/masque/AGENT-MASQUE-CORE-ISSUES.md`
