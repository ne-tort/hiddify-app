# AGENTS — handoff: GUI-интеграция `masque` и `warp_masque`

Краткий документ для ИИ/разработчика по текущей задаче: встроить endpoints **`type: masque`** и **`type: warp_masque`** в Hiddify client / Flutter GUI без потери параметров и с включением MASQUE в сборку core. История H2/H3/VPS и низкоуровневого WARP MASQUE перенесена в [docs/masque/AGENTS-MASQUE-H2-ARCHIVE.md](docs/masque/AGENTS-MASQUE-H2-ARCHIVE.md) и [docs/masque/MASQUE-SINGBOX-CONFIG.md](docs/masque/MASQUE-SINGBOX-CONFIG.md).

---

## 1. Цель задачи

Сделать так, чтобы Hiddify client:

1. Импортировал `masque` и `warp_masque` endpoints из подписок, локального JSON и "Вставить из буфера обмена".
2. Позволял редактировать такие профили как raw text в существующем редакторе профиля.
3. Не нормализовал и не переписывал поля `masque` / `warp_masque` на уровне GUI-парсинга: параметры должны доходить до core/sing-box как есть, кроме уже существующей общей оболочки Hiddify config.
4. Собирал core/libcore с поддержкой MASQUE во всех целевых бинарниках клиента.
5. Не трогал WARP settings page в первой реализации: MASQUE/WireGuard вкладки и генерация `warp_masque` preset остаются отдельной следующей задачей.

---

## 2. Важные инварианты

- Не превращать `warp_masque` в legacy WireGuard WARP. Это отдельный sing-box endpoint с собственным `profile` и MASQUE bootstrap.
- Не выкидывать неизвестные поля `masque` / `warp_masque`, не пересобирать JSON вручную в Dart, если можно передать raw config в core.
- Не добавлять секреты в логи, тестовые fixtures или docs: `auth_token`, private keys, `masque_ecdsa_private_key`, полный live profile.
- Для generic `masque` и `warp_masque` считать источником схемы `hiddify-core/hiddify-sing-box/option/masque.go`.
- При изменениях core config builder проверять, что обычные outbounds, WireGuard, AWG, WARP и raw sing-box JSON не ломаются.
- В Plan mode редактировать только markdown; код менять только после явного перехода в Agent mode.

---

## 3. Карта репозитория

Корень: `c:\Users\qwerty\git\hiddify-app`.

Flutter / GUI:

- `lib/features/profile/notifier/profile_notifier.dart` — импорт из буфера: `AddProfileNotifier.addClipboard`.
- `lib/features/profile/data/profile_parser.dart` — определение имени/протокола профиля, headers override, `safeDecodeBase64`, `protocol()`.
- `lib/features/profile/data/profile_repository.dart` — запись temp file, `validateConfig`, `offlineUpdate`, `getRawConfig`.
- `lib/features/profile/add/add_profile_modal.dart` — UI добавления профиля, manual URL и clipboard flow.
- `lib/features/profile/details/profile_details_page.dart` — существующий raw text editor профиля (`TextFormField` с `data.configContent`).
- `lib/features/profile/details/profile_details_notifier.dart` — сохранение raw content из details page.
- `lib/singbox/model/singbox_proxy_type.dart` — labels протоколов; сейчас нет `masque` / `warp_masque`.
- `test/features/profile/data/profile_parser_test.dart` — тесты `ProfileParser.protocol`.

WARP GUI / prefs (только для справки, в первой реализации не менять):

- `lib/features/settings/overview/sections/warp_options_page.dart` — текущая WARP settings page; не трогать в первой итерации.
- `lib/features/settings/notifier/warp_option/warp_option_notifier.dart` — генерирует `warp` и `warp2`, сохраняет prefs.
- `lib/features/settings/data/config_option_repository.dart` — `ConfigOptions`, private keys exclusion, `singboxConfigOptions`.
- `lib/singbox/model/singbox_config_option.dart` — Dart JSON model для Hiddify options (`SingboxWarpOption`).
- `lib/singbox/model/singbox_config_enum.dart` — `WarpDetourMode`, service modes, enum labels.
- `lib/singbox/model/warp_account.dart` — парсинг ответа генерации WARP config.
- `lib/hiddifycore/hiddify_core_service.dart` — Dart gRPC wrapper, `generateWarpConfig`.

