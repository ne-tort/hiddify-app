# AGENT Test and Stand Runbook

Краткий runbook для go-тестов и docker-стенда.

## 1) Рабочие директории

- Ядро (`go test`, `go build`): `hiddify-core/hiddify-sing-box`
- Стенд/артефакты: `experiments/router/stand/l3router`

## 2) Базовые прогоны

- Smoke all: `python masque_stand_runner.py --scenario all`
- CONNECT-IP bulk: `python masque_stand_runner.py --scenario tcp_ip --megabytes <N>`
- UDP bulk: `python masque_stand_runner.py --scenario udp --megabytes <N> --udp-send-bps <BPS>`
- TCP stream bulk: `python masque_stand_runner.py --scenario tcp_stream --megabytes <N>`

## 3) Anti-hang

- Любой сценарий с timeout трактовать как FAIL.
- Сохранять `runtime/*.json` и разбирать `error_class`, `stop_reason`, `bytes_received/hash`.
- Не лечить fail только ростом budget/slack без анализа первопричины.

## 4) Наблюдения по текущей среде

- Burst без пейсинга может давать искажённый throughput в fail-path (долгий wait timeout).
- Для честной оценки UDP использовать `--udp-send-bps` и искать стабильную границу без потерь.

## 5) Performance checkpoints (latest)

- `connect_ip` начинает деградировать примерно после ~90 Mbit/s.
- `connect_udp` стабилен до ~70 Mbit/s, выше начинается срыв.
- `tcp_stream` 500 MiB проходил на ~602 Mbit/s.
