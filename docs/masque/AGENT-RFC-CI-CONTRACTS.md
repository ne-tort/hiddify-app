# AGENT RFC + CI Contracts

Этот документ хранит детальные RFC/CI-контракты для MASQUE.  
`AGENTS.md` содержит только master-правила и ссылки.

## 1) RFC intent vs implementation

- RFC 9297/9298/9484 задают целевое поведение.
- Текущее состояние кода может иметь пробелы; закрытие пробелов делается правками в ядре и тестами.
- RFC-пересказ без кода не считается завершением работы.

## 2) Ключевые RFC принципы

- Unknown capsule/context-id: fail-safe поведение без silent corruption.
- DATAGRAM только после согласованных HTTP/3 settings.
- Parse/validation ошибки: детерминированная классификация (не string-match).
- CONNECT-IP route advertisement и scoped-пути: fail-fast/typed contracts.

## 3) CI contract source-of-truth

- Канон порядка и шагов: `hiddify-core/.github/workflows/ci.yml` (job `masque-gates`).
- Контрактные артефакты: `experiments/router/stand/l3router/runtime/*.json`.
- Агрегатор: `masque_runtime_contract_validator.py`.
- Typed asserts: `masque_runtime_ci_gate_asserts.py`.

## 4) Обязательные checks (high level)

- schema/assert контракта runtime-артефактов.
- anti-bypass parity rows.
- scoped/lifecycle parity rows.
- согласованность error_class/error_source между runtime и summary.

## 5) Правило работы

При любом RFC/CI-расхождении:

1. Воспроизвести в тестах/стенде.
2. Локализовать boundary.
3. Исправить код.
4. Подтвердить артефактами.
