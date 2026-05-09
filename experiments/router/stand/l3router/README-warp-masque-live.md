# Docker: live `warp_masque` (Cloudflare WARP) + DNS

Один контейнер **без** локального `masque-server-core`: sing-box поднимает endpoint **`warp_masque`**, резолвит edge через Cloudflare device API, затем строит QUIC/MASQUE к реальному хосту WARP.

## Требования

- **Linux** (или **WSL2 + Docker Desktop** на Windows): нужны `NET_ADMIN` и `/dev/net/tun`.
- Собранный бинарник **`artifacts/sing-box-linux-amd64`** в каталоге `experiments/router/stand/l3router/` (тот же артефакт, что для `docker-compose.masque-e2e.yml`). Сборка — из `hiddify-core/hiddify-sing-box` под `GOOS=linux GOARCH=amd64`.
- Учётные данные WARP в JSON (см. ниже). **Не коммитьте** секреты.

## DNS (sing-box и ОС)

1. **Docker `dns:`** в [docker-compose.warp-masque-live.yml](docker-compose.warp-masque-live.yml) — резолв на стороне контейнера до/вне sing-box (bootstrap `api.cloudflareclient.com`, системный резолв Go при `GetWarpProfile`).

2. **Модуль `dns` в конфиге** — два UDP upstream (`bootstrap-dns`, `public-dns`) с **`detour: direct`**, чтобы запросы к резолверу **не** уходили в туннель `warp_masque` (избегаем петли).

3. **`domain_resolver` на endpoint** — указывает тег транспорта DNS (`bootstrap-dns`); через него sing-box резолвит **доменное имя MASQUE-сервера** после bootstrap (см. `dialer.New` / `DomainResolver` в ядре).

4. **`route.default_domain_resolver`** — тот же тег для остальных доменных дайлов без явного `domain_resolver`.

5. **`profile.detour: direct`** — control-plane Cloudflare (**регистрация / `GET reg/{id}`**) идёт через outbound `direct`, не через сам `warp_masque`.

## Учётные данные (`profile`)

Файл по умолчанию: [configs/warp-masque-live.json](configs/warp-masque-live.json) (`compatibility: consumer`, пустые `license` / `private_key` — sing-box может создать **новое** анонимное устройство; для постоянного профиля заполните поля или скопируйте файл).

Рекомендуется рабочая копия с секретами:

```bash
cp configs/warp-masque-live.json configs/warp-masque-live.local.json
# отредактируйте profile.* ; затем:
export WARP_MASQUE_CONFIG=./configs/warp-masque-live.local.json
```

Режимы (см. `validateWarpMasqueOptions` в `protocol/masque/endpoint_warp_masque.go`):

| `profile.compatibility` | Что задать |
|-------------------------|------------|
| `consumer` (по умолчанию) | `license` и/или `private_key`; не задавайте `auth_token`/`id` |
| `zero_trust` | `auth_token` + `id` |
| `both` | сначала ZT, при ошибке — consumer |

**`server_token`** (поле верхнего уровня MASQUE) — это **Bearer для CONNECT-UDP/IP/stream** к MASQUE-серверу, не токен Cloudflare API. Для публичного WARP edge обычно оставляют пустым, пока не потребуется явная авторизация на стороне сервера.

**`profile.dataplane_port`** — необязательное переопределение **UDP/QUIC-порта** после ответа device API (если в кэше/`wgcf` виден порт в духе WG, а MASQUE ожидается на другом порту, часто **443**). Поле и слои `detour` см. [`docs/masque/MASQUE-SINGBOX-CONFIG.md`](../../../../docs/masque/MASQUE-SINGBOX-CONFIG.md) §7.2.1.

## Запуск

Скопируйте при необходимости [`.env.example`](.env.example) в `.env` и задайте переменные; либо экспортируйте `WARP_MASQUE_CONFIG` в shell перед `compose`.

Удобная обёртка: [scripts/warp_masque_live.sh](scripts/warp_masque_live.sh) (`up` / `down` / `logs` / `smoke` / `check`).

