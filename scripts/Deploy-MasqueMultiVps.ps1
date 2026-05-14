# Деплой мульти-MASQUE на VPS (sing-box, порты 18610+).
# TLS: Let's Encrypt в /etc/letsencrypt/live/<Domain>/ — certbot только если серта ещё нет; повторные деплои не трогают серты.
# Локально нужен experiments/router/stand/l3router/configs/masque-server-multi-vps.json (Generate-MasqueMultiVpsConfigs.ps1).
#
# Пример:
#   powershell -NoProfile -File scripts/Generate-MasqueMultiVpsConfigs.ps1 -PublicHost masque.ai-qwerty.ru
#   powershell -NoProfile -File scripts/Deploy-MasqueMultiVps.ps1 -SshTarget 163.5.180.181 -Domain masque.ai-qwerty.ru -CertbotEmail you@example.com

param(
    [string]$SshTarget = "163.5.180.181",
    [string]$Domain = "masque.ai-qwerty.ru",
    [string]$CertbotEmail = "",
    [int]$PortStart = 18610,
    [int]$ServerCount = 18
)

$ErrorActionPreference = "Stop"
$RepoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
$ServerJson = Join-Path $RepoRoot "experiments\router\stand\l3router\configs\masque-server-multi-vps.json"
if (-not (Test-Path $ServerJson)) {
    Write-Error "Нет файла $ServerJson — сначала scripts/Generate-MasqueMultiVpsConfigs.ps1"
}

$remoteDir = "/etc/sing-box/masque-multi"
$portEnd = $PortStart + $ServerCount - 1

$certbotExtra = if (-not [string]::IsNullOrWhiteSpace($CertbotEmail)) {
    "-m $CertbotEmail"
} else {
    "--register-unsafely-without-email"
}

# Bash-скрипт на сервер: остановка старых инстансов, certbot при отсутствии LE, firewall, systemd.
$bash = @"
#!/bin/bash
set -eu
DOMAIN='$Domain'
LE_FULL='/etc/letsencrypt/live/$Domain/fullchain.pem'
LE_KEY='/etc/letsencrypt/live/$Domain/privkey.pem'
REMOTE_DIR='$remoteDir'
PORT0=$PortStart
PORT1=$portEnd

echo '=== stop legacy MASQUE / sing-box (освободить :80 для первого certbot) ==='
systemctl stop sing-box-masque-multi 2>/dev/null || true
systemctl stop sing-box-masque-stand 2>/dev/null || true
if command -v docker >/dev/null 2>&1; then
  docker stop sing-box-warp-masque-live-server 2>/dev/null || true
fi
pkill -f '/usr/local/bin/sing-box run' 2>/dev/null || true
sleep 2

if ! command -v certbot >/dev/null 2>&1; then
  echo '=== install certbot ==='
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -qq
  apt-get install -y certbot
fi

mkdir -p "`$REMOTE_DIR"

if [ ! -f "`$LE_FULL" ] || [ ! -f "`$LE_KEY" ]; then
  echo "=== Let's Encrypt (standalone) для `$DOMAIN ==="
  if command -v ufw >/dev/null 2>&1; then
    ufw allow 80/tcp comment 'acme-http-01' || true
  fi
  certbot certonly --standalone -d "`$DOMAIN" --non-interactive --agree-tos $certbotExtra --preferred-challenges http
else
  echo '=== LE сертификат уже есть, certbot пропускаем ==='
fi

test -f "`$LE_FULL" && test -f "`$LE_KEY"

if command -v ufw >/dev/null 2>&1; then
  ufw allow ${PortStart}:${portEnd}/tcp comment 'masque-multi tcp' || true
  ufw allow ${PortStart}:${portEnd}/udp comment 'masque-multi udp' || true
fi

chmod 755 "`$REMOTE_DIR" || true
chmod 644 "`$LE_FULL" 2>/dev/null || true

cat > /etc/systemd/system/sing-box-masque-multi.service <<'UNIT'
[Unit]
Description=sing-box MASQUE multi (lab, $ServerCount listeners, LE TLS)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/sing-box run -c $remoteDir/config.json
Restart=on-failure
RestartSec=3
LimitNOFILE=1048576
Environment=GODEBUG=http2xconnect=1

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
chmod 644 "`$REMOTE_DIR/config.json" || true
systemctl enable sing-box-masque-multi.service
systemctl restart sing-box-masque-multi.service
sleep 2
systemctl is-active sing-box-masque-multi.service
ss -lun | grep 1861 || true
ss -lnt | grep 1861 || true
"@

$tmpSh = Join-Path ([System.IO.Path]::GetTempPath()) ("masque-deploy-" + [Guid]::NewGuid().ToString("n") + ".sh")
$utf8NoBom = New-Object System.Text.UTF8Encoding $false
[System.IO.File]::WriteAllText($tmpSh, ($bash -replace "`r`n", "`n"), $utf8NoBom)

try {
    Write-Host "=== scp remote bootstrap + config ==="
    ssh -o StrictHostKeyChecking=accept-new $SshTarget "mkdir -p $remoteDir"
    scp -o StrictHostKeyChecking=accept-new $tmpSh "${SshTarget}:/tmp/masque-multi-deploy.sh"
    scp -o StrictHostKeyChecking=accept-new $ServerJson "${SshTarget}:$remoteDir/config.json"
    Write-Host "=== remote: cert + systemd + start ==="
    ssh -o StrictHostKeyChecking=accept-new $SshTarget "chmod +x /tmp/masque-multi-deploy.sh && bash /tmp/masque-multi-deploy.sh && rm -f /tmp/masque-multi-deploy.sh"
}
finally {
    Remove-Item -Force $tmpSh -ErrorAction SilentlyContinue
}

Write-Host "=== done ==="
