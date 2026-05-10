# AGENTS — `experiments/router`

## Фокус

Единственный релевантный фокус в этой ветке: стенд MASQUE/WARP_MASQUE и доведение `connect_ip` до production-ready поведения.

## Единая точка входа тестов

Используется только Python entrypoint:

- `stand/l3router/masque_stand_runner.py`

Сценарии (см. `--help` у раннера):

- `udp`, `udp_matrix`, `udp_socks_associate`
- `tcp_stream`, `tcp_stream_auto`, `socks_tcp_ip_stack`
- `tcp_ip`, `tcp_ip_threshold`, `tcp_ip_scoped`, `tcp_ip_icmp`, `tcp_ip_iperf`
- `proxy_masque_matrix` (HTTP/SOCKS/mixed × connect_stream + connect_ip; только отдельно, не в очереди через запятую)
- `tun_rule_masque_perf`, `degrade_matrix`, пресеты `all`, `real`, `connect_ip_ingress_quick`, …

Режим по умолчанию: 10KB smoke.
`--stress` (500MB) включать только после стабильного non-stress green.

## Важные файлы стенда

- `stand/l3router/docker-compose.masque-e2e.yml`
- `stand/l3router/configs/masque-server.json`
- `stand/l3router/configs/masque-client.json`
- `stand/l3router/configs/masque-client-connect-ip.json`
- `stand/l3router/configs/masque-client-http.json`, `masque-client-mixed.json`, `masque-client-auto.json`, `masque-client-matrix-*.json` (подмножества покрываются раннером)
- `stand/l3router/masque_stand_runner.py`

## Семантика UDP: раннер vs SOCKS

- Сценарий **`udp`** в `masque_stand_runner.py` гоняет **TUN → CONNECT-IP** (`udp_tun_send.py`), а не SOCKS5 **UDP ASSOCIATE**.
- Цепочка **SOCKS5 UDP ASSOCIATE → CONNECT-UDP**: `python masque_stand_runner.py --scenario udp_socks_associate` (или матрица `proxy_masque_matrix`).

## Текущее состояние

Канон статуса strict bulk и smoke совпадает с корневым [`AGENTS.md`](../../AGENTS.md) репозитория: `udp` / `tcp_stream` / `tcp_ip` smoke и strict bulk `10/20/50MiB` для `tcp_ip` — ожидаемый зелёный контур.

## Следующий шаг

1) Любые правки dataplane подтверждать `masque_stand_runner.py --scenario all` и при необходимости отдельными bulk-вызовами `tcp_ip`.  
2) Держать observability в JSON-артефакте каждого `tcp_ip` прогона.  
3) Smoke JSON для CI (`runtime/smoke_*_latest.json`) пишет сам раннер после прогона; не удалять шаг валидации в CI.

## Примечание по `--scenario all` и bulk

При `--megabytes` > 1 раннер с `--scenario all` оставляет в матрице только **`tcp_ip`** (см. сообщение в stdout раннера). Для bulk по `udp` или `tcp_stream` используйте отдельный `--scenario`.