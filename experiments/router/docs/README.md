# Документация `experiments/router` / l3router

Единая точка входа: архитектура, инварианты, сравнение с `wg-go`, бенчмарки и сборка.

## Обязательные ссылки

| Документ | Содержание |
|----------|------------|
| [router-architecture.md](router-architecture.md) | Каноническое описание: слои, dataplane, инварианты `RouteID`/`PeerID`, KPI и матрица измерений |
| [../AGENTS.md](../AGENTS.md) | Оперативный контекст и команды прогона тестов/бенчмарков |
| [../../AGENTS.md](../../AGENTS.md) | Репозиторные правила и отчётность по CPU vs `wg-go` |
| [Интеграционный стенд](../stand/l3router/README.md) | Один вход: `python run.py` — сборка, деплой на VPS, Docker-клиенты, SMB 100 MiB |

## Кратко по слоям (sing-box)

- **`protocol/l3router`**: эндпоинт `type: l3router` (рядом с `protocol/wireguard`, `protocol/tailscale`) — сессии, `SessionKey`, очереди egress, `WritePacketBuffer`; привязка транспорта к `PeerID` один раз на соединение.
- **`common/l3router`**: переиспользуемое ядро — только `PeerID` + IP-пакет; FIB — trie как в `wireguard-go` [`allowedips`](https://github.com/WireGuard/wireguard-go/blob/master/device/allowedips.go), код в `allowedips_peer.go`; ACL — membership-trie (`prefix_matcher.go`), не путать с FIB.

## Сборка sing-box с l3router

Тег: `with_l3router` (включён в `Makefile`/`TAGS` сборки sing-box в этом дереве). Пример:

`cd experiments/router/hiddify-sing-box && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -tags with_gvisor,with_clash_api,with_utls,with_l3router -o sing-box-linux-amd64 ./cmd/sing-box`

## Бенчмарки (сравнение с `wg-go`)

Полные команды — в [../AGENTS.md](../AGENTS.md) (секция **Benchmark matrix**).

Синтетические профили транспорта (`plain_l3router_baseline`, `vless_reality_vision_synthetic`, `hy2_synthetic`, `tuic_synthetic`, `mieru_synthetic`) **не являются** wire-совместимыми с реальными протоколами; они моделируют накладные расходы AEAD/фрейминга для повторяемого сравнения с бенчмарками в `replace/wireguard-go/device`.

### Пример снимка (один прогон, `windows/amd64`)

Относительная дельта: `((l3router_ns/op - wg_ns/op) / wg_ns/op) * 100%`.

| Профиль (e2e single-thread) | l3router ns/op | wg-go ns/op | Δ |
|------------------------------|----------------|-------------|---|
| plain | 12.24 | 20.90 | −41% |
| vless | 1591 | 1975 | −19% |
| hy2 | 1742 | 2082 | −16% |
| tuic | 5680 | 6547 | −13% |
| mieru | 1755 | 2136 | −18% |

| Якорь | l3router | wg-go | Δ |
|-------|----------|-------|---|
| Lookup single-flow | 9.891 | 9.019 | +9.7% |
| ManyFlows parallel | 17.14 | 38.91 | −56% |

Цифры зависят от CPU/OS; для итераций фиксировать свои `x1`/`x10` на целевой машине.

## Устаревшие заметки (история)

- [l3router-wg-gap-analysis.md](l3router-wg-gap-analysis.md) — сжатый gap-анализ (обновлён под текущий код).
- [l3router-reference-patterns.md](l3router-reference-patterns.md) — архив исследований до ядра `wg_allowedips`.
- [l3router-compressed-backend-decision.md](l3router-compressed-backend-decision.md) — архивное решение по альтернативным FIB-backend’ам.
