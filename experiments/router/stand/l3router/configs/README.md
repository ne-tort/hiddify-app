# Конфигурации MASQUE для L3 stand

JSON-файлы в этой папке без комментариев (валидный JSON). Соответствуют шпаргалке [**`docs/masque/MASQUE-SINGBOX-CONFIG.md`**](../../../../docs/masque/MASQUE-SINGBOX-CONFIG.md):

| Файл | Роль |
|------|------|
| `masque-server.json` | Сервер: `mode: server`, `listen_port`, certs, три шаблона с `{target_host}/{target_port}` для UDP/TCP и path для CONNECT-IP. |
| `masque-server-scoped.json` | Вариант сервера под scoped/gates (сервер только MASQUE, без клиентских transport полей). |
| `masque-client.json` | Клиент: `transport_mode`/шаблоны согласованы с сервером Docker hostname `masque-server-core`; `tcp_transport: connect_stream` обязательно. |
| `masque-client-connect-ip.json` | Клиент `connect_ip` + `template_ip` только (без `template_udp`). |
| `masque-client-connect-ip-scoped.json` | То же CONNECT-IP + scope placeholders в `template_ip` `{target}/{ipproto}`. |
| `masque-client-connect-ip-scoped-bad-target.json` | Отрицательный кейс (неверная цель/контракт). |
