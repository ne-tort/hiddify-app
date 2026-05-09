# AGENTS — WARP MASQUE (sing-box endpoint)

## 1. Задача

Доработать **`type: warp_masque`** в `hiddify-core/hiddify-sing-box`: находить и настраивать **MASQUE dataplane** на реальном Cloudflare WARP (**HTTP/3 + CONNECT-UDP / CONNECT-IP / CONNECT-stream**), добиться **рабочего исходящего трафика** через этот канал на эталонном стенде.

Legacy **`warp` (WireGuard)** — не цель итерации; при затрагивании общего кода профиля/регистрации **`GetWarpProfile`** не регрессить (см. `hiddify-core/docs/masque-warp-architecture.md`). Общий транспорт MASQUE без WARP-смягчений поддерживать в `transport/masque` и связанном коде без поломки consumers.

Общее ядро: `transport/masque`, `third_party/*-go`. Точки WARP bootstrap: [`protocol/masque/endpoint_warp_masque.go`](hiddify-core/hiddify-sing-box/protocol/masque/endpoint_warp_masque.go), [`protocol/masque/warp_control_adapter.go`](hiddify-core/hiddify-sing-box/protocol/masque/warp_control_adapter.go).

## 2. Гипотезы (не факты до проверки)

Ниже — рабочие предположения для triage и кода; **не** смешивать с нормативными требованиями Cloudflare, если они не найдены и подтверждены во внешних источниках:

- Гипотеза: профиль/reg API отдаёт **host:port** под **tunnel/classic WG** и для **RFC MASQUE** нужен другой dataplane-хост или путь (официально MASQUE часто описывается как HTTPS-подобный сценарий, не WG UDP edge).
- Гипотеза: QUIC «оживает», но узел не объявляет **Extended CONNECT / H3 DATAGRAMS** как ожидают [`connect-ip-go`](hiddify-core/hiddify-sing-box/third_party/connect-ip-go) / [`masque-go`](hiddify-core/hiddify-sing-box/third_party/masque-go).
- Гипотеза: для CONNECT-* нужен **отдельный Bearer** (`server_token`), недоступный из того же профиля, что поднимает WG; либо наоборот — достаточно корректного edge + заголовков.
- Гипотеза: таймауты и PMTU/UDP на VPS маскируют отсутствие правильной цели dataplane — длинные ожидания не должны использоваться как «решение».

**Фактические контракты edge WARP под MASQUE** для стороннего клиента в репозитории полностью не заданы — искать во внешних источниках (RFC ниже, документация и записи Cloudflare, community‑эталоны), **сверять с нашей реализацией** и при необходимости править код **архитектурно** (например probe/discovery SETTINGS до полного runtime, конфигурируемые caps, альтернативные candidates).

Ссылки на нормативку транспортного контраста: [RFC 9220](https://www.rfc-editor.org/rfc/rfc9220.html), [RFC 9298](https://www.rfc-editor.org/rfc/rfc9298), [RFC 9484](https://www.rfc-editor.org/rfc/rfc9484.html).

## 3. Инварианты

- Цикл: гипотеза → код/конфиг → стенд → короткий артефакт (лог/metadata без секретов) → следующий шаг.
- Секреты не коммитить; описывать только способ подстановки через env/secrets.
- Не маскировать ошибки; triage: **control-plane** (`warp_control_adapter`, профиль/API) против **data-plane** (QUIC / TLS / H3 SETTINGS / CONNECT). В логах старта клиентского рантайма смотрите короткий ключ **`class=`** из [`ClassifyMasqueFailure`](hiddify-core/hiddify-sing-box/protocol/masque/errors_classify.go) (`h3_extended_connect`, `h3_datagrams`, `quic_tls`, `connect_http_auth`, …) — одинаково для `masque` и `warp_masque`.
- Изменения ядра в **`hiddify-core`**; при необходимости зафиксировать SHA сабмодуля в **`hiddify-app`**.
- **Таймауты:** не допускать «подвисания» прототипом и автотестами. Ориентиры: проверочный HTTP (curl и аналог) **`--max-time` ≤ ~20 с**; `connect_timeout` endpoint — **до ~20–30 с** на итерации, без минутных значений и без неограниченного ожидания в скриптах. Детали примеров — в README стенда (согласовать с этим файлом).

## 4. Приоритет источников истины

При споре о том, как **мы** хотим себя вести в коде:  
1) код `protocol/masque/*`, `transport/masque`, `option/masque.go`; 2) `hiddify-core/docs/masque-warp-architecture.md`; 3) `IDEAL-MASQUE-ARCHITECTURE.md`; 4) `docs/masque/MASQUE-SINGBOX-CONFIG.md`; 5) этот файл.

При споре о том, **как себя ведёт реальный edge / что «канонично» для живого WARP без публичного SLA третьим лицам** — первичны **внешние** источники (RFC, CF, наблюдаемые эталоны), затем проверка в коде и на стенде.

## 5. Учётные данные (два слоя)

