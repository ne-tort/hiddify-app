# AGENT Layer Source-of-Truth

Карта слоёв и границ ответственности для MASQUE.

## 1) Основная вертикаль

`route/config -> endpoint manager -> protocol/masque -> common/masque/runtime -> transport/masque -> third_party/connect-ip-go + quic-go patched`

## 2) Где искать проблемы

- Клиентский dataplane: `hiddify-core/hiddify-sing-box/protocol/masque/endpoint.go`
- Lifecycle/runtime: `hiddify-core/hiddify-sing-box/common/masque/runtime.go`
- QUIC/H3 и режимы: `hiddify-core/hiddify-sing-box/transport/masque/transport.go`
- Серверный relay/inbound: `hiddify-core/hiddify-sing-box/protocol/masque/endpoint_server.go`
- CONNECT-IP vendor: `hiddify-core/hiddify-sing-box/third_party/connect-ip-go`

## 3) Границы режимов (short)

- `tcp_stream`: TCP relay path.
- `connect_udp`: UDP tunnel path.
- `connect_ip`: packet-plane + connect-ip UDP bridge path.
- Не смешивать mode semantics между API путями.

## 4) Error/source truth

- Классификация ошибок и boundary mapping должны быть typed/deterministic.
- Источник истины по итоговому поведению: runtime artifacts + test output.

## 5) Practical rule

Если есть регресс:

1. Проверить, в каком слое boundary нарушен.
2. Править только этот слой и его ближайшие зависимости.
3. Подтвердить изменением контрактных тестов/артефактов.
