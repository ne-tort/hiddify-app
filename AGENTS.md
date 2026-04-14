# Правила для ассистентов (репозиторий `hiddify-app`)

## Ручная проверка VPN: CLI → конфиг → SMB

Сценарий не привязан к конкретному протоколу входа: нужен собранный `**HiddifyCli.exe**`, JSON **sing-box** (после импорта/генерации под ваш профиль) и доступ к тестовой SMB-шаре по VPN-адресу стенда.

1. **Соберите portable-клиент** (Windows), чтобы рядом с `HiddifyCli.exe` лежали DLL и данные ядра. Подробности: `[docs/BUILD.md](docs/BUILD.md)` — раздел про `make windows-portable` и каталог `portable/windows-x64/Hiddify/`.
2. **Подготовьте конфиг** — путь к JSON, который передаётся в `HiddifyCli ... srun -c <файл>`. Секреты и рабочие каталоги не коммитьте; для черновиков используйте локальный `tmp/` (он в `.gitignore`).
3. **Проверка SMB** — скрипт `[scripts/test_cli_smb_self.py](scripts/test_cli_smb_self.py)`: поднимает CLI с указанным конфигом, ждёт mixed-порт, проверяет TCP к VPN-IP (по умолчанию `10.8.0.3:445`) и при необходимости полноценный SMB (`--full-smb` / `--require-smb-auth`).

Пример (PowerShell), учётные данные только через переменные окружения:

```powershell
$env:HIDDIFY_SMB_USER = "<логин>"
$env:HIDDIFY_SMB_PASSWORD = "<пароль>"
python scripts/test_cli_smb_self.py `
  --cli "portable\windows-x64\Hiddify\HiddifyCli.exe" `
  --workdir "tmp\cli_smb_work" `
  --config "path\to\your\sing-box.json" `
  --vpn-ip 10.8.0.3 `
  --full-smb
```

Параметры `--vpn-ip` и путь к конфигу должны соответствовать вашему стенду. Креды в репозиторий не добавлять.

## Локальный стенд `universal`

Каталог `**stand-multi-cascad-prod-l3-universal/**` (копия одноимённого стенда из репозитория `sui`) используется только локально для Docker/compose и **не коммитится** (см. `.gitignore`). При отсутствии папки скопируйте её из `sui` в корень этого репозитория под тем же именем.

## Стенд HY2 + WG-хаб (VPS `31.56.211.60`)

Каталог `**tmp/universal-singlehop-31.56.211.60/**`: один sing-box на сервере (HY2 → отдельные `wg-leaf` на клиента + звезда через `wg-hub` на loopback), два клиента в Docker (`docker-compose.wg-hub.yml`), третий — Windows portable CLI. Текущая схема, тесты SMB и критерий успеха: **`tmp/universal-singlehop-31.56.211.60/AGENTS.md`**.