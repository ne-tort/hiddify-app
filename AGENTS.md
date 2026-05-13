# AGENTS — handoff: MASQUE multi-endpoint, ошибка «close endpoint timed out after 10s»

Документ для ИИ/разработчика: **текущая задача** — стабильное отключение сессии в Hiddify client при профилях с **множеством** `endpoints` типа `masque` (лабораторный multi-VPS JSON и т.п.). Интеграция GUI `masque` / `warp_masque`, H2/H3/VPS-история и схемы полей по-прежнему в [docs/masque/MASQUE-SINGBOX-CONFIG.md](docs/masque/MASQUE-SINGBOX-CONFIG.md) и [docs/masque/AGENTS-MASQUE-H2-ARCHIVE.md](docs/masque/AGENTS-MASQUE-H2-ARCHIVE.md).

---

## 1. Короткий контекст

- В одном sing-box/Hiddify-конфиге может быть **много** клиентских MASQUE-эндпоинтов (`type: masque`, разные теги/порты/http_layer и т.д.) — см. генератор и примеры в `scripts/Generate-MasqueMultiVpsConfigs.ps1`, `scripts/examples/masque_multi_vps_client*.json`.
- При **остановке ядра / смене профиля / выходе из сессии** клиент закрывает `Box` sing-box. Пользователь видит ошибку вида **`close endpoint timed out after 10s`** (или с длительностью из правки ниже).

---

## 2. Симптом и причина (найдено)

**Симптом:** при отключении в Hiddify для профиля с multi-MASQUE одинаково для всех эндпоинтов всплывает таймаут закрытия endpoint.

**Источник строки:** не Flutter и не gRPC обёртка — это **`sing-box` Box.Close** в форке:

- Файл: `hiddify-core/hiddify-sing-box/box.go`
- Функция: `(*Box).Close` → `closeWithTimeout("endpoint", …, s.endpoint.Close)`
- Раньше для **всех** фаз shutdown использовался один бюджет **`10 * time.Second`**.
- `s.endpoint.Close()` вызывает **`adapter/endpoint/manager.go` → `(*Manager).Close`**, который раньше **последовательно** вызывал `endpoint.Close()` **для каждого** зарегистрированного endpoint.

При десятках/сотнях MASQUE-клиентов каждый `Close()` тянет за собой teardown QUIC/H2/CONNECT-IP (`transport/masque` `coreSession.Close` и т.д.). Суммарное время **легко превышает 10 с** → срабатывает `closeWithTimeout` и возвращается:

`fmt.Errorf("close %s timed out after %s", name, timeout)` → **`close endpoint timed out after 10s`**.

Итог: это **не** «эндпоинт не отвечает» в смысле сети, а **слишком жёсткий бюджет shutdown** при большом числе эндпоинтов (и/или не обновлённый `hiddify-core.dll` после правок в `box.go`).

**Исправления в дереве:**

1. **`box.go`:** для фазы **`endpoint`** бюджет **`3 * time.Minute`** (`endpointCloseTimeout`); остальные фазы — **`10 * time.Second`** (`defaultCloseTimeout`).
2. **`adapter/endpoint/manager.go`:** `Close()` закрывает эндпоинты **последовательно** (как upstream sing-box), с `taskmonitor` на каждый `Close`; карта `endpointByTag` сбрасывается вместе со слайсом.
3. **`lib/hiddifycore/hiddify_core_service.dart`:** у вызова `bgClient.stop` задан **`CallOptions(timeout: 240s)`**, чтобы клиент не обрывал RPC раньше сервера.

**Почему у вас всё ещё было «10s»:** в логе видно ровно `after 10s` — значит в процессе работал **старый** `hiddify-core.dll` (Flutter не подхватил пересобранное ядро). После правок нужно **пересобрать** `hiddify-core/bin/hiddify-core.dll` и снова **`flutter build windows`**, иначе `box.go` не участвует в рантайме.

---

## 3. Карта путей (репозиторий и ядро)

Корень клиента / монорепо: **`hiddify-app`** (в этом окружении: `c:\Users\qwerty\git\hiddify-app`).

| Зона | Путь |
|------|------|
| Flutter-клиент | `lib/` |
| Обёртка ядра / gRPC | `lib/hiddifycore/` (`hiddify_core_service.dart`, `core_interface/core_interface_desktop.dart`, …) |
| Submodule core | `hiddify-core/` |
| Fork sing-box внутри core | `hiddify-core/hiddify-sing-box/` (`go.mod`: `replace github.com/sagernet/sing-box => ./hiddify-sing-box`) |
| Закрытие Box, таймауты | `hiddify-core/hiddify-sing-box/box.go` |
| Менеджер endpoints (`Close`, параллельно + сброс `endpointByTag`) | `hiddify-core/hiddify-sing-box/adapter/endpoint/manager.go` |
| Клиент `type: masque` | `hiddify-core/hiddify-sing-box/protocol/masque/endpoint_client.go` → `common/masque/runtime.go` → `transport/masque/` (`transport.go`, …) |
| Опции JSON | `hiddify-core/hiddify-sing-box/option/masque.go` |
| Сборка libcore (Windows и пр.) | `hiddify-core/Makefile`, `hiddify-app/Makefile` (`windows-prepare`, `build-windows-libs`) |
| Лаб multi VPS (генерация/деплой) | `scripts/Generate-MasqueMultiVpsConfigs.ps1`, `scripts/Deploy-MasqueMultiVps.ps1`, `experiments/router/stand/l3router/configs/masque-server-multi-vps.json` |

---

## 4. Что проверить после правок

- **Сборка Windows portable (рекомендуемый один шаг, без Git Bash):**  
  `powershell -ExecutionPolicy Bypass -File scripts/build-windows-portable.ps1`  
  Полный цикл на PowerShell: ядро (Go+MinGW) → `pub get` / `build_runner` / `slang` → `flutter build windows --release` → копия в `portable/windows-x64/Hiddify` через **robocopy** (не делает `rm -rf` всей portable-папки, чтобы не упираться в занятый `hiddify_portable_data`). Только ядро + три DLL/EXE в уже готовую portable: `-Mode CoreRefresh`. Альтернатива: `make windows-portable` из Git Bash. В Makefile на Windows для `make` задан **`unexport CGO_LDFLAGS`**.
- **Обязательно** пересобрать **`hiddify-core/bin/hiddify-core.dll`** (и при необходимости `HiddifyCli.exe`), затем **`flutter build windows`** — иначе в логе снова будет `after 10s` при старом бинарнике.
- Импортировать multi-клиентский JSON; **включить VPN → выключить** — не должно быть стабильного `close endpoint timed out` из-за числа эндпоинтов.

Go (точечно):

```powershell
Set-Location "c:\Users\qwerty\git\hiddify-app\hiddify-core"
go test ./hiddify-sing-box/... -count=1 -short 2>&1 | Select-Object -First 40
```

---

## 5. Инварианты и секреты

- Не класть в репозиторий и не цитировать в AGENTS реальные `server_token`, ключи, live-профили.
- Не смешивать **`warp_masque`** с legacy WireGuard WARP в документации задач.

---

## 6. Следующие шаги (по желанию)

- Если одиночный MASQUE `Close()` иногда >3 мин: поднять `endpointCloseTimeout` в `box.go`.
- Добавить unit/integration тест на «много stub endpoints» сложно без моков — проще регрессия руками + лог `box.go` на время `close endpoint`.
