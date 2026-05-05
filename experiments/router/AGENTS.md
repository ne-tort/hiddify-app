# AGENTS — `experiments/router`

## Фокус

Единственный релевантный фокус в этой ветке: стенд MASQUE/WARP_MASQUE и доведение `connect_ip` до production-ready поведения.

## Единая точка входа тестов

Используется только Python entrypoint:

- `stand/l3router/masque_stand_runner.py`

Сценарии:

- `udp`
- `tcp_stream`
- `tcp_ip`
- `all`

Режим по умолчанию: 10KB smoke.
`--stress` (500MB) включать только после стабильного non-stress green.

## Важные файлы стенда

- `stand/l3router/docker-compose.masque-e2e.yml`
- `stand/l3router/configs/masque-server.json`
- `stand/l3router/configs/masque-client.json`
- `stand/l3router/configs/masque-client-connect-ip.json`
- `stand/l3router/masque_stand_runner.py`

## Текущее состояние

Канон статуса strict bulk и smoke совпадает с корневым [`AGENTS.md`](../../AGENTS.md) репозитория: `udp` / `tcp_stream` / `tcp_ip` smoke и strict bulk `10/20/50MiB` для `tcp_ip` — ожидаемый зелёный контур.

## Следующий шаг

1) Любые правки dataplane подтверждать `masque_stand_runner.py --scenario all` и при необходимости отдельными bulk-вызовами `tcp_ip`.  
2) Держать observability в JSON-артефакте каждого `tcp_ip` прогона.  
3) Smoke JSON для CI (`runtime/smoke_*_latest.json`) пишет сам раннер после прогона; не удалять шаг валидации в CI.

## Примечание по `--scenario all` и bulk

При `--megabytes` > 1 раннер с `--scenario all` оставляет в матрице только **`tcp_ip`** (см. сообщение в stdout раннера). Для bulk по `udp` или `tcp_stream` используйте отдельный `--scenario`.