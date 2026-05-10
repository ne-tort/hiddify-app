# AGENT Test and Stand Runbook

Краткий runbook для go-тестов и docker-стенда.

## 1) Рабочие директории

- Ядро (`go test`, `go build`): `hiddify-core/hiddify-sing-box`
- Стенд/артефакты: `experiments/router/stand/l3router`

## 2) Базовые прогоны

- Полный стенд по умолчанию: `python masque_stand_runner.py` → **`--scenario all`**, 100 MiB по очереди: `udp`, `tcp_stream`, `tcp_ip`, `tcp_ip_icmp`, `udp_socks_associate` (прогресс: `[stand i/N]`, `[stand heartbeat]`, `[stand wait]`).
- Один сценарий: `python masque_stand_runner.py --scenario tcp_stream` (и т.д.).
- CONNECT-IP bulk: `python masque_stand_runner.py --scenario tcp_ip --megabytes <N>`
- UDP bulk: `python masque_stand_runner.py --scenario udp --megabytes <N> --udp-send-bps <BPS>`
- TCP stream bulk: `python masque_stand_runner.py --scenario tcp_stream --megabytes <N>`
- **`transport_mode: auto` + SOCKS TCP:** `python masque_stand_runner.py --scenario tcp_stream_auto` (клиент [`configs/masque-client-auto.json`](../../experiments/router/stand/l3router/configs/masque-client-auto.json); семантика UDP для `auto` — как `connect_udp` в `ListenPacket`, см. §6).
- **HTTP / SOCKS / mixed × connect_stream + connect_ip (матрица):** `python masque_stand_runner.py --scenario proxy_masque_matrix` — один процесс, конфиги [`masque-client-matrix-connect-stream.json`](../../experiments/router/stand/l3router/configs/masque-client-matrix-connect-stream.json) и [`masque-client-matrix-connect-ip.json`](../../experiments/router/stand/l3router/configs/masque-client-matrix-connect-ip.json); TCP измеряется через [`scripts/masque_tcp_throughput_measure.py`](../../experiments/router/stand/l3router/scripts/masque_tcp_throughput_measure.py) внутри раннера.
- **SOCKS5 UDP ASSOCIATE → CONNECT-UDP** (не через TUN): `python masque_stand_runner.py --scenario udp_socks_associate` (probe шлёт нулевой payload **чанками**; размер задаётся как у остальных сценариев — по умолчанию 100 MiB). Это **не** сценарий `udp` (там TUN + CONNECT-IP, см. §6).

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

## 6) `transport_mode` и входы (tun / socks / http / mixed)

| `transport_mode` | Ветка `ListenPacket` (UDP к host:port) | TUN / сырой IP (`connect_ip` сессия) | TCP через MASQUE stream (`tcp_transport: connect_stream`) |
|------------------|----------------------------------------|--------------------------------------|------------------------------------------------------------|
| `connect_ip` | Через инкапсуляцию в CONNECT-IP (`newConnectIPUDPPacketConn`) | Да (`OpenIPSession`, пакеты IP) | Задаётся отдельно (`tcp_transport` + netstack при `connect_ip`) |
| `connect_udp` | CONNECT-UDP / datagram | Нет (`template_ip` запрещён) | Да (CONNECT-stream) |
| `auto` | Как **`connect_udp`** (пока режим не `connect_ip`) | Нет, пока явно не `connect_ip` | Да |

Inbound (**socks**, **http**, **mixed**, **tun**) не меняет эту таблицу: он только задаёт, как трафик попадает в маршрут с `final` на tag endpoint.

**Раннер `masque_stand_runner.py --scenario udp`:** идёт через **TUN** и отправку IP/UDP в CONNECT-IP (`udp_tun_send.py`), а **не** через SOCKS5 UDP ASSOCIATE. Для последнего: `--scenario udp_socks_associate` (или строки UDP внутри `--scenario proxy_masque_matrix`).

## 7) Замер TCP goodput (HTTP / SOCKS / mixed)

Канонический прогон без отдельных shell: **`python masque_stand_runner.py --scenario proxy_masque_matrix`** (по умолчанию **100 MiB** на TCP и UDP; иначе `--megabytes` / `--stress`). Для каждой строки TCP раннер ждёт ровный объём на приёмнике и сравнивает **SHA256** с эталоном нулевого payload того же размера (логика совпадает с [`masque_tcp_throughput_measure.py`](../../experiments/router/stand/l3router/scripts/masque_tcp_throughput_measure.py)).

Опционально для ручного goodput-лога (TSV `scenario\tbytes\tsec\tpayload_mbps`) можно по-прежнему вызывать `masque_tcp_throughput_measure.py` внутри контейнера после `compose up` — переменные: `MASQUE_BENCH_SCENARIO`, `MASQUE_BENCH_MODE` (`http`/`socks`), `MASQUE_PROXY_*`, `MASQUE_DST_*`, `MASQUE_TCPSEND_BYTES`, `MASQUE_BENCH_CONNECT_TIMEOUT_SEC`, `MASQUE_BENCH_DRAIN_SLEEP_SEC`.

Конфиг **только benchmark-inbounds** (без фазы `connect_ip`): [`configs/masque-client-benchmark.json`](../../experiments/router/stand/l3router/configs/masque-client-benchmark.json) — для узких ручных замеров; матрица раннера использует `masque-client-matrix-*.json`.
