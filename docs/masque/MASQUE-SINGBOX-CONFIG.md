# MASQUE в sing-box (hiddify fork): конфиг, режимы, дефолты, interop

Краткая шпаргалка по полям JSON для endpoint **`type: masque`** (и типам, расширяющим его профилем, например `warp_masque`). Полный норматив dataplane см. [`IDEAL-MASQUE-ARCHITECTURE.md`](../../IDEAL-MASQUE-ARCHITECTURE.md). ADR/топология — [`hiddify-core/docs/masque-warp-architecture.md`](../../hiddify-core/docs/masque-warp-architecture.md).

| Слой | Файлы в дереве |
|------|----------------|
| Объявление полей (`MasqueEndpointOptions`, константы) | [`hiddify-core/hiddify-sing-box/option/masque.go`](../../hiddify-core/hiddify-sing-box/option/masque.go) |
| Валидация, нормализация режимов, дефолт шаблонов на сервере | [`hiddify-core/hiddify-sing-box/protocol/masque/endpoint.go`](../../hiddify-core/hiddify-sing-box/protocol/masque/endpoint.go) (`validateMasqueOptions`, `normalize*`, `defaultTemplateIfEmpty`) |
| Клиентский QUIC/H3 CONNECT-UDP/IP/stream | [`hiddify-core/hiddify-sing-box/transport/masque/transport.go`](../../hiddify-core/hiddify-sing-box/transport/masque/transport.go) (`buildTemplates`, factories) |
| HTTP/3 listener, multiplex CONNECT-UDP / CONNECT-IP / TCP | [`hiddify-core/hiddify-sing-box/protocol/masque/endpoint_server.go`](../../hiddify-core/hiddify-sing-box/protocol/masque/endpoint_server.go) |
| WARP-bootstrap дефолт сервера | [`hiddify-core/hiddify-sing-box/protocol/masque/endpoint_warp_masque.go`](../../hiddify-core/hiddify-sing-box/protocol/masque/endpoint_warp_masque.go) |

В форке отдельного outbound `type: masque` нет: **одна схема опций**, разделение **клиент/сервер** через **`mode`** и `validateMasqueOptions`.

---

## 1. Режимы (`mode`)

| `mode` (JSON) | Описание |
|---------------|----------|
| пусто / `client` | Клиент: dial к удалённому MASQUE (поля `server`, транспорт, шаблоны TCP/UDP/IP). По нормализации пустое → client. |
| `server` | Сервер HTTP/3: `listen_port`, `certificate`, `key` обязательны; клиентские поля (`server`, hops, transport_mode, tcp_*, connect_ip_scope_*) **запрещены**. |

---

## 2. Плоскость UDP: `transport_mode`

| Значение | Нормализация пустого |
|----------|----------------------|
| `auto` | default |
| `connect_udp` | UDP через CONNECT-UDP + `template_udp`; **`template_ip` запрещён** |
| `connect_ip` | UDP через CONNECT-IP + UDP-мост + `template_ip`; **`template_udp` запрещён** |

Правило валидации: несовместимые пары `template_udp` / `template_ip` отсекаются см. [`endpoint.go`](../../hiddify-core/hiddify-sing-box/protocol/masque/endpoint.go).

**Flow forwarding CONNECT-IP:** если заданы `connect_ip_scope_target` и/или `connect_ip_scope_ipproto`, обязательны `transport_mode: connect_ip` и `template_ip` с подстроками **`{target}`** и **`{ipproto}`**.

### 2.1 Проверка по коду: имя режима ↔ фактический носитель (клиент [`transport.go`](../../hiddify-core/hiddify-sing-box/transport/masque/transport.go))

`coreSession.ListenPacket`:

- Если **`transport_mode` (после trim) эквивалентен строке `connect_ip`** (`EqualFold`), вызывается **`openIPSessionLocked`** → `connectip.Dial`, дальше **`newConnectIPUDPPacketConn`**. Это **CONNECT-IP** для носителя, но интерфейс к роутеру для «UDP-сокета к хосту/порту» реализован как **IPv4/UDP инкапсуляция в IP-плоскость** того же CONNECT-IP (не отдельный CONNECT-UDP).
- Иначе (в том числе **`auto`** или **`connect_udp`**) — **`client.DialAddr`** через **CONNECT-UDP** (`Connect-udp`/`qmasque.Client`): между клиентом и целью **`host:port`** передаётся **payload UDP-датаграмм**, без полной пользовательской IP-датаграммы как семантики MASQUE-UDP.

