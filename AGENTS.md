# Правила для ассистентов (репозиторий `hiddify-app`)

## Основная цель текущего этапа

Реализовать опцию `WG Cloak` в маршрутизации Hiddify:

- добавить переключатель в UI Routing и сохранить флаг в preferences;
- при включенном флаге пересобирать WG endpoint-ы в cloak-форму;
- скрывать WG endpoint-ы из `select`, чтобы на них нельзя было переключиться вручную;
- автоматически добавлять `detour: select` для WG и необходимые `tun/route` правки;
- сохранить совместимость с обычным поведением при выключенном флаге.

## Где находится реализация

- UI/настройки: `lib/features/settings/overview/sections/route_options_page.dart`.
- Preferences и сбор опций: `lib/features/settings/data/config_option_repository.dart`.
- Модель конфиг-опций: `lib/singbox/model/singbox_config_option.dart`.
- Генерация sing-box: `hiddify-core/v2/config/builder.go`, `hiddify-core/v2/config/outbound.go`.
- Структура hiddify options: `hiddify-core/v2/config/hiddify_option.go`.

## Архитектурные инварианты (обязательные)

- WG Cloak применяется только когда `wg-cloak=true`.
- Если WG endpoint есть, но валидных proxy outbound нет, Cloak игнорируется (fallback на обычное поведение).
- Для каждого WG endpoint при Cloak:
  - endpoint остается в `endpoints`,
  - исключается из списка `selector`,
  - получает `detour: select`,
  - использует клиентский режим (`system: false`).
- Конфиг WG должен собираться из исходных данных endpoint-а (ключи, peers, allowed CIDR, адреса).
- Изменения должны быть минимальными и не ломать WARP/обычные прокси/прочие endpoint-ы.

## Правила маршрутизации и TUN

- При включенном WG Cloak в `tun.address` должна быть служебная IPv4 сеть и WG IPv4 `/32`.
- Для WG CIDR должны добавляться route-правила вида:
  - `inbound: tun-in`,
  - `ip_cidr: <wg allowed CIDR>`,
  - `outbound: <wg endpoint tag>`.
- При нескольких WG endpoint-ах правила и tun-адреса должны формироваться для каждого.

## Runtime и совместимость

- Поведение без WG Cloak должно оставаться прежним.
- Существующие профили с WG endpoint должны корректно открываться даже если флаг выключен.
- Нельзя ломать `client-to-client`, `warp`, DNS-правила и базовые маршруты.

## Требования к UI

- В Routing должен быть видимый чекбокс `WG Cloak`.
- Значение должно сохраняться в preferences ключом `wg-cloak`.
- Флаг должен попадать в итоговые `SingboxConfigOption` и передаваться в core.

## Требования к проверкам

- Проверить, что при `wg-cloak=true` WG endpoint не появляется в selector.
- Проверить, что WG endpoint в итоговом JSON содержит `detour: select`.
- Проверить, что `tun.address` содержит служебный IPv4 + WG `/32`.
- Проверить, что есть правило `tun -> wg` для WG CIDR.
- Проверить fallback сценарии:
  - WG без proxy outbound,
  - несколько WG endpoint-ов.

## Политика изменений в ядре

- Правки в `hiddify-core/v2/config` должны быть точечными и обратимо-безопасными.
- Не добавлять необязательные фичи вне WG Cloak.
- Приоритет: корректность генерации конфига и отсутствие регрессий.

## Формат отчета по этапу

В итоговом отчете обязательно указывать:

- какие файлы UI/preferences/core были изменены;
- какие сценарии WG Cloak проверены и с каким результатом;
- какие команды верификации запускались;
- какие ограничения/риски остались (если есть).