| Слой | Назначение | Конфиг / код |
|------|------------|----------------|
| Control-plane | Куда звонить: endpoint из профиля Cloudflare через API (как сегодня для bootstrap) | `profile.*`; [`warp_control_adapter.go`](hiddify-core/hiddify-sing-box/protocol/masque/warp_control_adapter.go) → `wireguard.GetWarpProfile` |
| Data-plane MASQUE | Bearer на CONNECT-* при необходимости | `server_token` |
| Переопределение порта QUIC к edge | После разрешения host из API | `profile.dataplane_port` (см. [`MASQUE-SINGBOX-CONFIG.md`](docs/masque/MASQUE-SINGBOX-CONFIG.md) §7.2.1) |

**MASQUE Bearer ≠ WG-ключ.** Bootstrap профилем Bearer для CONNECT автоматически не гарантируется.

## 6. Ссылки для чтения и стенда

- Топология: [`hiddify-core/docs/masque-warp-architecture.md`](hiddify-core/docs/masque-warp-architecture.md)
- Поля JSON: [`docs/masque/MASQUE-SINGBOX-CONFIG.md`](docs/masque/MASQUE-SINGBOX-CONFIG.md)
- Архитектура MASQUE: [`IDEAL-MASQUE-ARCHITECTURE.md`](IDEAL-MASQUE-ARCHITECTURE.md)
- **Стенд и smoke на VPS (прототип, правится по необходимости):** [`experiments/router/stand/l3router/README-warp-masque-live-server.md`](experiments/router/stand/l3router/README-warp-masque-live-server.md), compose [`docker-compose.warp-masque-live.server.yml`](experiments/router/stand/l3router/docker-compose.warp-masque-live.server.yml), шаблон [`configs/warp-masque-live.server.docker.json`](experiments/router/stand/l3router/configs/warp-masque-live.server.docker.json)
- Прогоны l3router/CI-контекст: [`docs/masque/AGENT-TEST-AND-STAND-RUNBOOK.md`](docs/masque/AGENT-TEST-AND-STAND-RUNBOOK.md); perf/connect-ip: [`hiddify-core/docs/masque-perf-gates.md`](hiddify-core/docs/masque-perf-gates.md), [`docs/masque/AGENT-LAYER-SOURCE-OF-TRUTH.md`](docs/masque/AGENT-LAYER-SOURCE-OF-TRUTH.md)

## 7. Как проверять (этот цикл ожиданий от агента)

- **Стенд:** Docker на сервере **163.5.180.181** по инструкции README стенда; при изменении кода — **пересборка linux/amd64**, деплой бинаря/образа, **сверка SHA256** артефакта с локальной сборкой (команды не дублировать здесь — в README/`upload_warp_masque_stand.ps1`).
- **Успех итерации:** `warp_masque` стартует без фатала, есть **измеримый исход** (например trace/curl через SOCKS или узкие маршруты — как описано для выбранного JSON в README стенда). Прототип стенда при нерелевантности править вместе с кодом.

## 8. Definition of Done (warp_masque)

- На выбранном профиле (consumer / ZT) документируемые поля `profile.*`, роль **`server_token`**, способ смокей без секретов.
- Хотя бы один **повторяемый** путь до трафика на реальном WARP или явно зафиксированное продуктовое ограничение с обоснованием.
- Локально: нет регресса затронутого generic MASQUE / legacy warp там, где шарится код.

## 9. Текущая итерация (перезаписывать)

| Поле | Значение |
|------|----------|
| Дата | _заполнить_ |
| `hiddify-core` HEAD/ветка | _заполнить_ |
| Профиль | consumer \| zero_trust \| both |
| Результат live | _этап сбоя / успех ; артефакт без секретов_ |
| Токены | control достаточен \| нужен Bearer \| уточнить |

Опциональный длинный runbook можно вести отдельно в `docs/masque/` по мере стабилизации; держать AGENTS коротким.

## 10. Задачи вперёд

1. Сверить с внешними источниками ожидания edge: SETTINGS (Extended CONNECT, datagrams), URI шаблоны, заголовки — против `transport/masque` и [`third_party`](hiddify-core/hiddify-sing-box/third_party).
2. При подтверждении необходимости — **discovery/probe кандидатов** dataplane до полного [`CM.NewRuntime`](hiddify-core/hiddify-sing-box/protocol/masque/endpoint_warp_masque.go) (QUIC + приём SETTINGS, затем CONNECT).
3. Явная стратегия **`transport_mode`**: smoke **`connect_udp`** vs **`connect_ip`**, документировать выбор без путаницы с WG-портом из профиля; при несовпадении попробовать **`profile.dataplane_port`** (например 443).
4. Прояснить и автоматизировать где возможно **Bearer** / политики org для CONNECT.
5. Поддерживать актуальность прототипа Docker-стенда и таймаутов README под §3.
6. При правках общего transport — не сломать `masque-gates`/локальные CI-смоки (без живых секретов в пайплайне).
