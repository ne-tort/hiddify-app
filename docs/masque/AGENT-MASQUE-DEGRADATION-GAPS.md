# MASQUE / CONNECT-IP — риски деградации и пробелы (код + стенд)

Цель: единый список **наблюдаемых** и **потенциальных** причин потерь/таймаутов на Docker-стенде и в production-подобной нагрузке. Часть пунктов подтверждена прогонами `degrade_matrix` и triage по boundary; часть — гипотезы по коду и апстриму, требующие счётчиков/peer-split.

## 0) Статус мер (обновляемый)

- **[DONE] CONNECT-IP payload hard-cap (ядро, не раннер):** в `transport/masque/transport.go` введён и применён `connectIPUDPWriteHardCap=1152` для `initialPayload` и `maxUDPPayload` в `newConnectIPUDPPacketConn`. Это компенсирующая мера против oversize/MTU-ошибок на TX boundary при high-rate.
- **[DONE] CONNECT-IP local MTU retry (ядро, не раннер):** в `connectIPUDPPacketConn.WriteTo` добавлен ограниченный локальный retry-path на `DatagramTooLarge/EMSGSIZE` с `decreasePayloadCeiling("local_mtu_error")` и повторной отправкой уменьшенного chunk (до 3 попыток). Это снижает вероятность потерь при transient oversize на high-rate.
- **[DONE] HTTP/3 stream DATAGRAM очередь: headroom + структура (ядро, не раннер):** дефолт per-stream очереди `4096`, env `HIDDIFY_HTTP3_STREAM_DATAGRAM_QUEUE_LEN` (`128..65536`). Дополнительно очередь реализована как **фиксированное кольцо** (O(1) dequeue), без сдвига слайса на каждом `ReceiveDatagram` — снижение CPU/GC на bulk CONNECT-IP/CONNECT-UDP.
- **[DONE] Unknown-stream DATAGRAM race buffering (ядро, не раннер):** в `replace/quic-go-patched/http3/conn.go` добавлен bounded pending-buffer для DATAGRAM, пришедших до `TrackStream` (пер-stream `64`, global `4096`). Это снижает потери по setup race и уменьшает `unknownStreamDatagramDropTotal` на high-rate старте.
- **[DONE] Unknown-stream pending backlog anti-stall (ядро, не раннер):** в `replace/quic-go-patched/http3/conn.go` добавлен bounded eviction (`drop-oldest`) для per-stream/global pending очередей + очистка pending при lifecycle (`clearStream`). Это компенсирующая мера против залипания старых unknown stream-id и последующих ложных потерь новых DATAGRAM при high-rate.
- **[DONE] QUIC packet-packer ACK-vs-DATAGRAM budget arbitration (ядро, hot path):** в `replace/quic-go-patched/packet_packer.go` добавлен приоритет DATAGRAM при конфликте бюджета с ACK (`DATAGRAM` влезает без ACK, но не влезает вместе с ACK). Это убирает повторный defer головы очереди DATAGRAM под ACK-pressure и снижает риск TX-side no-progress без дополнительного drop. (Коммит `hiddify-core` `32724914` закрепил поведение + обновление `packet_packer_test`.)
- **[DONE] Read-side lock narrowing (ядро):** в `replace/quic-go-patched/http3/conn.go` реестр stream → **`RWMutex`**, горячий `receiveDatagrams` только **`RLock`**. В vendored `third_party/connect-ip-go/conn.go` снимки policy для каждого пакета под **`RLock`**. Цель: меньше контенции между приёмником DATAGRAM и редкими мутациями карты / маршрутов.
- **[DONE] CONNECT-IP OBS periodic_active sampling (ядро):** в `transport/masque/transport.go` после первого успешного epoch горячие вызовы `maybeEmitConnectIPActiveSnapshot` сэмплируются (**~1 из 128**): меньше `time.Now`/CAS на каждую датаграмму при неизменной частоте JSON-OBS (~1 Hz). Коммит `hiddify-core` `ac74034`.
- **[DONE] HTTP/3 per-stream DATAGRAM backlog (ядро):** дефолт очереди `state_tracking_stream` с **4096 → 8192** (`replace/quic-go-patched/http3/state_tracking_stream.go`, коммит `hiddify-core` `2508f5a0`) до отведения приложением `ReceiveDatagram`; ENV `HIDDIFY_HTTP3_STREAM_DATAGRAM_QUEUE_LEN` без изменений. Стендовое подтверждение `last_pass`/счётчиков — после рабочего compose (локально блокировался pull/buildx).
- **[IN PROGRESS] Стендовая матрица после hot-path правок:** полный `degrade_matrix` + deltas `http3_*_drop_total`, `quic_datagram_packer_oversize_drop_total`, `connect_ip_packet_write_fail_reason_total["mtu"]` и `last_pass/first_fail` против `connect_ip_udp_degrade_matrix.json` на свежем `hiddify-core` HEAD (**нужен рабочий** `docker compose` / registry).
- **[DONE] CONNECT-UDP unknown-stream startup race backlog (ядро, hot path):** в `replace/quic-go-patched/http3/conn.go` увеличен bounded pending backlog (`per-stream: 64 -> 512`, `global: 4096 -> 65536`) для DATAGRAM до `TrackStream`. Быстрый стенд-факт после правки: `udp` (10 MiB, `--udp-send-bps 25000000`) перешёл из `receiver_incomplete/timeout` в **PASS** (`bytes_received=bytes_expected`, `measured_loss_pct_approx=0`, `payload_hash_ok=true`, `stop_reason=none`, `throughput_target_met=true`).
- **[DONE] TCP-over-CONNECT-IP netstack egress double-copy (ядро, hot path):** в `transport/masque/netstack_adapter.go` `writePacketWithRetry` больше не делает `bytes.Clone` + буфер повторных попыток на каждый кадр из gVisor перед `connectip.Conn.WritePacket` — compose в connect-ip-go уже копирует и мутирует TTL на внутренней копии. Снижает CPU/аллокации на исходящем пути туннеля (коммит `hiddify-core` `1cf5a685`).

