# AGENT CI Replay Cheatsheet

Source-of-truth: `hiddify-core/.github/workflows/ci.yml`, job `masque-gates`.

## 1) Canonical order (high level)

1. pre-docker go gates (unit/race/fast regression/integration/lifecycle)
2. build linux sing-box artifact
3. preflight + stand smoke
4. strict bulk/scoped/anti-bypass
5. runtime contract validator + typed asserts

## 2) Rule

- Не переставлять шаги при локальном реплее без осознанной миграции контракта.
- Команды всегда сверять с актуальным `ci.yml`, а не по памяти.

## 3) Artifacts

- Главный summary: `runtime/masque_python_runner_summary.json`
- Контракт: `runtime/masque_runtime_contract_latest.json`
