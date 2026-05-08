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
- **`hiddify-core` HEAD:** `e675dbf996ac68df18c943cd6d27561227791988`
- **Стенд (Docker + `masque_stand_runner`, `degrade_matrix`, последний зафиксированный артефакт до этой итерации):**
  - **10 MiB, лестница `100–150 mbit/s`:** CONNECT-IP — PASS до **130** mbit/s (`loss_pct=0`), первый FAIL на **140** mbit/s (~`0.056%` loss, `sink_udp_ingress_datagram_gap_no_udp_errors`), на **150** mbit/s — ~`1.04%` + `budget_exceeded`. Файл: `experiments/router/stand/l3router/runtime/connect_ip_udp_degrade_matrix.json`.
  - **150 MiB @ 120 mbit/s:** ~`0.064%` loss; **@ 100 mbit/s** — без потерь.
- **Сводка рисков:** `docs/masque/AGENT-MASQUE-DEGRADATION-GAPS.md`
- **Гипотеза (unchanged):** узкое место на стыке **QUIC/HTTP3 DATAGRAM** (ingress/TX) и **sink vs runner budget**; сверка со счётчиками `http3_*`, `quic_datagram_*`, MTU.
- **Код (эта итерация):** `replace/quic-go-patched/http3/state_tracking_stream.go` — очередь входящих DATAGRAM переведена с **слайса со сдвигом** на **фиксированное кольцо** (O(1) enqueue/dequeue, без копирования хвоста слайса при каждом `ReceiveDatagram`). Цель: меньше CPU/GC и стабильнее drain на bulk CONNECT-IP/CONNECT-UDP.
- **Локальная верификация:** PASS полный `go test ./...` в модуле `replace/quic-go-patched`; PASS `./transport/masque/...`, `./protocol/masque/...` из `hiddify-sing-box`.
- **Интерпретация скорости:** зона **120–140 mbit/s** на стенде может флаппать; индикатор прогресса — устойчивость **160 mbit/s+** при нулевых drops по ключевым счётчикам.
- **Стенд после backlog unknown-stream + ACK arbitration (исторический quick check, до ring-buffer):** `tcp_ip` / `udp` 10 MiB @ `--udp-send-bps 25000000`, `MASQUE_TCP_IP_RATE_LIMIT=160mbit` — PASS без потерь и без delta drops по HTTP3/packer (см. предыдущие записи).

## 8) Next Iteration Tasks (single-thread)

1. **WSL + Docker:** прогнать `degrade_matrix` и зафиксировать `max_pass/first_fail` и дельты счётчиков (`quic_datagram_packer_oversize_drop_total`, `http3_stream_datagram_queue_drop_total`, `http3_datagram_unknown_stream_drop_total`, `connect_ip_packet_write_fail_reason_total["mtu"]`) **после** ring-buffer; при каждом fail — `stop_reason`, loss/hash, runtime JSON.
2. **CONNECT-IP:** лестница `tcp_ip_threshold` с явным pace (без `udp_send_bps_source=none` на Windows) и сравнение границы до/после.
3. При пустом чеклисте в gaps — одна новая запись в `docs/masque/AGENT-MASQUE-CORE-ISSUES.md` и правка ядра в той же итерации (как в протоколе §5.6).

## 9) Where Heavy Details Live

Чтобы не раздувать `AGENTS.md`, подробности держать в профильных файлах:

- Архитектура и wire semantics: `IDEAL-MASQUE-ARCHITECTURE.md`
- Layer/boundary triage и observability-карты: `docs/masque/AGENT-LAYER-SOURCE-OF-TRUTH.md`
- Команды и порядок локального replay: `docs/masque/AGENT-TEST-AND-STAND-RUNBOOK.md`, `docs/masque/AGENT-CI-REPLAY-CHEATSHEET.md`
- RFC/CI контракты: `docs/masque/AGENT-RFC-CI-CONTRACTS.md`
- История прогонов и числовые факты: `experiments/router/stand/l3router/runtime/*.json`
- Риски деградации MASQUE / CONNECT-IP (чеклист): `docs/masque/AGENT-MASQUE-DEGRADATION-GAPS.md`
- Отдельный backlog новых проблем ядра (если текущий чеклист пуст): `docs/masque/AGENT-MASQUE-CORE-ISSUES.md`