## 1) Зафиксированные сигналы со стенда (текущие результаты)

Условия: compose-стенд, `masque_stand_runner.py`, режим `degrade_matrix`, объём **10 MiB** на шаг, лестница TCP shaping **100 / 120 / 130 / 140 / 150 mbit/s** (`MASQUE_DEGRADE_TCP_IP_RATES`), UDP BPS синхронизированы по той же шкале. Артефакт: `experiments/router/stand/l3router/runtime/connect_ip_udp_degrade_matrix.json`.

- **CONNECT-IP (`tcp_ip_threshold`, `bulk_single_flow`):**
  - **100 / 120 / 130 mbit/s:** PASS, `loss_pct = 0`.
  - **140 mbit/s:** FAIL, небольшие потери (`loss_pct ≈ 0.0559%`), `stop_reason ≈ sink_udp_ingress_datagram_gap_no_udp_errors`.
  - **150 mbit/s:** FAIL, потери выше (`loss_pct ≈ 1.04%`), `stop_reason = budget_exceeded` (таймаут/бюджет раннера).
  - **Граница:** `last_pass = 130m`, `first_fail = 140m`.

- **Большой объём (отдельный прогон):** **150 MiB** при shaping **120 mbit/s** для CONNECT-IP — **небольшие потери** (`loss_pct ≈ 0.064%`), при **100 mbit/s** на том же объёме — без потерь. То есть деградация появляется и как **скорость выше порога**, и как **длительность/объём** на граничной скорости.

Интерпретация: симптомы на стыке **ingress sink (gap)** и **runner budget** при росте скорости; при большом файле на 120 mbit/s — «долгоиграющий» сценарий с тем же классом boundary-loss, без обязательного совпадения с лестницей 10 MiB по всем точкам.

## 2) Класс 1 — QUIC / HTTP3 DATAGRAM path (самый частый по triage)

**Наблюдаемость в коде (`transport/masque/transport.go`, снапшот):**

| Счётчик | Смысл |
|--------|--------|
| `http3_stream_datagram_queue_drop_total` | per-stream очередь HTTP/3 после маршрутизации — silent drop при переполнении |
| `http3_datagram_unknown_stream_drop_total` | DATAGRAM пришёл до известного stream mapping / неверный quarter stream id path |
| `quic_datagram_rcv_queue_drop_total` | conn-level receive queue QUIC |
| `quic_datagram_packer_oversize_drop_total` | packer: кадр не влез в остаток пакета (в т.ч. HOL/after rotate) |

**Поведение в патче quic-go:**

- **Очередь send + `Rotate`:** смягчение HOL одним шагом; при узкой очереди возможен остаточный HOL глубже второй позиции; `Rotate` меняет порядок в send-queue (для ненадёжных датаграмм обычно допустимо).
- **Blocking send-queue (большой лимит):** не drop, а backpressure и рост задержки — косвенно усиливает loss на приёмной стороне при burst.
- **Per-stream queue (лимит порядка 1024 в текущей ветке):** burst без достаточного чтения приложением → прямые потери полезной нагрузки туннеля.

