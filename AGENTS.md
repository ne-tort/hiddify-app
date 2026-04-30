# AGENTS — MASQUE / WARP_MASQUE (реальный production-проект)

## 1) Что это за проект и почему нельзя упрощать

Этот репозиторий — **реальный продукт**, а не учебный стенд.

- Реализация `masque` и `warp_masque` должна быть production-grade.
- Цель не "сделать green любой ценой", а обеспечить корректный dataplane для подключения к **реальным MASQUE серверам**.
- Любые костыли "лишь бы тест прошел" запрещены: поведение должно соответствовать RFC, go-практикам и архитектуре sing-box.

## 2) Текущая задача

Главный приоритет: довести до успеха `tcp_transport=connect_ip` (полноценный TCP-over-CONNECT-IP dataplane), без деградации:

- `connect_udp`;
- `connect_stream`;
- `warp_masque` (consumer + zero-trust).

Контекст:

- Базовый e2e entrypoint переведен на Python (`experiments/router/stand/l3router/masque_stand_runner.py`).
- Для non-stress:
  - `udp` — PASS;
  - `tcp_stream` — PASS;
  - `tcp_ip` — FAIL (корневой dataplane-блокер в core/endpoint, не в runner).

## 3) Обязательные инварианты

- Не ломать legacy `warp`.
- Не делать скрытых миграций legacy путей.
- Не отключать проверки/таймауты для "зеленого" отчета.
- Сохранять fail-fast там, где контракт нарушен.
- Держать прозрачные логи ошибок по классам (policy/capability/transport/dial/lifecycle).

## 4) Где читать контекст перед правками

Обязательно прочитать:

- `AGENTS.md` (этот файл).
- `hiddify-core/docs/masque-warp-architecture.md`
- `hiddify-core/docs/masque-connect-ip-staged-closure.md`
- `hiddify-core/docs/masque-perf-gates.md`

Ключевые файлы реализации:

- `hiddify-core/hiddify-sing-box/protocol/masque/endpoint.go`
- `hiddify-core/hiddify-sing-box/protocol/masque/endpoint_server.go`
- `hiddify-core/hiddify-sing-box/transport/masque/transport.go`
- `hiddify-core/hiddify-sing-box/transport/masque/tcp_over_ip.go`
- `hiddify-core/hiddify-sing-box/transport/masque/netstack_adapter.go`
- `hiddify-core/hiddify-sing-box/transport/masque/m2_factory.go`
- `hiddify-core/hiddify-sing-box/transport/masque/tcp_policy_planner.go`

Ключевые тесты:

- `hiddify-core/hiddify-sing-box/protocol/masque/endpoint_test.go`
- `hiddify-core/hiddify-sing-box/transport/masque/netstack_adapter_test.go`
- `hiddify-core/hiddify-sing-box/transport/masque/m2_factory_test.go`
- `hiddify-core/hiddify-sing-box/transport/masque/transport_test.go`
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

Цель: закрыть `tcp_ip` до PASS в реальном dataplane сценарии.

Требования к подходу:

- Работать по RFC-паттернам CONNECT-IP и практикам sing-box/quic-go/connect-ip-go.
- Использовать корректные go-паттерны конкурентности: bounded contexts, cancellation, ownership goroutines, deterministic lifecycle.
- Не подменять реальный dataplane direct-диалами.
- Поддерживать чистую классификацию ошибок и контрактов capability/policy.

Обязательная тактика работы:

- Использовать субагентов:
  - для сбора контекста по коду;
  - для поиска внешних паттернов/референсов (GitHub, RFC);
  - для независимой проверки гипотез и тестовых разрывов.
- Делать короткие итерации: гипотеза → правка → `--scenario tcp_ip` → анализ логов → повтор.
- После первого PASS по `tcp_ip` зафиксировать устойчивость: минимум 3 повторных прогона.

Критерий готовности:

- `udp`, `tcp_stream`, `tcp_ip` одновременно PASS через единый Python runner без ручных шагов.
- Никаких упрощений, которые делают решение нерелевантным для реальных MASQUE серверов.
