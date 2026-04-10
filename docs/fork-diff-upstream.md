# Сравнение сабмодуля `hiddify-core` с upstream

- **Upstream remote:** `https://github.com/hiddify/hiddify-core`, ветка **`v3`**.
- **Merge-base** с `upstream/v3` существует: локальная история **не** оторвана от upstream (не полная перезапись репозитория).
- **Коммитов поверх `upstream/v3`:** 103 (на момент генерации отчёта).
- **Дифф `upstream/v3...HEAD` (кратко):** 97 files changed, 9109 insertions(+), 3077 deletions(-).

Крупные изменения затрагивают в том числе `hiddify-sing-box`, `ray2sing`, gRPC/proto в `v2/hcore/`. Для обновления отчёта:

```bash
cd hiddify-core
git fetch upstream v3
git merge-base HEAD upstream/v3
git rev-list --count upstream/v3..HEAD
git diff --shortstat upstream/v3...HEAD
```