Разница **`connect_udp`** vs **`auto`** в этом коде только в **валидируемых полях** (`template_udp` без `template_ip` у `connect_udp` и т.д. — см. `validateMasqueOptions` в [`endpoint.go`](../../hiddify-core/hiddify-sing-box/protocol/masque/endpoint.go)); **ветка ListenPacket одинаково идёт в CONNECT-UDP**, пока режим явно не `connect_ip`.

**Полная IP-датаграмма (байты IPv4/IPv6 пакета) на границе сессии** — у **`OpenIPSession` / `IPPacketSession`** (`ReadPacket` / `WritePacket` → `connectip.Conn`). Эту плоскость используют маршрут/TUN-слой и при необходимости **TCP через netstack** ([`netstack_adapter.go`](../../hiddify-core/hiddify-sing-box/transport/masque/netstack_adapter.go)) поверх того же CONNECT-IP. Она **не** заменяет носитель **`ListenPacket`** в ветке `connect_udp/auto`.

TCP как **MASQUE CONNECT-stream** — отдельно: **`tcp_transport: connect_stream`**, см. ниже §3.

### 2.2 CONNECT-IP и L3 (теория и поддержка кодом RFC-ориентируемого канала)

- На wire CONNECT-IP ( [`connect-ip-go`](../../hiddify-core/hiddify-sing-box/third_party/connect-ip-go) / RFC 9484 семантика) несёт **IP-пакеты** в QUIC HTTP Datagram (**context-id = 0** — см. общий контракт в [`IDEAL-MASQUE-ARCHITECTURE.md`](../../IDEAL-MASQUE-ARCHITECTURE.md)).
- Сервер после `ipProxy.Proxy` вызывает **`AssignAddresses`** и **`AdvertiseRoute`** (пример см. [`endpoint_server.go`](../../hiddify-core/hiddify-sing-box/protocol/masque/endpoint_server.go)) и отдаёт пакеты в **`RoutePacketConnectionEx`** — т.е. есть явные блоки для **назначения адресов и маршрутов** на стороне прокси, как у виртуальных L3-интерфейсов.
- Клиентский **`connectIPPacketSession.ReadPacket` / `WritePacket`** пробрасывают сырые байты IP в **`connectip.Conn`** — это и есть транспорт «виртуальной IP-поды» между концами, на которую можно навесить статическую/динамическую маршрутизацию при правильном бэкенде.

Оградные факторы продукта (политика провайдеров, IPv4-only UDP-мост, лимиты `mtu`/`ceiling`, конкретные префиксы на сервере) не отменяют того, что **носитель действительно IP**, а конфиг-слой именует это **`transport_mode: connect_ip`**.

---

## 3. TCP: два разных контракта (не путать)

1. **`tcp_transport: connect_stream`** (на клиенте **обязателен явно**; `auto`/`connect_ip`/пусто — ошибка валидации) — релей TCP через MASQUE **CONNECT stream** по `template_tcp` с `{target_host}`/`{target_port}`.
2. **TCP через IP-пакетную плоскость** — это **`transport_mode: connect_ip`** + netstack/IP session, **не** значение `tcp_transport = connect_ip` (она **запрещена**: «TUN-only» ошибка из валидации).

**Fallback на прямой TCP:** только пара **`tcp_mode: masque_or_direct`** + **`fallback_policy: direct_explicit`**. Иные сочетания отклоняются.

---

## 4. Цепочка (`hop_policy`)

- пустое → трактуется как **`single`**
- **`chain`**: нужен непустой `hops`, у каждого hop — **`server`**, **`server_port`** (>0); `tag` уникальны.

---

## 5. Протоколы: возможности и компенсация

### UDP

