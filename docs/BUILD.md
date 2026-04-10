# Сборка форка (ne-tort): клиент и ядро

Репозитории: [hiddify-app](https://github.com/ne-tort/hiddify-app), сабмодуль [hiddify-core](https://github.com/ne-tort/hiddify-core). Исходники **sing-box** и **ray2sing** входят в состав ядра как обычные каталоги `hiddify-core/hiddify-sing-box` и `hiddify-core/ray2sing` (без вложенных git-сабмодулей) и подключаются через `replace` в `hiddify-core/go.mod` на `./hiddify-sing-box` и `./ray2sing`.

## Требования

- **Flutter**: версия из `pubspec.yaml` (`environment.flutter`, сейчас `^3.38.5`).
- **Go**: версия из `hiddify-core/go.mod` (сейчас `1.25.6`).
- **Windows DLL / CLI ядра**: сборка `make -C hiddify-core windows-amd64` использует **bash и MinGW (кросс-компиляция)**. На Windows удобнее **WSL2 (Ubuntu)** или CI; нативный PowerShell без MinGW для этой цели не подходит.
- **Linux desktop core**: перед `linux-amd64` нужен **cronet** (`make -C hiddify-core cronet-amd64`) — долго при первом запуске; в CI кэшируется.

## Клонирование

```bash
git clone --recursive https://github.com/ne-tort/hiddify-app.git
cd hiddify-app
# если уже клонировали без сабмодуля:
git submodule update --init --recursive
```

## Источник нативного ядра (`dependencies.properties`)

| `core.source`   | Поведение |
|-----------------|-----------|
| `submodule`     | По умолчанию: собрать ядро из `./hiddify-core` (`make build-*-libs` / цели `*-core-resolve`). |
| `prebuilt`      | Скачать готовые архивы `hiddify-lib-*` с `core.prebuilt.base` (ветка `draft` или `v$(core.version)` в зависимости от `CHANNEL`). |

Переменная **`CORE_PREBUILT_IN_TREE=1`**: не собирать и не качать — уже заполненный `hiddify-core/bin` (используется в CI для Windows после загрузки артефакта).

## Команды: ядро

Из корня репозитория приложения:

| Платформа        | Команда |
|------------------|---------|
| Windows amd64    | `make build-windows-libs` |
| Linux amd64      | `make build-linux-libs` (cronet + core) |
| Linux arm64      | `make build-linux-arm64-libs` |
| Linux amd64 musl | `make build-linux-amd64-musl-libs` |
| Linux arm64 musl | `make build-linux-arm64-musl-libs` |
| Android AAR      | `make build-android-libs` (нужны JDK, Android SDK/NDK) |
| macOS            | `make build-macos-libs` |
| iOS              | `make build-ios-libs` |

Артефакты появляются в `hiddify-core/bin/` (и для Android AAR копируется в `android/app/libs/`).

## Команды: клиент

После сборки ядра (или при `core.source=prebuilt`):

```bash
make windows-prepare    # или linux-amd64-prepare, android-prepare, macos-prepare, ios-prepare
make windows-release    # см. Makefile: есть linux-release, android-*, macos-release
```

Локальный запуск без полного релиза: `flutter run` (после соответствующего `*-prepare`).

## CI (GitHub Actions)

- Юнит-тесты: `make get gen translate` + `flutter test` (без нативного ядра).
- Сборка Windows-клиента: ядро собирается на **Ubuntu** (`build-windows-core`), артефакт подставляется в `hiddify-core/bin`, затем на **windows-latest** выполняется `make windows-prepare` с `CORE_PREBUILT_IN_TREE=1`.
- Linux / Android / macOS: `actions/checkout` с `submodules: recursive`, затем `make *-prepare` с `core.source=submodule`.

## Отличия форка (AWG и парсинг)

- Встроенное ядро использует кастомный **AWG** в `hiddify-core/hiddify-sing-box` (`protocol/awg`, `transport/awg`).
- Разбор профилей: `hiddify-core/ray2sing/ray2sing/awg.go`; на стороне Flutter — `lib/features/profile/data/profile_parser.dart` (INI с полями AWG → тип `AWG`, plain WG → `WireGuard`).
- В реестре outbound встроенного sing-box для форка отключён **psiphon** (см. историю коммитов ядра).

Проверка вручную: импорт WG и AWG INI, подключение, при необходимости SMB/маршруты по вашему сценарию.

## Сравнение с upstream hiddify-core

Краткий отчёт об отличиях от `hiddify/hiddify-core` (ветка `v3` или `main`): см. [fork-diff-upstream.md](fork-diff-upstream.md) — обновляйте после крупных слияний.
