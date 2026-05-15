# s-ui: MASQUE и WARP MASQUE

Кратко для операторов и разработчиков форка.

## Breaking change (TLS, 2026)

- В JSON endpoint **больше нет** плоских `certificate` / `key` / `tls_server_name` / `insecure` / `client_tls_*`.
- **Сервер** (`masque`, `mode: server`): в sing-box уходит вложенный объект **`tls`** (полный JSON из строки **`Tls.server`** по `sui_tls_id`). В подписке и у клиента объект **`tls`** и **`sui_tls_id`** **не** передаются.
- **Клиент** (`masque` client / `warp_masque`): в подписку и в итоговый JSON для sing-box подставляется **`outbound_tls`** из **`Tls.client`** той же строки `tls`, на которую ссылается **`sui_tls_id`** в options endpoint. Панель хранит в БД только `sui_tls_id` и общие поля endpoint; PEM/пути живут в таблице `tls`.

## Подписка

- URL: `…/sub/<clientName>?format=json-masque`
- В тело добавляются sing-box `endpoints` типов `masque` и `warp_masque`, если клиент или его группа указаны в `member_client_ids` / `member_group_ids` соответствующего endpoint в панели.
- Поле `server_auth` **не** попадает в подписку; клиентские секреты подставляются из `Client.config.masque` и `Client.config.warp_masque` (например `server_token`).
- После сборки endpoint для подписки вызывается инъекция **`outbound_tls`** из профиля **`Tls.client`** по `sui_tls_id`; из выдачи удаляются **`tls`**, **`sui_tls_id`**, любые остатки серверного материала.

## Серверный MASQUE

- В UI endpoint типа `masque`, режим `server`: `listen`, `listen_port`, выбор **TLS-профиля** (`sui_tls_id`), `server_auth` (JSON). PEM сертификата/ключа задаются **только** в записи TLS панели (`Tls.server`), не в options endpoint.
- Для клиентов в подписке строится режим `client` с `server` = хост запроса подписки и `server_port` из `listen_port`.

### TLS из таблицы `tls` (`sui_tls_id`)

- В форме MASQUE выбирается **сохранённый TLS-профиль** панели: в JSON options хранится `sui_tls_id` (ссылка на строку `model.Tls`). При сохранении для режима `server` **обязателен** ненулевой `sui_tls_id`; плоские TLS-ключи в options **не** сохраняются (см. `normalizeMasqueEndpointOptionsOnSave`).
- Поле **`sui_tls_id` не уходит** в подписку и во внешний JSON endpoint для клиента: его убирает `model.Endpoint.MarshalJSON` для типов `masque` / `warp_masque`.
- При генерации полного конфига для sing-box (`GetAllConfig`, `ApplyRuntimeAction` для `masque` и после слияния для `warp_masque`) вызывается **`mergeMasqueTLSPemFromStoredProfile`**: в options подставляется **`tls`** = JSON из **`Tls.server`** (режим server) или **`outbound_tls`** = JSON из **`Tls.client`** (режим client / `warp_masque`).

## WARP MASQUE

- Тип endpoint `warp_masque`. Чувствительные поля профиля можно хранить в **`ext`**; при применении конфига к sing-box (`GetAllConfig` / runtime) и в подписке `json-masque` они сливаются в `options.profile` через **`MergeWarpMasqueOptionsWithExt`**.
- После слияния **`license_key` из ext** (формат legacy WARP) копируется в **`profile.license`**, т.к. sing-box и `wireguard.GetWarpProfile` опираются на поле **`license`**, а не `license_key`. Из объекта `profile` убираются служебные ключи **`license_key`**, **`access_token`**, **`device_id`** — они остаются в `ext` в БД для **`SetWarpLicense`**, но в выдаваемый sing-box JSON не дублируются.
- **`warpMasqueNeedsCloudflareRegister`** учитывает лицензию и в **`profile.license`**, и в **`ext.license_key`**, чтобы не запускать повторную регистрацию при разнесённом хранении.
- **Авторегистрация WARP:** при первом сохранении нового `warp_masque`, если ещё нет ни consumer-пары (`license` + `private_key` в `profile`), ни Zero Trust (`id` + `auth_token`), сервис вызывает ту же цепочку, что и legacy `warp` (`RegisterWarp`), временно подставляя `type: warp`, затем пересобирает options под **`warp_masque`** (`profile.compatibility: consumer`, `auto_enroll_masque: true` и т.д.) и сохраняет `ext` с токенами для последующего `SetWarpLicense` при редактировании.
- Для **`outbound_tls`** действует тот же **`sui_tls_id`**, что и у generic MASQUE client: в UI выбирается TLS-профиль; клиентская часть (`Tls.client`) попадает в подписку как **`outbound_tls`**.

## QR

В модалке клиента доступен QR для `?format=json-masque` рядом с другими JSON-форматами.