- **CONNECT-UDP:** крупный `WriteTo` режется до безопасного размера датаграммы (см. `masqueUDPDatagramSplitConn`, [`transport.go`](../../hiddify-core/hiddify-sing-box/transport/masque/transport.go)).
- **CONNECT-IP UDP-мост:** в продуктовом коде ограничения IPv4-к цели, фиксированный исходный порт в шаблоне, потолки payload/PMTU (`connectIPUDPPacketConn`).
- Компенсация: **`mtu`** как потолок IP-датаграммы CONNECT-IP, env **`HIDDIFY_MASQUE_DATAGRAM_CEILING_MAX`** для верхнего клампа; не путать с `tun_mtu` ОС (см. IDEAL §1).

### TCP

- STREAM: retry до 3 раз на некоторые ошибки; дедлайны на `streamConn` требуют поддержки нижним reader/writer (`ErrDeadlineUnsupported` иначе).

### ICMP / PMTU

- Отдельного поля конфига «включить ICMP» нет: PTB/feedback в циклах CONNECT-IP (клиент/сервер), policy ICMP в **`connect-ip-go`**. Компенсация — согласованный `mtu`, осторожное отключение PMTU только при необходимости (`quic_experimental` под env **`MASQUE_EXPERIMENTAL_QUIC=1`**).

---

## 6. Поле `mtu` в JSON

В [`masque.go`](../../hiddify-core/hiddify-sing-box/option/masque.go) поле **`mtu`** пробрасывается в клиентском рантайме как **`ConnectIPDatagramCeiling`** (потолок полного IP-датаграммы для CONNECT-IP). Диапазон при ненулевом значении: **[1280, 65535]** (`validateMasqueOptions`).

---

## 7. Плоскость конфига: дефолты, «стандарт MASQUE», конфликты

Источник: **`validateMasqueOptions`** + **`normalize*`** в [`endpoint.go`](../../hiddify-core/hiddify-sing-box/protocol/masque/endpoint.go), **`buildTemplates`** + **`NewSession`** в [`transport.go`](../../hiddify-core/hiddify-sing-box/transport/masque/transport.go).

### 7.1 RFC / спецификация vs дефолты этого форка

- **RFC 9298 / 9484** задают семантику CONNECT-UDP / CONNECT-IP (Extended CONNECT, H3 DATAGRAM, капсулы), но **не фиксируют** обязательный path в URL вида `/masque/udp/...`. Это **согласование между клиентом и сервером**.
- В этом форке приняты **согласованные дефолты путей** (удобно для «написал минимум полей»):
  - **Клиент** (`buildTemplates`, если строка шаблона пустая):  
    `https://{server}:{port}/masque/udp/{target_host}/{target_port}`,  
    `https://{server}:{port}/masque/ip`,  
    `https://{server}:{port}/masque/tcp/{target_host}/{target_port}`.
  - **Сервер** (`defaultTemplateIfEmpty`): тот же **path** (`/masque/udp/...`, `/masque/ip`, `/masque/tcp/...`), но хост в строке по умолчанию **`masque.local`** — он нужен только чтобы распарсить **path** для mux; реальный TLS/SNI на сервере задаётся `listen` + клиентский `server`/`tls_server_name`.
- **Вывод:** опускать `template_*` можно **только если** peer использует те же path (как в дефолтах выше). Сторонний сервер с другим path потребует **явных** `template_*` с обеих сторон.

### 7.2 Дефолты по полям (после нормализации / до рантайма)

| Поле (клиент) | Пусто / не задано | Замечание |
|----------------|-------------------|-----------|
| `mode` | **`client`** | `normalizeMode` |
| `transport_mode` | **`auto`** | `normalizeTransportMode` |
| `hop_policy` | **`single`** | в `validateMasqueOptions` |
| `fallback_policy` | **`strict`** | |
| `tcp_mode` | **`strict_masque`** | |
| `server_port` | **`443`** | `buildTemplates` (после подстановки hop) |
| `template_udp` / `template_ip` / `template_tcp` | см. §7.1 | Если заданы непустые строки — **должны** содержать `{target_host}`/`{target_port}` там, где это требует валидатор (см. §7.4). |
| `mtu` | **внутренний потолок CONNECT-IP ≈ 1500** байт IP-датаграммы (типичный Ethernet path) | `NewSession`: если `mtu` 0 — эффективный ceiling `min(1500, HIDDIFY_MASQUE_DATAGRAM_CEILING_MAX)`; при `mtu > 0` — диапазон [1280, 65535] в валидаторе, затем кламп к env-max |
| `server_token` | нет Bearer | сервер с пустым токеном тоже без app-auth |
| `tcp_transport` | — | **нельзя** оставить пустым: валидация требует **явно** `connect_stream` (пустое и `auto` отклоняются) |

