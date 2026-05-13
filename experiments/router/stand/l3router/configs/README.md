# Конфигурации MASQUE для L3 stand

JSON-файлы в этой папке без комментариев (валидный JSON). Соответствуют шпаргалке [**`docs/masque/MASQUE-SINGBOX-CONFIG.md`**](../../../../docs/masque/MASQUE-SINGBOX-CONFIG.md):

| Файл | Роль |
|------|------|
| `masque-server.json` | Сервер: `mode: server`, `listen_port`, certs, три шаблона с `{target_host}/{target_port}` для UDP/TCP и path для CONNECT-IP. |
| `masque-server-multi.json` | **Несколько** независимых MASQUE server endpoints: разные `listen_port` (18500–18503), уникальные `tag`, непересекающиеся шаблоны URL; варианты: дефолтные пути `/masque/…`, свой префикс `/m2/…`, scoped CONNECT-IP `/m3/ip/{target}/{ipproto}`, портовый allowlist + `server_token` на :18503. HTTP/2 (TCP+TLS) и HTTP/3 (QUIC) на **каждом** порту включаются автоматически (как у одного `masque-server`). Docker: `docker-compose.masque-e2e.multi.override.yml`. |
| `masque-server-scoped.json` | Вариант сервера под scoped/gates (сервер только MASQUE, без клиентских transport полей). |
| `masque-client.json` | Клиент: `transport_mode`/шаблоны согласованы с сервером Docker hostname `masque-server-core`; `tcp_transport: connect_stream` обязательно. |
| `masque-client-connect-ip.json` | Клиент `connect_ip` + `template_ip` только (без `template_udp`). |
| `masque-client-connect-ip-tcp-via-stack.json` | Тот же CONNECT-IP, но `tcp_transport: connect_ip` (TCP через gVisor userspace над CONNECT-IP); проверка: `python masque_stand_runner.py --scenario socks_tcp_ip_stack`. |
| `masque-client-connect-ip-scoped.json` | То же CONNECT-IP + scope placeholders в `template_ip` `{target}/{ipproto}`. |
| `masque-client-connect-ip-scoped-bad-target.json` | Отрицательный кейс (неверная цель/контракт). |
