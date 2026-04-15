# Runtime artifacts policy

Эта директория хранит исторические артефакты прошлых прогонов.

- Артефакты из сценариев с runtime route upsert/delete считаются legacy и не являются эталоном для текущей миграции.
- Актуальный контур — `static-no-control-plane/` с конфигами и скриптами без обязательного route API.
- Новые валидационные артефакты (`smoke`, `>=100MB`, hash/time) нужно сохранять в `static-no-control-plane/runtime/`.
