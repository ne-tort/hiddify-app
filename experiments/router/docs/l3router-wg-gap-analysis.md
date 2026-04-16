# l3router vs wg-go — разрыв по hot path

Актуальное описание слоёв и измерений: [README.md](README.md), [router-architecture.md](router-architecture.md).

## Что сопоставляется

- **wg-go:** `device/allowedips.go` (LPM до пира).
- **l3router:** `common/l3router/allowedips_peer.go` + optional packet filter (`prefix_matcher.go`) + parse (`packet_parse.go`).

## Источники различий

1. У l3router при **`packet_filter: true`** на пакет добавляются проверки по `filter_source_ips` / опционально `filter_destination_ips` до LPM.
2. У l3router путь **`packet_filter: false`** сведён к `parse dst → LPM → no-loop`.
3. Слой `protocol/l3router` (сессии, очереди) отсутствует в wg-go — это не gap engine, а интеграция с sing-box.

## Стратегия выравнивания

- Держать путь без packet filter узким; политику инжектить с control path.
- Сохранять контракт `Engine` и packetization sing-box.
- Один FIB-путь: trie в духе `wireguard-go`, без альтернативных backend’ов в dataplane.

## Безопасность (не ослаблять)

- Anti-spoof при включённом packet filter, no-loop, LPM и причины drop остаются согласованными с тестами.