**Внешние треки (для стратегии апгрейда, проверять актуальность):** quic-go issues про DATAGRAM throughput/очереди (например **#3766**, **#4471**), coalescing/oversize (**#4984**), PMTU на long-lived сессиях (**#3955**).

## 3) Класс 2 — PMTU / эффективный payload / `DatagramTooLarge`

- В мосте CONNECT-IP: PTB/ICMP, `datagramCeiling`, классификация `*quic.DatagramTooLargeError` / `EMSGSIZE` как **`mtu`** — параллельно с **packer oversize** как отдельной «вселенной» счётчиков. Риск: один симптом — два разных слоя без автоматической склейки в одном `stop_reason` на стенде.
- **connect-ip-go:** нюансы ICMP / too large (апстрим issue **#38** — держать в уме при обновлении зависимости).

## 4) Класс 3 — Границы приложения (parse-drop, deadline, частичная запись)

Из `transport.go` / `endpoint_server.go` / `netstack_adapter.go`:

- **Parse-drop на IPv4/UDP без ошибки вверх** — «тихие» потери на границе моста; серверный путь отражён в `connect_ip_server_parse_drop_total` при регистрации поставщика.
- **Read deadline:** проверка не обязательно внутри длинного внутреннего цикла — снаружи может выглядеть как stall / `budget_exceeded`, а не `DeadlineExceeded`.
- **WriteTo с chunking:** при ошибке после части успешных отправок семантика «сколько реально ушло» vs возврат ошибки может путать интерпретацию loss.
- **Netstack TCP-over-CONNECT-IP:** отдельные drop при переполнении outbound queue — релевантно для TCP-over-tun, не для чистого UDP harness.

## 5) Класс 4 — Окружение Windows / WSL2 / Docker Desktop

UDP/QUIC edge cases усиливаются; **каноничный** прогон стенда — WSL + Docker Desktop (см. `AGENTS.md`). Иначе сравнение `max_pass` / `first_fail` между машинами слабо переносимо.

## 6) Процесс triage — что ещё недостаточно жёстко

- **Peer-split обязателен:** любые выводы о «где режет» — только с `delta_client` / `delta_server` и согласованными снимками счётчиков.
- **Один `stop_reason`** часто схлопывает разные причины (sink gap vs QUIC queue vs budget); нужна корреляция с §2 счётчиками и с MTU/payload полями в runtime JSON.
- **Process-wide счётчики** quic/http3 не привязаны к сессии — для узкой диагностики может понадобиться расширение (за отдельные итерации, без маскировки fail).

## 7) Зависимости — политика обновления

Обновлять **согласованным набором**: `quic-go` (патч/вендор) + `connect-ip-go` + `masque-go`, после матрицы **`max_pass + next_boundary`**, не по одному модулю.

## 8) RFC-ориентиры

- **RFC 9297** — HTTP Datagrams (семантика ненадёжности, порядок не гарантирован).
- **RFC 9484** — CONNECT-IP поверх HTTP/3.

## 9) Приоритет следующих проверок (предложение)

1. На каждом fail лестницы: полный набор §2 + PTB/PMTU поля + peer-split.
2. Отдельно прогон **большой объём** на фиксированных **100m vs 120m** для воспроизводимости «долгоиграющей» границы.
3. При росте `quic_datagram_packer_oversize_drop_total` без rcv queue drops — фокус на TX/packet budget; и наоборот.

## 10) Быстрый смоук «10 MiB при ~200 Mb/s» и ориентир ~4%

Цель: один короткий прогон вместо полной лестницы, чтобы отслеживать регрессию против исторического класса отказов (~потери порядка нескольких процентов при высоком pacing — см. triage `budget_exceeded`/sink в §1 и §6).

### Команды `masque_stand_runner.py` (байт/с pacing)

- **CONNECT-UDP** (`scenario udp`): `python masque_stand_runner.py --scenario udp --megabytes 10 --udp-send-bps 25000000` (`200e6` бит/с делить на 8 ⇒ **25_000_000 байт/с**, как в `MASQUE_UDP_RATE_BPS` в отправляющем скрипте).
- **CONNECT-IP bulk** (`scenario tcp_ip`): передать **то же** `--udp-send-bps 25000000`. На **Windows** при `--udp-send-bps 0` включается `_win_host_tcp_ip_default_udp_send_bps()` (**4_000_000 байт/с**) и оно **перебивает** расчёт из `MASQUE_TCP_IP_RATE_LIMIT`; без явного CLI выше вы не проверяете именно 200 Mb/s, даже если экспортировали переменную окружения.

### Что фиксировать в артефакте / summary

- Для `tcp_ip`: `metrics.loss_pct`, `stop_reason`, `budget_exceeded`, строки compact OBS (HTTP3/QUIC), `bytes_received`/`bytes_expected`, `hash_ok`.
- Для `udp`: `measured_loss_pct_approx`, `throughput_target_met`, `throughput_mbps`.
- **Контроль к ~4%**: не менять пороги PASS; сохранять числа и сравнивать с прошлыми прогонами (например, фиксировать `loss_pct` и факт отклонения от прошлого «якорного» fail/пограничного уровня матрицы).

### Наблюдаемость при недокачке без «near-full» (связано с §6)

Если получена умеренная потеря (~4–5%) и при этом `budget_exceeded`, то `got >= 99% expected` может **не выполняться** → `_should_collect_sink_udp_diag` не включается → `sink_udp_diag` часто остаётся `null`. Расширение сбора диагностики **только** для наблюдаемости (например, при любом `ok: false` или при `got >= 90%`) не ослабляет гейты — см. обсуждение в коде раннера.
