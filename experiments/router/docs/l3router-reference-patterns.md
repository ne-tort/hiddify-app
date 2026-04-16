# l3router — архив исследований (до ядра wg_allowedips)

**Устарело.** Актуальная архитектура и слои: [README.md](README.md), [router-architecture.md](router-architecture.md).

Этот файл сохраняет историческую идею смотреть на `bart`, `go-iptrie`, stride и т.д. для альтернативного FIB. Текущее ядро — адаптация `wireguard-go` `allowedips` в `common/l3router/allowedips_peer.go`; отдельные «classic/stride» backend’и удалены.
