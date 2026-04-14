# Router experiment workspace

Изолированная зона для экспериментов с L3 Router на базе архитектурных паттернов `wireguard-go` и обвязки `sing-box`.

## Состав

- `wireguard-go/` - клон `github.com/sagernet/wireguard-go` (локально изменяемый).
- `hiddify-sing-box/` - копия `hiddify-core/hiddify-sing-box` для независимых правок endpoint.
- `docs/` - архитектурные документы и проектные решения.
- `go.work` - рабочее пространство Go для локальной сборки двух модулей вместе.

## Локальная связка модулей

В `hiddify-sing-box/go.mod` подключение `wireguard-go` направлено на локальный путь:

- `replace github.com/sagernet/wireguard-go => ../wireguard-go`

Это гарантирует, что сборка `hiddify-sing-box` использует изменяемый код в `experiments/router/wireguard-go`.

## Быстрая проверка сборки

```powershell
cd experiments/router/hiddify-sing-box
go build ./cmd/sing-box
```

## Примечание по совместимости

Для сохранения совместимости с текущей веткой `hiddify-sing-box` в локальный `wireguard-go` добавлен пакет `hiddify/` из `hiddify-sing-box/replace/wireguard-go/hiddify`.
