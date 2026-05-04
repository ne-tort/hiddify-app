# AGENTS — MASQUE / WARP_MASQUE (реальный production-проект)

## 1) Что это за проект и почему нельзя упрощать

Этот репозиторий — **реальный продукт**, а не учебный стенд.

- Реализация `masque` и `warp_masque` должна быть production-grade.
- Цель не "сделать green любой ценой", а обеспечить корректный dataplane для подключения к **реальным MASQUE серверам**.
- Любые костыли "лишь бы тест прошел" запрещены: поведение должно соответствовать RFC, go-практикам и архитектуре sing-box.

## 2) Текущая задача

Главный приоритет: закрыть post-migration разрывы TUN-only `connect_ip` без деградации:

- `connect_udp`;
- `connect_stream`;
- `warp_masque` (consumer + zero-trust).

Актуальный статус:

- `udp` — PASS;
- `tcp_stream` — PASS;
- `tcp_ip` smoke (10KB) — PASS, hash OK.
- strict bulk (`10/20/50MB` с бюджетом `N MB => N sec`) — PASS, hash/loss/budget OK.

## 3) Обязательные инварианты

- Не ломать legacy `warp`.
- Не делать скрытых миграций legacy путей.
- Не отключать проверки/таймауты для "зеленого" отчета, не увеличивать таймауты.
- Сохранять fail-fast там, где контракт нарушен.
- Держать прозрачные логи ошибок по классам (policy/capability/transport/dial/lifecycle).

## 4) Где читать контекст перед правками

Обязательно прочитать:

- `AGENTS.md` (этот файл).
- `IDEAL-MASQUE-ARCHITECTURE.md` — идеальная слойная архитектура и контракты MASQUE (норматив для правок).
- `MASQUE-ARCHITECTURE-GAP-CHECKLIST.md` — реестр расхождений с IDEAL и чекбокс-план исправления (обязательный трекер перед реализацией).
- `hiddify-core/docs/masque-warp-architecture.md`
- `hiddify-core/docs/masque-connect-ip-staged-closure.md`
- `hiddify-core/docs/masque-perf-gates.md`

Ключевые файлы реализации:

- `hiddify-core/hiddify-sing-box/protocol/masque/endpoint.go`
- `hiddify-core/hiddify-sing-box/protocol/masque/endpoint_server.go`
- `hiddify-core/hiddify-sing-box/transport/masque/transport.go` (в т.ч. `CoreClientFactory` / `DirectClientFactory`)
- `hiddify-core/hiddify-sing-box/transport/masque/netstack_adapter.go`

Ключевые тесты:

- `hiddify-core/hiddify-sing-box/protocol/masque/endpoint_test.go`
- `hiddify-core/hiddify-sing-box/transport/masque/netstack_adapter_test.go`
- `hiddify-core/hiddify-sing-box/transport/masque/transport_test.go` (в т.ч. тесты фабрики сессии)
- `hiddify-core/hiddify-sing-box/common/masque/runtime_test.go`

Стенд:

- `experiments/router/stand/l3router/docker-compose.masque-e2e.yml`
- `experiments/router/stand/l3router/configs/masque-server.json`
- `experiments/router/stand/l3router/configs/masque-client.json`
- `experiments/router/stand/l3router/configs/masque-client-connect-ip.json`
- `experiments/router/stand/l3router/masque_stand_runner.py`

## 5) Релевантное тестирование (только через Python entrypoint)

Единая точка входа:

- `python experiments/router/stand/l3router/masque_stand_runner.py --scenario udp`
- `python experiments/router/stand/l3router/masque_stand_runner.py --scenario tcp_stream`
- `python experiments/router/stand/l3router/masque_stand_runner.py --scenario tcp_ip`
- `python experiments/router/stand/l3router/masque_stand_runner.py --scenario all`

Правила:

- По умолчанию 10KB smoke.
- `--stress` (500MB) не использовать, пока не закрыт non-stress green.
- Любой фикс в dataplane подтверждать повторным `--scenario all`.
- После code fix — unit/race по затронутым пакетам обязательны.

## 6) Задание для следующего ИИ (handoff)

Текущая цель: довести bulk-путь TUN-only CONNECT-IP до production-готовности и закрыть acceptance.

Кратко по последней итерации:

- В `masque_stand_runner.py` bulk sender для `tcp_ip` переведен на paced python UDP loop (вместо `head|pv|socat`) при сохранении strict budget.
- Добавлен JSON-блок observability в результат `tcp_ip` (`connect_ip_ptb_rx_total`, `connect_ip_packet_write_fail_total`, `connect_ip_packet_read_exit_total`, `connect_ip_session_reset_total`).
- На серверной границе CONNECT-IP (`connectIPNetPacketConn`) UDP путь нормализован до payload (без raw IP header leakage в route/sink).
- Unit/race и non-stress e2e (`tcp_ip 10/20/50MB`, `all`) — PASS.

Следующий фокус:

- Удержать strict bulk `10/20/50MB` в green при последующих правках packet-plane.
- Поддерживать non-legacy MASQUE контур (без `tcp_over_ip`/M2 planner хвостов), синхронизировать контракты и доки.
- Перед кодовыми правками пройти чекбокс-план из `MASQUE-ARCHITECTURE-GAP-CHECKLIST.md` и отмечать пункты по мере закрытия.
