# WARP: CLI для регистрации и WireGuard-профиля

В каталоге используются два независимых инструмента с GitHub (не официальный продукт Cloudflare).

## 1. [ViRb3/wgcf](https://github.com/ViRb3/wgcf) — обычный WARP (бесплатный / WARP+ по лицензии)

**Бинарник:** `wgcf.exe` (скачивается скриптом `download-wgcf.ps1`).

Типичный порядок (из каталога `tools/wgcf`):

```powershell
.\wgcf.exe register
.\wgcf.exe generate
```

- Создаётся `wgcf-account.toml` (учётная запись устройства у Cloudflare).
- `wgcf generate` пишет `wgcf-profile.conf` — готовый WireGuard-конфиг.
- Лицензию WARP+ (если есть ключ из приложения 1.1.1.1): см. `wgcf update --help` и документацию репозитория.

**Аккаунт Cloudflare в браузере для этого сценария не обязателен** — регистрация идёт как у анонимного клиента WARP.

## 2. [poscat0x04/wgcf-teams](https://github.com/poscat0x04/wgcf-teams) — Zero Trust / WARP for Teams

**Бинарник:** `wgcf-teams\wgcf-teams.exe`

Нужен **JWT** после входа в организацию по адресу вида `https://<team-name>.cloudflareaccess.com/warp` (подставьте свой team id из Zero Trust).

Как взять токен: [guide.md в репозитории](https://github.com/poscat0x04/wgcf-teams/blob/master/guide.md) — в DevTools в `<head>` у `meta` искать длинный URL, скопировать параметр `token=` (строка начинается с `eyJhb`).

Запуск (интерактивно запросит токен в stdin):

```powershell
.\wgcf-teams\wgcf-teams.exe
# опционально свой WG private key:
.\wgcf-teams\wgcf-teams.exe -p
```

На stdout выводится готовый WireGuard-конфиг.

**Здесь как раз используется ваш Zero Trust / аккаунт организации** (логин через Access, не API-ключ дашборда).

## Секреты

Файлы `wgcf-account.toml`, `wgcf-profile.conf` и сгенерированные ключи **нельзя** коммитить в git — они в `.gitignore` корня репозитория для этого каталога.

## Повторная установка бинарников

```powershell
Set-Location tools\wgcf
.\download-wgcf.ps1
```
