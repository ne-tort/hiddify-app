# AGENTS — handoff: P0 CONNECT-IP TCP egress, P1 GUI

Основная задача в этом репозитории: закрыть разрыв в generic MASQUE server для профилей `transport_mode=connect_ip` + `tcp_transport=connect_ip`, где клиент отправляет TCP как IP-пакеты в CONNECT-IP packet-plane, а серверный путь не гарантирует выход TCP в реальный egress.

История ранних H2/H3/VPS экспериментов и WARP MASQUE: [docs/masque/AGENTS-MASQUE-H2-ARCHIVE.md](docs/masque/AGENTS-MASQUE-H2-ARCHIVE.md), [docs/masque/MASQUE-SINGBOX-CONFIG.md](docs/masque/MASQUE-SINGBOX-CONFIG.md).

---

## 1) Основная задача (P0)

Реализовать рабочий dataplane для CONNECT-IP TCP на generic server:

1. Клиентский `DialContext` в `hiddify-core/hiddify-sing-box/transport/masque/transport.go` (ветка `MasqueTCPTransportConnectIP`) должен стабильно завершать TCP handshake через server mode `masque`.
2. Серверный путь `hiddify-core/hiddify-sing-box/protocol/masque/endpoint_server.go` для `ipPath` не должен ограничиваться «туннельной» маршрутизацией без гарантированного TCP egress.
3. Политики безопасности обязательны: `allow_private_targets`, `allowed_target_ports`, `blocked_target_ports`.
4. Критерий готовности: локальный интеграционный тест без VPS зелёный.

---

## 2) Текущее архитектурное решение (S2)

Выбран путь S2 (server mini-forwarder):

- Реализация: `hiddify-core/hiddify-sing-box/transport/masque/connect_ip_tcp_server_forwarder.go`.
- Вход: IPv4/TCP сегменты из `connectip.Conn.ReadPacket`.
- Выход: `net.Dial` на `dstIP:dstPort`, обратные TCP сегменты через `connectip.Conn.WritePacket`.
- Подключение в сервере: `hiddify-core/hiddify-sing-box/protocol/masque/endpoint_server.go` через `TM.RunConnectIPTCPPacketPlaneForwarder(...)`.
- Router path (`RoutePacketConnectionEx`) зафиксирован как альтернативный S1, но в текущей итерации закрываем задачу S2.

---

## 3) Ключевые файлы P0

- `c:\Users\qwerty\git\hiddify-app\hiddify-core\hiddify-sing-box\transport\masque\transport.go` — клиентский `dialConnectIPTCP`, netstack path.
- `c:\Users\qwerty\git\hiddify-app\hiddify-core\hiddify-sing-box\transport\masque\netstack_adapter.go` — userspace TCP stack.
- `c:\Users\qwerty\git\hiddify-app\hiddify-core\hiddify-sing-box\protocol\masque\endpoint_server.go` — HTTP handlers `/masque/ip` и запуск forwarder.
- `c:\Users\qwerty\git\hiddify-app\hiddify-core\hiddify-sing-box\transport\masque\connect_ip_tcp_server_forwarder.go` — S2 dataplane.
- `c:\Users\qwerty\git\hiddify-app\hiddify-core\hiddify-sing-box\protocol\masque\connect_ip_tcp_forwarder_e2e_test.go` — локальный E2E для P0.
- `c:\Users\qwerty\git\hiddify-app\scripts\Generate-MasqueMultiVpsConfigs.ps1` — лаб-генератор профилей.

---

## 4) Критерии готовности P0

- Тест `TestMasqueConnectIPTCP_E2E_Local` зелёный и стабилен.
- Клиент `DialContext(..., "tcp", target)` через `connect_ip` устанавливает handshake и передаёт payload (`ping`/`pong`) через generic server без VPS.
- Политики `allowed_target_ports` / `blocked_target_ports` / `allow_private_targets` соблюдаются.
- Никаких секретов в логах/fixtures/доках.

---

## 5) Локальные проверки P0

```powershell
Set-Location "c:\Users\qwerty\git\hiddify-app\hiddify-core\hiddify-sing-box"
go test -tags with_masque ./protocol/masque -run TestMasqueConnectIPTCP_E2E_Local -count=1 -v
```

Дополнительно:

```powershell
Set-Location "c:\Users\qwerty\git\hiddify-app\hiddify-core"
go test ./v2/config -count=1
```

---

## 6) P1 (отложено): GUI `masque` / `warp_masque`

GUI-задача остаётся в бэклоге и не закрывает P0. При возврате к P1:

- Импорт/парсинг: `lib/features/profile/**`.
- Raw editor без потери полей: `lib/features/profile/details/**`.
- Build tags с `with_masque` для libcore.
- WARP settings page в первой итерации не менять.
