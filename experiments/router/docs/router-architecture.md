# Router architecture (concept)

## Цель

`Router` - это отдельный L3-модуль для `sing-box`, который принимает raw IP-пакеты от любых multi-user протоколов и маршрутизирует их между логическими маршрутами (`Route`) без криптосемантики WireGuard.

Ключевая идея: сохранить профессиональную архитектуру `wireguard-go` (разделение ответственности, очереди, табличная маршрутизация, peer-like модель), но убрать зависимость от Noise/криптоидентификации.

## Что должно получиться

- Новый endpoint в `sing-box`, работающий с модулем `Router`.
- `Route` как аналог `Peer`, но это объект маршрутизации, а не криптообъект.
- ACL в духе WireGuard (`AllowedIPs`) сохраняется и остается базовой моделью доступа.
- Универсальность по транспорту: endpoint получает user/session-контекст от любого поддерживаемого multi-user inbound протокола.

## Концептуальная модель

### 1) Объект Route

`Route` описывает логический маршрут:

- `route_id` - идентификатор маршрута;
- `owner` - логический владелец (user/group/policy-id);
- `allowed_src` - разрешенные source prefix для ingress-пакетов;
- `allowed_dst` - разрешенные destination prefix (опционально отдельной policy);
- `exported_prefixes` - префиксы, которые публикует этот маршрут как точка выхода;
- `session_binding` - активная сессия/канал доставки в runtime.

Смысл: "создал Route -> появился маршрут и права", как у peer в WG, но без ключей.

### 2) ACL (Allowed IPs)

ACL в Router остается центральной:

- ingress anti-spoof: пакет от маршрута допустим только если `src` входит в `allowed_src`;
- egress policy: `dst` должен соответствовать разрешенному пути через FIB/policy;
- default deny: все неразрешенное отбрасывается.

Это повторяет идею WG `AllowedIPs` на уровне L3, но источник identity - не криптография, а контекст user/session от `sing-box`.

### 3) Таблица маршрутов (FIB)

FIB строится по `exported_prefixes` и выбирает `egress Route` по longest-prefix match.

Pipeline:

1. endpoint передает в Router `(packet, session_context)`;
2. Router определяет `ingress Route` по runtime-привязке сессии;
3. проверяет ACL ingress (`allowed_src`);
4. выполняет FIB lookup по `dst`;
5. проверяет policy ingress->egress (если включена межгрупповая политика);
6. возвращает решение доставки в целевую egress-сессию.

## Что убрать/обойти из wireguard-go

## Сохраняем

- модель "peer-like объект + allowed IPs";
- табличную маршрутизацию и очереди обработки пакетов;
- разделение control-plane (конфиг, менеджмент маршрутов) и data-plane (форвард пакетов).

## Удаляем или отключаем

- Noise handshake;
- криптоидентификацию peer через ключи;
- шифрование/дешифрование как обязательный этап data-plane;
- зависимости жизненного цикла маршрута от криптосостояния.

## Заменяем

- идентификацию peer -> идентификация через user/session-контекст, который уже предоставлен `sing-box` protocol layer;
- криптопривязку -> policy-привязка `session -> Route`.

## Router как отдельный проект

Минимальные контуры отдельного проекта:

- runtime registry маршрутов и их session-binding;
- FIB + ACL engine;
- API/конфиг для управления `Route`, `AllowedIPs`, группами и политиками;
- стабильный интерфейс для интеграции в endpoint `sing-box`.

Формат конфигурации: JSON-first (ini не обязателен).

## Router внутри sing-box endpoint

Endpoint-обвязка в `sing-box` должна:

- получать пакет и user/session-контекст от inbound-протокола;
- передавать это в Router API;
- по результату lookup отправлять пакет в соответствующую egress-сессию/канал;
- обеспечивать multi-user изоляцию на уровне policy context.

Важно: endpoint не реализует криптологику маршрутов, а только мостит protocol session context в Router data-plane.

## Роли и границы ответственности

- `sing-box protocol layer`: аутентификация пользователя, lifecycle сессии, транспорт.
- `Router`: L3-маршрутизация, `AllowedIPs` ACL, выбор egress маршрута, anti-spoof.
- `config/control-plane`: создание/обновление Route и политик (users, groups, prefixes).
