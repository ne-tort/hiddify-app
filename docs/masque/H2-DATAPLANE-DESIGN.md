# MASQUE HTTP/2 dataplane (расширения)

Кратко для ревью архитектуры; нормативные детали — в RFC 8441 (Extended CONNECT на HTTP/2), RFC 9297 (Capsule Protocol, DATAGRAM capsule type 0x00), RFC 9298 (CONNECT-UDP).

## Реализовано

- Внешний транспорт **TLS + TCP + ALPN `h2`**, общий TCP dial через `buildMasqueTCPDialFunc` (detour/router sing-box).
- **CONNECT-UDP**: Extended CONNECT с `:protocol: connect-udp`, заголовок `Capsule-Protocol`, обмен UDP полезной нагрузкой через **DATAGRAM capsules** на потоке запроса (клиент пишет в тело запроса, читает из тела ответа).
- Политика слоя: **`http_layer`** (`h3` | `h2` | `auto`), **`http_layer_fallback`** (однократная смена H2↔H3 после ошибки из белого списка), **`http_layer_cache_ttl`** + процессный TTL-кэш ключа без секретов.
- Capability `Datagrams` отражает **QUIC DATAGRAM** и сбрасывается для активного H2 CONNECT-UDP.
- **CONNECT-stream** (`template_tcp`) на H2: `transport/masque/h2_connect_stream.go`, общий TCP+TLS пул с CONNECT-UDP (`ensureH2UDPTransport`), выбор слоя согласован с `currentUDPHTTPLayer()` (в т.ч. после H2↔H3 fallback на CONNECT-UDP).

## Не реализовано (фаза E)

- **CONNECT-IP** поверх HTTP/2: убрать зависимость от `ReceiveDatagram` там, где сейчас `connect-ip-go` заточен под HTTP/3; доставка IP PDU через capsules / единый слой чтения в `transport/masque`.
