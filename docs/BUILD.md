# Сборка форка (ne-tort): клиент и ядро

**Канонические remote (именно так задано в git):**

| Роль | URL |
|------|-----|
| Клиент (этот репозиторий) | `https://github.com/ne-tort/hiddify-app` |
| Сабмодуль `hiddify-core` | `https://github.com/ne-tort/hiddify-core` (см. `.gitmodules`, ветка `main`) |

Доработки **клиента** под AWG (парсинг и пр.) — в **ne-tort/hiddify-app**. Отдельные изменения **ядра** (sing-box AWG и т.д.) по мере готовности коммитятся в **ne-tort/hiddify-core** и поднимают SHA сабмодуля здесь.

Репозитории для справки: [hiddify-app](https://github.com/ne-tort/hiddify-app), сабмодуль [hiddify-core](https://github.com/ne-tort/hiddify-core). Исходники **sing-box** и **ray2sing** входят в состав ядра как обычные каталоги `hiddify-core/hiddify-sing-box` и `hiddify-core/ray2sing` (без вложенных git-сабмодулей) и подключаются через `replace` в `hiddify-core/go.mod` на `./hiddify-sing-box` и `./ray2sing`.

## Требования и одна среда

Не смешивайте в одной сборке **разные** установки Go/Flutter (например, Go из WSL и Flutter только на Windows). Выберите профиль и придерживайтесь его.

- **Профиль «Windows-клиент целиком»** (рекомендуется для `make windows-portable`): **Git Bash** или **MSYS2** (POSIX shell), **Flutter for Windows**, **Go for Windows**, **GNU make**, **MinGW-w64** в PATH, **`rsrc`** (`go install github.com/akavel/rsrc@latest`). Команды `make` не запускайте из `cmd.exe` без `sh`.
- **Профиль «только ядро Windows DLL»** в **WSL (Ubuntu)**: `make bootstrap-wsl-deps`, затем `make build-windows-libs` или `bash hiddify-core/scripts/wsl-build-windows-amd64.sh`. Полный Flutter-клиент под Windows в этом профиле не собирается — для него используйте профиль Windows.

Первичная настройка на Windows (один раз): `powershell -ExecutionPolicy Bypass -File scripts/bootstrap-windows.ps1`, затем новый терминал Git Bash и `make windows-env-check`.

- **Flutter**: версия из `pubspec.yaml` (`environment.flutter`, сейчас `^3.38.5`).
- **Go**: версия из `hiddify-core/go.mod` (сейчас `1.25.6`).
- **Linux desktop core**: перед `linux-amd64` нужен **cronet** (`make -C hiddify-core cronet-amd64`) — долго при первом запуске; в CI кэшируется.

## Клонирование

```bash
git clone --recursive https://github.com/ne-tort/hiddify-app.git
cd hiddify-app
# если уже клонировали без сабмодуля:
git submodule update --init --recursive
```

## Сабмодуль `hiddify-core`: это не «просто локальные файлы»

Каталог `hiddify-core/` — **отдельный git-репозиторий** (вложенный клон с `origin` на `ne-tort/hiddify-core`), а не набор файлов без истории. Родительский `hiddify-app` в коммите хранит только **указатель (SHA)** на конкретный коммит ядра.

Чтобы изменения ядра попали на GitHub, нужны **коммиты внутри** `hiddify-core` и **push в `ne-tort/hiddify-core`**, затем коммит в корне `hiddify-app`, который обновляет запись `hiddify-core` на новый SHA, и **push** `ne-tort/hiddify-app`.

### Два репозитория — два коммита (классический цикл)

```bash
cd hiddify-core
git add -A && git commit -m "feat(core): …" && git push origin main
cd ..
git add hiddify-core
git commit -m "chore: bump hiddify-core submodule"
git push origin main
```

### Один `git push` из корня клиента (удобно)

После того как **уже закоммичены** и правки в `hiddify-core`, и обновление указателя в `hiddify-app`, из **корня** репозитория клиента:

```bash
git push --recurse-submodules=on-demand origin main
```

Git сначала отправит незапушенные коммиты сабмодуля (если есть), затем push родителя.

Чтобы не вводить флаг каждый раз (настройка только этого клона):

```bash
git config push.recurseSubmodules on-demand
```

Нужны **права push** в оба репозитория. Если в `hiddify-core` есть только незакоммиченные правки, родитель не сможет зафиксировать новый SHA — сначала `git status` / коммит в каталоге ядра.

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
| Windows amd64    | `make build-windows-libs` (в WSL: сначала `make bootstrap-wsl-deps`, опционально `bash hiddify-core/scripts/wsl-build-windows-amd64.sh`) |
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

### Windows: portable-папка (exe + все DLL)

После `flutter build windows` CMake кладёт в `build/windows/x64/runner/Release/` не только `Hiddify.exe`, но и `flutter_windows.dll`, каталог `data/`, плагины, **`hiddify-core.dll`**, **`libcronet.dll`**, **`HiddifyCli.exe`** (см. `windows/CMakeLists.txt`).

Чтобы автоматически собрать релиз с `portable=true` и скопировать **всё содержимое Release** в одну рабочую папку:

**Рекомендуемый способ (PowerShell + Flutter + Go + MinGW, без Git Bash / make):**  
`powershell -ExecutionPolicy Bypass -File scripts/build-windows-portable.ps1`  
Скрипт: ядро в `hiddify-core\bin\`, `pub get` / `build_runner` / `slang`, `flutter build windows --release` с `portable=true`, затем **robocopy /E** в `portable\windows-x64\Hiddify\` (без `rm -rf` всей папки — не ломает занятый `hiddify_portable_data`). Параметры: `-PortableDst`, `-Mode` (`Full`, `Core`, `Prepare`, `Sync`, `CoreRefresh`).  
Альтернатива: `make windows-portable` из Git Bash; `make windows-portable-sync` удаляет целевой каталог целиком — закройте приложение, если папка занята.

Результат: **`portable/windows-x64/Hiddify/`** (каталог в `.gitignore`). Запуск: `portable/windows-x64/Hiddify/Hiddify.exe`. Данные portable-режима — в **`hiddify_portable_data/`** внутри этой папки.

## CI (GitHub Actions)

- Юнит-тесты: `make get gen translate` + `flutter test` (без нативного ядра).
- Сборка Windows-клиента: ядро собирается на **Ubuntu** (`build-windows-core`), артефакт подставляется в `hiddify-core/bin`, затем на **windows-latest** выполняется `make windows-prepare` с `CORE_PREBUILT_IN_TREE=1`. После `windows-zip-release` дополнительно выполняется **`make windows-portable-sync`** — в рабочем дереве появляется `portable/windows-x64/Hiddify` (удобно для ручной проверки; в артефакты CI по умолчанию не входит).
- Linux / Android / macOS: `actions/checkout` с `submodules: recursive`, затем `make *-prepare` с `core.source=submodule`.

## Отличия форка (AWG и парсинг)

- Встроенное ядро использует кастомный **AWG** в `hiddify-core/hiddify-sing-box` (`protocol/awg`, `transport/awg`).
- Разбор профилей: `hiddify-core/ray2sing/ray2sing/awg.go`; на стороне Flutter — `lib/features/profile/data/profile_parser.dart` (INI с полями AWG → тип `AWG`, plain WG → `WireGuard`).
- В реестре outbound встроенного sing-box для форка отключён **psiphon** (см. историю коммитов ядра).

Проверка вручную: импорт WG и AWG INI, подключение, при необходимости SMB/маршруты по вашему сценарию.

## Сравнение с upstream hiddify-core

Краткий отчёт об отличиях от `hiddify/hiddify-core` (ветка `v3` или `main`): см. [fork-diff-upstream.md](fork-diff-upstream.md) — обновляйте после крупных слияний.