```bash
cd experiments/router/stand/l3router
# при необходимости: export WARP_MASQUE_CONFIG=./configs/warp-masque-live.local.json
docker compose -f docker-compose.warp-masque-live.yml build
docker compose -f docker-compose.warp-masque-live.yml up -d
docker compose -f docker-compose.warp-masque-live.yml logs -f sing-box-warp-masque-live
```

Проверка конфигурации (на хосте с Linux-бинарником sing-box):

```bash
sing-box check -c configs/warp-masque-live.json
```

## Smoke: «интернет» через sing-box

Внутри контейнера SOCKS-inbound слушает `127.0.0.1:1080` и уходит в `route.final` → `warp-masque-live`:

```bash
docker exec sing-box-warp-masque-live curl -sS --max-time 20 \
  --proxy socks5h://127.0.0.1:1080 https://1.1.1.1/cdn-cgi/trace
```

Ожидается ответ Cloudflare (строка `warp=on` или аналог для вашего аккаунта).

Останов:

```bash
docker compose -f docker-compose.warp-masque-live.yml down
```

## Диагностика

- Если в логах долго **`warp_masque startup in progress`** / мониторинг **`not ready`**, включите **`log.level: debug`** в копии конфига и перезапустите контейнер.
- **`connect_timeout`** задаёт базовый дедлайн дозвона; полный старт `warp_masque` ограничен **min/max(2×connect_timeout, 12s..45s)** в коде. Для смокей на стенде держите **короткие** значения порядка **15–20 с** (не минутные) — см. корневой **`AGENTS.md`**. Healthy edge обычно успевает за секунды; долгое зависание — не лечится увеличением таймаута без triage сети/edge.
- Если **`api.cloudflareclient.com`** с хоста недоступен, но профиль уже получен на другой машине: положите **`license` + `private_key`** в [configs/warp-masque-live.local.json](configs/warp-masque-live.local.json) и добавьте кэш edge (ключ = `compat:consumer|license:<ваш>|detour:<profile.detour>`, значения `server`/`port` из `wgcf-profile.conf`). Установите переменную **`WARP_MASQUE_WARP_CACHE`** на этот файл; по умолчанию Compose монтирует пустой [configs/warp-masque-warp-cache.empty.json](configs/warp-masque-warp-cache.empty.json). Дальше всё равно нужен **UDP/QUIC** до этого edge с вашей сети.
- В логах строка **`runtime start failed class=<ключ>`** (`ClassifyMasqueFailure` в коде) помогает отличить отсутствие Extended CONNECT / datagrams / TLS / 401 на CONNECT от чистого таймаута.

## Вспомогательный скрипт

[scripts/warp_masque_live.sh](scripts/warp_masque_live.sh) — `up`, `down`, `logs`, `check` (если есть `sing-box` в `PATH`), `smoke`.

## Отличие от `docker-compose.masque-e2e.yml`

| masque-e2e | warp-masque-live |
|------------|------------------|
| Локальный `masque-server-core` + клиент | Только клиент к **реальному** CF edge |
| `type: masque`, фиксированный `server` | `type: warp_masque`, bootstrap + API |

## TUN: только нужные префиксы

В [configs/warp-masque-live.json](configs/warp-masque-live.json) включены **`auto_route` + `route_address`** (синтаксис [sing-box inbound/tun](https://sing-box.sagernet.org/configuration/inbound/tun/)): в таблицу маршрутизации попадают **только перечисленные CIDR** (лаб‑диапазон **`198.18.0.0/15`** и блоки **`1.1.1.0/24`** / **`1.0.0.0/24`** для смоков к Cloudflare), а не «весь интернет». На Linux включён **`auto_redirect`** как рекомендует документация. **`mtu`** на виртуальном интерфейсе — **9000** (учитывайте PMTU на реальном пути).

Прозрачный смок без SOCKS (если успешная маршрутизация к `1.1.1.x` через `tun0` уже настроена в netns контейнера):

```bash
docker exec sing-box-warp-masque-live curl -sS --max-time 20 https://1.1.1.1/cdn-cgi/trace
```

## Запуск на VPS

Отдельный серверный конфиг (`tun-warp-test`, SOCKS на `0.0.0.0:1080`) и упаковка архива — **[README-warp-masque-live-server.md](README-warp-masque-live-server.md)**.