Core / config builder:

- `hiddify-core/v2/config/parser.go` — JSON/subscription parser в core; full config vs endpoints/outbounds extraction; `patchConfigOptions`.
- `hiddify-core/v2/config/config.go` — `ReadSingOptions`, `BuildConfigJson`.
- `hiddify-core/v2/config/builder.go` — `BuildConfig`, `setOutbounds`, перенос input endpoints/outbounds в итоговый config.
- `hiddify-core/v2/config/outbound.go` — `patchEndpoint`, `patchOutbound`.
- `hiddify-core/v2/config/hiddify_option.go` — `HiddifyOptions`, `WarpOptions`.
- `hiddify-core/v2/config/warp.go` — legacy WARP/WireGuard generation and `patchWarp`.
- `hiddify-core/v2/hcore/warp.go` — gRPC `GenerateWarpConfig`; сейчас возвращает пустой stub, старый реальный код закомментирован.

MASQUE в sing-box fork:

- `hiddify-core/hiddify-sing-box/option/masque.go` — JSON поля `MasqueEndpointOptions`, `WarpMasqueEndpointOptions`, `WarpMasqueProfileOptions`.
- `hiddify-core/hiddify-sing-box/protocol/masque/endpoint.go` — generic `masque` validation.
- `hiddify-core/hiddify-sing-box/protocol/masque/endpoint_warp_masque.go` — `warp_masque` endpoint bootstrap/runtime.
- `hiddify-core/hiddify-sing-box/protocol/masque/warp_control_adapter.go` — Cloudflare profile, MASQUE ECDSA enroll/state/cache.
- `hiddify-core/hiddify-sing-box/transport/masque/` — H2/H3 CONNECT-UDP/IP/stream dataplane.

Сборка:

- `hiddify-core/cmd/internal/build_libcore/main.go` — сборка libcore для desktop/mobile; теги берутся из `build_shared.CoreSingBoxBaseTags()` / `CoreSingBoxTagsWindows()`.
- `hiddify-core/cmd/internal/build_shared/sdk.go` — сейчас в рабочем дереве не видно функций build tags; проверить до правок сборки.
- `.github/workflows/build.yml` — сборка Flutter app; Windows core собирается отдельно через `hiddify-core make windows-amd64`.
- `hiddify-core/.github/workflows/build.yml` — сборка core artifacts.
- `hiddify-core/build_windows.bat`, Makefile в `hiddify-core/` — локальная/CI автоматизация core.

---

## 4. Текущие наблюдения перед реализацией

Импорт через clipboard:

- `AddProfileNotifier.addClipboard` сначала пробует `LinkParser.parse(rawInput)` и remote URL.
- Если это не URL, вызывает `_profilesRepo.addLocal(safeDecodeBase64(rawInput))`.
- `ProfileRepository.addLocal` пишет content во временный файл, вызывает `ProfileParser.addLocal`, затем `validateConfig`.
- `validateConfig` вызывает core parse/check через `_singbox.validateConfigByPath`.

Редактирование:

- `ProfileDetailsPage` уже показывает raw profile content и вызывает `setContent`.
- Нужно проверить `ProfileDetailsNotifier.save()` и `ProfileRepository.offlineUpdate`, чтобы raw JSON `endpoints: [{type: masque|warp_masque, ...}]` сохранялся без лишней сериализации.

Core parser:

- JSON с `outbounds`/`endpoints` идёт через `options.UnmarshalJSONContext`.
- Если full config выключен, parser оставляет `outbounds`, `endpoints` и только TUN-inbounds.
- `patchConfigOptions` вызывает `patchWarp` для endpoints. Важно убедиться, что `patchWarp` не трогает `TypeWarpMasque` и `TypeMasque`.
- `BuildConfig` в `setOutbounds` переносит input endpoints в `options.Endpoints` после `patchEndpoint`; это главный путь, где нельзя потерять `masque` endpoints.

