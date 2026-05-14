# s-ui: MASQUE и WARP MASQUE

Кратко для операторов и разработчиков форка.

## Подписка

- URL: `…/sub/<clientName>?format=json-masque`
- В тело добавляются sing-box `endpoints` типов `masque` и `warp_masque`, если клиент или его группа указаны в `member_client_ids` / `member_group_ids` соответствующего endpoint в панели.
- Поле `server_auth` **не** попадает в подписку; клиентские секреты подставляются из `Client.config.masque` и `Client.config.warp_masque` (например `server_token`).

## Серверный MASQUE

- В UI endpoint типа `masque`, режим `server`: `listen`, `listen_port`, выбор **TLS-сертификата** (строка `tls` / `sui_tls_id`), `server_auth` (JSON). Встроенные поля PEM сертификата/ключа в форме **не** используются — материал берётся только из профиля TLS.
- Для клиентов в подписке строится режим `client` с `server` = хост запроса подписки и `server_port` из `listen_port`.

### TLS из таблицы `tls` (`sui_tls_id`)

- В форме MASQUE (server) выбирается **сохранённый TLS-профиль** панели: в JSON options хранится `sui_tls_id` (ссылка на строку `model.Tls`). При сохранении для режима `server` **обязателен** ненулевой `sui_tls_id`; поля `certificate` / `key` в options **не** сохраняются (очищаются на сервере).
- Поле **`sui_tls_id` не уходит** в подписку и во внешний JSON endpoint: его убирает `model.Endpoint.MarshalJSON` для типов `masque` / `warp_masque`.
- При генерации полного конфига для sing-box (`GetAllConfig`, `ApplyRuntimeAction` для `masque`) вызывается **`mergeMasqueTLSPemFromStoredProfile`**: из `Tls.server` подставляются inline **`certificate`** и **`key`** (если оба PEM заданы), а при пустом **`tls_server_name`** / отсутствии **`insecure`** в options — ещё **`server_name`** и **`insecure`** из того же JSON профиля TLS (как у инбаундов).
- Пути `certificate_path` / `key_path` в merge **не** разрешаются в файлы на диске — только inline PEM в JSON профиля TLS (как и раньше).

## WARP MASQUE

- Тип endpoint `warp_masque`. Чувствительные поля профиля можно хранить в **`ext`**; при применении конфига к sing-box (`GetAllConfig` / runtime) и в подписке `json-masque` они сливаются в `options.profile` через **`MergeWarpMasqueOptionsWithExt`**.
- После слияния **`license_key` из ext** (формат legacy WARP) копируется в **`profile.license`**, т.к. sing-box и `wireguard.GetWarpProfile` опираются на поле **`license`**, а не `license_key`. Из объекта `profile` убираются служебные ключи **`license_key`**, **`access_token`**, **`device_id`** — они остаются в `ext` в БД для **`SetWarpLicense`**, но в выдаваемый sing-box JSON не дублируются.
- **`warpMasqueNeedsCloudflareRegister`** учитывает лицензию и в **`profile.license`**, и в **`ext.license_key`**, чтобы не запускать повторную регистрацию при разнесённом хранении.
- **Авторегистрация WARP:** при первом сохранении нового `warp_masque`, если ещё нет ни consumer-пары (`license` + `private_key` в `profile`), ни Zero Trust (`id` + `auth_token`), сервис вызывает ту же цепочку, что и legacy `warp` (`RegisterWarp`), временно подставляя `type: warp`, затем пересобирает options под **`warp_masque`** (`profile.compatibility: consumer`, `auto_enroll_masque: true` и т.д.) и сохраняет `ext` с токенами для последующего `SetWarpLicense` при редактировании.

## QR

В модалке клиента доступен QR для `?format=json-masque` рядом с другими JSON-форматами.
