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
- `tcp_ip` smoke PASS (10KB, hash OK) через python runner.
- strict bulk `10/20/50MB` в TUN-only режиме пока FAIL (loss/hash), требуется доработка packet-plane.

## Следующий шаг

1) Закрыть bulk-gap в `stand/l3router/masque_stand_runner.py` + dataplane без упрощений и без shaping-by-default.  
2) Держать observability в JSON-артефакте каждого `tcp_ip` прогона.  
3) После каждого изменения обязательно прогонять `--scenario tcp_ip` и `--scenario all`.