| Поле (сервер `mode: server`) | Пусто / не задано |
|------------------------------|-------------------|
| `template_udp` / `template_ip` / `template_tcp` | подставляются строки с **`https://masque.local/masque/...`** и теми же path, что у клиентских дефолтов |
| Клиентские поля `transport_mode`, `fallback_policy`, `tcp_mode`, `tcp_transport`, `connect_ip_scope_*` | **должны отсутствовать** (непустое значение → ошибка) |

**`warp_masque`:** при пустом `server` и не-chain подставляется **`bootstrap.warp.invalid:443`** ([`endpoint_warp_masque.go`](../../hiddify-core/hiddify-sing-box/protocol/masque/endpoint_warp_masque.go)) — это не generic MASQUE, а профиль WARP.

#### 7.2.1 `warp_masque`: профиль Cloudflare и наследование полей MASQUE

Тип **`warp_masque`** встраивает те же поля, что и **`type: masque`** (`MasqueEndpointOptions`): `transport_mode` (в т.ч. **`connect_udp`** как более мягкий smoke на живом edge), `server_token`, шаблоны, `hop_policy`/`hops`, корневой **`detour`** маршрутизатора, `tcp_*`, `tls_server_name`, и т.д. Валидация **`validateMasqueOptions`** выполняется для вложенного блока так же, как у generic клиента.

Дополнительно задаётся объект **`profile`** ([`option/masque.go`](../../hiddify-core/hiddify-sing-box/option/masque.go) `WarpMasqueProfileOptions`):

