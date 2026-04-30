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

- `udp` PASS через python runner.
- `tcp_stream` PASS через python runner.
- `tcp_ip` FAIL (dataplane bug в core, не runner).

## Следующий шаг

Довести `tcp_ip` до PASS без упрощений и без деградации `udp`/`tcp_stream`, затем зафиксировать стабильность серией повторных прогонов `--scenario all`.