WARP GUI:

- Текущая WARP страница завязана на legacy `ConfigOptions.warp*` и `SingboxWarpOption`.
- `WarpOptionNotifier.genWarps()` генерирует две legacy WARP записи (`warp`, `warp2`) через `generateWarpConfig`.
- `hiddify-core/v2/hcore/warp.go` сейчас возвращает пустой `WarpGenerationResponse`; перед интеграцией MASQUE WARP надо решить, восстанавливать ли legacy генерацию или добавить отдельный MASQUE generator рядом.
- MASQUE WARP может использовать `profile.license`/`profile.private_key` consumer flow или `profile.id`/`profile.auth_token` Zero Trust flow; core уже умеет auto enroll MASQUE ECDSA через `warp_control_adapter.go`.

Сборка:

- MASQUE уже используется в stand-бинарнике с тегом `with_masque`.
- Клиентские libcore сборки должны получить тот же тег в общей функции build tags, иначе GUI сможет импортировать JSON, но core не зарегистрирует endpoint.
- До правок проверить фактическую реализацию `CoreSingBoxBaseTags()` / `CoreSingBoxTagsWindows()` в текущем дереве: ссылки есть в `build_libcore/main.go`, но быстрый поиск по workspace их не нашёл.

---

## 5. Что должны проверить субагенты

Перед запуском субагентов передать им этот файл и конкретные зоны:

1. **Import/parser agent**
   - Файлы: `lib/features/profile/*`, `lib/singbox/model/singbox_proxy_type.dart`, `test/features/profile/data/profile_parser_test.dart`, `hiddify-core/v2/config/parser.go`, `hiddify-core/v2/config/builder.go`.
   - Ответить: где именно добавить detection labels `masque` / `warp_masque`; есть ли место, где JSON endpoint поля теряются; какие тесты нужны для clipboard/subscription/raw editor.

2. **WARP GUI agent**
   - Файлы: `warp_options_page.dart`, `warp_option_notifier.dart`, `config_option_repository.dart`, `singbox_config_option.dart`, `hiddify_core_service.dart`, `hiddify-core/v2/hcore/warp.go`, `hiddify-core/v2/config/warp.go`, `warp_control_adapter.go`.
   - Ответить: как устроена старая WireGuard WARP генерация; что сейчас stub; какой минимальный UI/модель для вкладок WireGuard/MASQUE; где хранить MASQUE state без утечек.

3. **Build/core agent**
   - Файлы: `hiddify-core/cmd/internal/build_libcore/main.go`, `hiddify-core/cmd/internal/build_shared/`, `hiddify-core/Makefile`, `.github/workflows/build.yml`, `hiddify-core/.github/workflows/build.yml`.
   - Ответить: где добавить `with_masque`; почему отсутствуют найденные build tag helpers; какие локальные и CI команды подтвердят включение endpoint.

4. **Tests/QA agent**
   - Файлы: Flutter tests, Go tests around `v2/config`, MASQUE unit tests.
   - Ответить: минимальный набор тестов без VPS для GUI-интеграции; когда нужен VPS smoke только для runtime `warp_masque`.

---

## 6. Предварительный план реализации

1. Зафиксировать поведение импорта:
   - добавить `masque` и `warp_masque` в `ProxyType`/`ProfileParser.protocol`;
   - добавить unit tests на raw JSON local content, single endpoint JSON и subscription text с `endpoints`;
   - убедиться, что `safeDecodeBase64` и `expandRemoteLinesInParallel` не ломают JSON.

2. Зафиксировать raw editing:
   - проверить и при необходимости поправить `ProfileDetailsNotifier` / `offlineUpdate`, чтобы редактирование endpoint JSON не меняло поля `profile`, `hops`, `http_layer*`, `server_token`, `masque_ecdsa_private_key`;
   - добавить regression test на сохранение raw `warp_masque` JSON.