| JSON | Назначение |
|------|------------|
| `profile.compatibility` | `auto` / `consumer` / `zero_trust` / `both` — выбор сценария embedded registration (`validateWarpMasqueOptions`). |
| `profile.id`, `profile.auth_token` | Zero Trust / уже созданное устройство: получение профиля через API. |
| `profile.license`, `profile.private_key` | Consumer: создание/получение профиля (как у legacy `warp`). |
| `profile.recreate` | Сброс кэша / принудительное обновление пути bootstrap в адаптере. |
| `profile.detour` | **Только control-plane:** исходящий detour для запросов к Cloudflare API при `GetWarpProfile` (не путать с корневым `detour` endpoint для dataplane). |
| `profile.dataplane_port` | Жёсткая подмена **одного** UDP/QUIC-порта (если не `0`): игнорируется авто-подбор кандидатов. Пример: **`443`**, когда профиль API отдаёт WG-first порт вроде **`2408`**, а MASQUE QUIC ожидается на другом порту. Код: [`warp_control_adapter.go`](../../hiddify-core/hiddify-sing-box/protocol/masque/warp_control_adapter.go). |
| `profile.dataplane_port_strategy` | `auto` (по умолчанию) или **`api_first`**. При **`auto`** и `policy.tunnel_protocol`, указывающем на **MASQUE**, ядро строит **упорядоченный список UDP-портов**: сначала типичные порты MASQUE по [документации Cloudflare One firewall](https://developers.cloudflare.com/cloudflare-one/connections/connect-devices/warp/deployment/firewall/) (**443, 4443, 8443, 8095, 500, 1701, 4500**), затем порты из профиля API (дедупликация). Старт **`warp_masque`** последовательно пробует порты (до 12) пока `Start` не успешен или ошибка не считается нечувствительной к смене порта (401, отсутствие Extended CONNECT/datagrams у сервера). **`api_first`** — только порядок портов из API (старое поведение «первый порт профиля» как база без приоритета 443). Логика списка: [`warp_dataplane_ports.go`](../../hiddify-core/hiddify-sing-box/protocol/masque/warp_dataplane_ports.go). |

Успешный порт записывается в кэш `hiddify_warp_masque_cache.json` (функция `RecordWarpMasqueDataplaneSuccess`), чтобы следующий запуск не гонял весь список.

Отладка «короткого» первого `read` на не-`UDPConn` пути QUIC: переменная окружения **`HIDDIFY_MASQUE_QUIC_HEX_SMALL_READS=1`** — hex первых байт входящей датаграммы ≤64 B в логе [`quic_dialer.go`](../../hiddify-core/hiddify-sing-box/protocol/masque/quic_dialer.go) (только для обёртки `connectedPacketConn`; если нижний dial отдаёт **`*net.UDPConn`**, QUIC идёт напрямую и quic-go может увеличивать receive buffer — предпочтительный путь).

**`server_token`:** то же поле верхнего уровня, что у `masque` — Bearer на dataplane (CONNECT-UDP/IP/stream), если edge или политика требуют авторизацию; не смешивать с `profile.auth_token` (control-plane).

**Цепочка (`hop_policy: chain` + `hops`):** для `warp_masque`, как и для generic `masque`, дозвон QUIC/H3 использует **`Server`/`server_port` entry-hop** из цепочки (первый hop с пустым `via` после `BuildChain`); результат bootstrap из API остаётся **fallback**, если нужно согласовать только host без явных hops.

### 7.3 Что считается «лишним» или запрещённым (клиент)

Общие запреты (любой `transport_mode` на клиенте):

| Условие | Ошибка / эффект |
|---------|-----------------|
| `tcp_transport` пусто или `auto` | ошибка валидации — нужен явный **`connect_stream`** |
| `tcp_transport: connect_ip` | ошибка — только TUN packet-plane, не этот ключ |
| `tcp_mode: masque_or_direct` без `fallback_policy: direct_explicit` | ошибка |
| `udp_timeout` или `workers` ≠ 0 | ошибка |
| `listen` / `listen_port` (как сервер) / `certificate` / `key` / `allow_private_targets` / port lists | ошибка в client mode |
| `quic_experimental.enabled` без `MASQUE_EXPERIMENTAL_QUIC=1` | ошибка |

### 7.4 Матрица плоскости: `transport_mode` и шаблоны / scope

| `transport_mode` (после нормализации) | Обязательно / допустимо в JSON | Запрещено / неприменимо |
|---------------------------------------|--------------------------------|-------------------------|
| **`connect_udp`** | `template_udp` (или дефолт buildTemplates); при непустом клиентском `template_udp` — подстроки `{target_host}` и `{target_port}` | **`template_ip` непустой** |
| **`connect_ip`** | `template_ip` (или дефолт); при непустом `template_udp` — ошибка; при scope — см. ниже | **`template_udp` непустой** |
| **`auto`** | Оба шаблона могут быть пустыми под дефолты **одновременно** в `buildTemplates`, но валидатор **не запрещает** оба непустыми: тогда оба должны удовлетворять правилам подстрок для TCP/UDP если заданы. На **`ListenPacket`** ветка как у `connect_udp` (пока не `connect_ip`). Для строгого «только одна плоскость UDP» в конфиге лучше выставить явно `connect_udp` или `connect_ip`. | `connect_ip_scope_*` без **`connect_ip`** |
| любой, не `connect_ip` | — | **`connect_ip_scope_target`** / **`connect_ip_scope_ipproto`** (если не нули) |
| **`connect_ip`** + scope | `template_ip` с переменными **`{target}`** и **`{ipproto}`** в URI (flow forwarding) | scope без этих placeholders |

Связка **TCP** с плоскостью:

- **`tcp_transport: connect_stream`** нужен **всегда** на клиенте (MASQUE TCP relay).
- **`tcp_mode` / `fallback_policy`** влияют на **dial TCP** (stream + опционально direct), **не** переключают `transport_mode` между CONNECT-UDP и CONNECT-IP ([комментарий в `validateMasqueOptions`](../../hiddify-core/hiddify-sing-box/protocol/masque/endpoint.go)).

### 7.5 Сервер: конфликты шаблонов

После подстановки дефолтов три path (**udp / ip / tcp**) должны быть **разными** и **не `'/'`**, иначе ошибка (`addServerTemplatePath` / collision). Непустые пользовательские шаблоны для UDP/TCP на сервере должны содержать **`{target_host}`** и **`{target_port}`**.

### 7.6 `omitempty` и обязательность

Тег **`omitempty`** в Go — только про сериализацию JSON. **Обязательность полей** определяется **`validateMasqueOptions`** и контекстом (клиент/сервер), а не тем, что ключ отсутствует в файле.

---

## 8. Клиент vs сервер: симметричные и асимметричные поля

**Должны совпадать по wire (политика связи):**

- Пути после разворачивания шаблонов (`template_udp`/`template_ip`/`template_tcp`): клиент генерирует URL, сервер регистрирует handler по тому же path shape.
- **`server_token`:** одно и то же поле на клиенте и сервере — при непустом значении клиент шлёт **`Authorization: Bearer <строка>`** на **CONNECT-UDP, CONNECT-IP и TCP CONNECT-stream**; сервер принимает Bearer (или `Proxy-Authorization`) на всех этих путях. Пустой токен на сервере = без app-level проверки (остаётся TLS).
- TLS: клиент доверяет сертификату сервера (`tls_server_name` / `insecure`); сервер предоставляет **`certificate`**/**`key`**.

**Только сервер:** `listen`, `listen_port`, certs, `allow_private_targets`, `allowed_target_ports`, `blocked_target_ports`; без client transport/tcp полей.

**Только клиент:** `server` (single hop), транспорт и TCP ключи и scope CONNECT-IP; без listen/cert/port policies.

**`DialerOptions`** встроены в общий тип на обе стороны: у сервера используются для **исходящих dial** релея, не как «приёмник MASQUE».

---

## 9. Минимальные примеры (иллюстрация подмножеств)

### Клиент CONNECT-IP + TCP stream

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

### Сервер

```json
{
  "endpoints": [
    {
      "type": "masque",
      "tag": "masque-server",
      "mode": "server",
      "listen": "0.0.0.0",
      "listen_port": 8443,
      "template_udp": "https://host:8443/masque/udp/{target_host}/{target_port}",
      "template_ip": "https://host:8443/masque/ip",
      "template_tcp": "https://host:8443/masque/tcp/{target_host}/{target_port}",
      "certificate": "/path/cert.pem",
      "key": "/path/key.pem"
    }
  ]
}
```

Шаблоны на реальном стенде см. [`experiments/router/stand/l3router/configs/`](../../experiments/router/stand/l3router/configs/) и [`README`](../../experiments/router/stand/l3router/configs/README.md).

---

## 10. Interop со сторонним MASQUE-сервером (не это дерево sing-box)

Чеклист перед ожиданием совместимости:

1. **URI и переменные** совпадают с тем, как peer парсит Extended CONNECT и path (другая разметка path → клиент получит отказ без «бага» sing-box).
2. **Поддерживаемые режимы на peer:** CONNECT-UDP (RFC 9298), CONNECT-IP (RFC 9484 — семантика маршрутов/addresses у каждого сервера своя), TCP stream если используете `template_tcp`.
3. **HTTP/3 + DATAGRAM** SETTINGS и QUIC ALPN должны поддерживаться обеими сторонами.
4. **Аутентификация:** согласованный `Authorization: Bearer` при `server_token`.
5. **CONNECT-IP:** сервер **sing-box** назначает маршруты/адреса своей политикой (`endpoint_server.go`); другой клиент должен понимать эти капсулы или ограничиться CONNECT-UDP.
6. **Продуктовое ограничение UDP-моста CONNECT-IPv4-only** при использовании `connect_ip` через мост к IPv4-сокету — не универсальный межвендорный контракт без проверки.
7. **`quic_experimental`:** включаете только синхронно с peer или для локальной диагностики.

На уровне wire vendored код: CONNECT-UDP — [`third_party/masque-go`](../../hiddify-core/hiddify-sing-box/third_party/masque-go); CONNECT-IP — [`third_party/connect-ip-go`](../../hiddify-core/hiddify-sing-box/third_party/connect-ip-go).

RFC/контракты CI см. также [`docs/masque/AGENT-RFC-CI-CONTRACTS.md`](AGENT-RFC-CI-CONTRACTS.md).
