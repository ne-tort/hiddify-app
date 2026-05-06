# AGENTS — MASQUE / WARP_MASQUE (master-prompt)

## 1) Mission

Цель: довести `masque` endpoint в `hiddify-core/hiddify-sing-box` до production-качества и сходимости с RFC/паттернами проекта (`connect_stream`, `connect_udp`, `connect_ip`) без регрессий legacy `warp`.

Ключевой принцип: приоритет у **реального поведения кода** и измеримых артефактов, а не у расширения текста runbook.

## 2) Non-negotiables

- Итерация = **код** и/или **новый измеримый прогон**; перефразирование документации без нового сигнала от тестов не считается прогрессом.
- Любой fail (`loss`, `budget_exceeded`, `hash drift`, `timeout`) разбирать по коду/контракту сценария, а не списывать на хост-ОС.
- Главный слой работ: `protocol/masque`, `transport/masque`, `common/masque`, `third_party/connect-ip-go`, inbound в `endpoint_server.go`.
- Не делать fake-green: не ослаблять пороги/таймауты/валидации ради формальной зелени.
- Решения принимать по артефактам (`runtime/*.json`, `go test`, логи стенда).
- Режим `masque_or_direct` допустим только с `fallback_policy=direct_explicit`.

## 3) READ FIRST (mandatory)

Перед первой правкой в итерации обязательно просмотреть:

1. [hiddify-core/.github/workflows/ci.yml](hiddify-core/.github/workflows/ci.yml) (job `masque-gates`)
2. [IDEAL-MASQUE-ARCHITECTURE.md](IDEAL-MASQUE-ARCHITECTURE.md)
3. [docs/masque/AGENT-RFC-CI-CONTRACTS.md](docs/masque/AGENT-RFC-CI-CONTRACTS.md)
4. [docs/masque/AGENT-LAYER-SOURCE-OF-TRUTH.md](docs/masque/AGENT-LAYER-SOURCE-OF-TRUTH.md)
5. [docs/masque/AGENT-TEST-AND-STAND-RUNBOOK.md](docs/masque/AGENT-TEST-AND-STAND-RUNBOOK.md)
6. [docs/masque/AGENT-CI-REPLAY-CHEATSHEET.md](docs/masque/AGENT-CI-REPLAY-CHEATSHEET.md)

При расхождении источников приоритет: `код -> ci.yml -> IDEAL -> AGENTS/docs`.

## 4) Execution loop

1. Взять конкретный сигнал из артефактов/CI.
2. Локализовать слой и гипотезу.
3. Править только целевой слой ядра MASQUE.
4. Прогнать релевантные `go test` + стенд.
5. Сравнить артефакты до/после и зафиксировать вывод.

## 5) Definition of Done

- Нет регрессий mode/fallback/lifecycle контрактов.
- Релевантные unit/integration тесты PASS.
- Стенд PASS для нужных сценариев/объёмов по матрице.
- Если итерация про RFC/interop: есть изменение поведения или тестов в ядре, уменьшающее разрыв с RFC.

## 6) Current performance focus (real issue)

Текущий приоритет: потери на high-rate dataplane.

- `connect_ip` (`tcp_ip`): потери начинают проявляться примерно после ~90 Mbit/s.
- `connect_udp` (`udp`): текущая стабильная зона без потерь около ~70 Mbit/s; при ~80 Mbit/s начинается резкий срыв.
- `tcp_stream`: на 500 MiB достигалось ~602 Mbit/s без видимой integrity-деградации (вопрос учёта ретрансмитов остаётся отдельной проверкой методики).

Обязательные следующие шаги:

- Разобрать причины деградации `connect_ip`/`connect_udp` на high-rate (buffering/backpressure/runner boundary).
- Проверить корректность интерпретации throughput в fail-path (чтобы отделять реальную пропускную способность от длительного wait timeout).
- Держать явную методику speed-test:
  - `connect_ip`: `python experiments/router/stand/l3router/masque_stand_runner.py --scenario tcp_ip --megabytes <N>` + при поиске порога менять `MASQUE_TCP_IP_RATE_LIMIT`.
  - `connect_udp`: `python experiments/router/stand/l3router/masque_stand_runner.py --scenario udp --megabytes <N> --udp-send-bps <BPS>`.
  - `tcp_stream`: `python experiments/router/stand/l3router/masque_stand_runner.py --scenario tcp_stream --megabytes <N>`.
- Фиксировать **последний успешный baseline-прогон** (сценарий/объём/скорость/дата) и не перезаписывать baseline без причины; свежий полный срез делать при изменениях в соответствующем слое или при проверке регресса.

## 7) Handoff (short)

Использовать шаблон: [docs/masque/AGENT-HANDOFF-TEMPLATE.md](docs/masque/AGENT-HANDOFF-TEMPLATE.md)

Ограничение: краткий срез текущего цикла, без энциклопедии и без дублирования вынесенных runbook-разделов.