3. Проверить core config path:
   - добавить Go tests для `ParseConfig` / `BuildConfigJson` с `type: masque` и `type: warp_masque`;
   - подтвердить, что `patchWarp` не применяется к MASQUE types;
   - если builder фильтрует endpoint tags или теряет endpoints, исправить точечно.

4. Включить MASQUE в libcore:
   - найти фактический список build tags;
   - добавить `with_masque` в базовые desktop/mobile tags и Windows tags;
   - добавить проверку/тест команды, что endpoint регистрируется в собранном core.

5. WARP settings не менять:
   - не добавлять вкладки;
   - не менять `WarpOptionsPage`, `WarpOptionNotifier`, `ConfigOptions.warp*`;
   - не проектировать генератор `warp_masque` preset в этой итерации.

---

## 7. Критерии готовности

- Clipboard/local import принимает JSON с `endpoints: [{ "type": "masque", ... }]` и `endpoints: [{ "type": "warp_masque", ... }]`.
- Remote subscription с таким JSON проходит download, parse, validate и появляется в списке профилей с понятным именем.
- Raw editor сохраняет эти профили без удаления/переименования MASQUE-specific полей.
- Generated full config содержит endpoint `type: masque` / `type: warp_masque` в `endpoints`.
- Клиентский core/libcore собирается с `with_masque` на целевых платформах.
- Старый WARP WireGuard UI не регрессирует, потому что его файлы не меняются.
- Секреты не попадают в logs/tests/docs.

---

## 8. Локальные проверки

Flutter:

```powershell
Set-Location "$REPO_ROOT"
flutter test test/features/profile/data/profile_parser_test.dart
```

Core config:

```powershell
Set-Location "$REPO_ROOT\hiddify-core"
Remove-Item Env:GOOS, Env:GOARCH -ErrorAction SilentlyContinue
go test ./v2/config -count=1
```

MASQUE core regressions, если трогались `hiddify-sing-box` MASQUE файлы:

```powershell
Set-Location "$REPO_ROOT\hiddify-core"
.\scripts\go-test-masque.ps1
```

Libcore build tags:

```powershell
Set-Location "$REPO_ROOT\hiddify-core"
go test ./cmd/internal/... -count=1
```

Если меняется runtime `warp_masque`, а не только GUI/import/build tags, нужен старый VPS smoke из архива: собрать `sing-box` с `with_masque`, задеплоить на стенд, проверить `warp=on` без публикации секретов.

---

## 9. Справка по MASQUE JSON

Минимальный generic `masque` пример для тестов:

```json
{
  "endpoints": [
    {
      "type": "masque",
      "tag": "masque-client",
      "server": "example.com",
      "server_port": 443,
      "transport_mode": "connect_ip",
      "fallback_policy": "strict",
      "tcp_mode": "strict_masque",
      "tcp_transport": "connect_stream",
      "template_ip": "https://example.com:443/masque/ip",
      "template_tcp": "https://example.com:443/masque/tcp/{target_host}/{target_port}",
      "tls_server_name": "example.com"
    }
  ]
}
```

Для **CONNECT-UDP / `auto`** без явного `tcp_transport` достаточно полей как в `TestBuildConfigMinimalMasqueEndpointPassthrough` ([`v2/config/masque_passthrough_test.go`](hiddify-core/v2/config/masque_passthrough_test.go)): `type`, `tag`, `server`, `server_port`, при необходимости `insecure` / TLS; ядро подставит **`tcp_transport: connect_stream`**, если `transport_mode` не **`connect_ip`**. Не смешивайте **`transport_mode: connect_udp`** с **`template_ip`** в осмысленном конфиге — лишний `template_ip` на старте endpoint очищается (профиль на диске через `BuildConfig` может сохранить оба ключа).

`warp_masque` поля описаны в `docs/masque/MASQUE-SINGBOX-CONFIG.md` и `hiddify-core/hiddify-sing-box/option/masque.go`. Для тестов использовать фиктивные значения и не коммитить реальные токены/ключи.
