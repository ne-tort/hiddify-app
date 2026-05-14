# Деплой лаборатории MASQUE ACL: два listener (auth + open), PKI для mTLS, sing-box, LE.
# Перед деплоем:
#   powershell -NoProfile -File scripts/Generate-MasqueAuthLabCerts.ps1
#   powershell -NoProfile -File scripts/Generate-MasqueAuthLabConfigs.ps1 -PublicHost <домен>
#
# Опционально собрать Linux-бинарник (из корня hiddify-core, тег with_masque):
#   $env:GOOS='linux'; $env:GOARCH='amd64'; go build -tags with_masque -trimpath -o sing-box-linux-amd64 github.com/sagernet/sing-box/cmd/sing-box
#
# IP + lab TLS (без LE): сгенерировать с -UseLabTls, затем:
#   powershell -NoProfile -File scripts/Deploy-MasqueAuthLab.ps1 -SshTarget 193.233.216.26 -Domain 193.233.216.26 -LabSelfSigned

param(
    [Parameter(Mandatory = $true)][string]$SshTarget,
    [Parameter(Mandatory = $true)][string]$Domain,
    [string]$CertbotEmail = "",
    [int]$AuthPort = 18710,
    [int]$OpenPort = 18711,
    [string]$SingBoxBinary = "",
    [string]$RepoRoot = "",
    [switch]$LabSelfSigned
)

$ErrorActionPreference = "Stop"
if ([string]::IsNullOrWhiteSpace($RepoRoot)) {
    $RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
}
$ServerJson = Join-Path $RepoRoot "experiments\router\stand\l3router\configs\masque-auth-lab-server.json"
$PkiLocal = Join-Path $RepoRoot "experiments\router\stand\l3router\masque-auth-lab\pki"
if (-not (Test-Path $ServerJson)) {
    Write-Error "Missing $ServerJson. Run Generate-MasqueAuthLabConfigs.ps1 -PublicHost $Domain"
}
if (-not (Test-Path (Join-Path $PkiLocal "ca.crt"))) {
    Write-Error "Missing PKI under $PkiLocal. Run Generate-MasqueAuthLabCerts.ps1"
}
if ($LabSelfSigned) {
    foreach ($f in @("server-tls.crt", "server-tls.key")) {
        if (-not (Test-Path (Join-Path $PkiLocal $f))) {
            Write-Error "LabSelfSigned requires $PkiLocal\$f (generate with UseLabTls / Generate-MasqueAuthLabCerts.ps1 -ServerTlsIpSan ... -ForceServerTls)"
        }
    }
}

if ([string]::IsNullOrWhiteSpace($SingBoxBinary)) {
    $defaultSb = Join-Path $RepoRoot "hiddify-core\bin\linux-artifacts\sing-box-linux-amd64"
    if (Test-Path $defaultSb) {
        $SingBoxBinary = $defaultSb
    }
}

$remoteDir = "/etc/sing-box/masque-auth-lab"
$remotePki = "$remoteDir/pki"

$certbotExtra = if (-not [string]::IsNullOrWhiteSpace($CertbotEmail)) {
    "-m $CertbotEmail"
} else {
    "--register-unsafely-without-email"
}

if ($LabSelfSigned) {
    $bash = @"
#!/bin/bash
set -eu
REMOTE_DIR='$remoteDir'
AUTH_PORT=$AuthPort
OPEN_PORT=$OpenPort

echo '=== stop sing-box masque auth lab (lab TLS / IP) ==='
systemctl stop sing-box-masque-auth-lab 2>/dev/null || true
systemctl stop sing-box-masque-multi 2>/dev/null || true
systemctl stop sing-box 2>/dev/null || true
pkill -f 'sing-box run -c $remoteDir' 2>/dev/null || true
sleep 2

mkdir -p "`$REMOTE_DIR/pki"

if command -v ufw >/dev/null 2>&1; then
  ufw allow `$AUTH_PORT/tcp comment 'masque-auth-lab auth' || true
  ufw allow `$AUTH_PORT/udp comment 'masque-auth-lab auth udp' || true
  ufw allow `$OPEN_PORT/tcp comment 'masque-auth-lab open' || true
  ufw allow `$OPEN_PORT/udp comment 'masque-auth-lab open udp' || true
fi

chmod 755 "`$REMOTE_DIR" || true
chmod 755 "`$REMOTE_DIR/pki" || true

/usr/local/bin/sing-box check -c "`$REMOTE_DIR/config.json"

cat > /etc/systemd/system/sing-box-masque-auth-lab.service <<'UNIT'
[Unit]
Description=sing-box MASQUE auth lab (ACL + open, lab TLS)
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
systemctl enable sing-box-masque-auth-lab.service
systemctl restart sing-box-masque-auth-lab.service
sleep 2
systemctl is-active sing-box-masque-auth-lab.service
ss -lntu | grep -E ':($AuthPort|$OpenPort)' || true
"@
}
else {
    $bash = @"
#!/bin/bash
set -eu
DOMAIN='$Domain'
LE_FULL='/etc/letsencrypt/live/$Domain/fullchain.pem'
LE_KEY='/etc/letsencrypt/live/$Domain/privkey.pem'
REMOTE_DIR='$remoteDir'
AUTH_PORT=$AuthPort
OPEN_PORT=$OpenPort

echo '=== stop sing-box masque auth lab / free :80 if needed ==='
systemctl stop sing-box-masque-auth-lab 2>/dev/null || true
systemctl stop sing-box-masque-multi 2>/dev/null || true
pkill -f 'sing-box run -c $remoteDir' 2>/dev/null || true
sleep 2

if ! command -v certbot >/dev/null 2>&1; then
  echo '=== install certbot ==='
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -qq
  apt-get install -y certbot
fi

mkdir -p "`$REMOTE_DIR/pki"

if [ ! -f "`$LE_FULL" ] || [ ! -f "`$LE_KEY" ]; then
  echo "=== Let's Encrypt (standalone) для `$DOMAIN ==="
  if command -v ufw >/dev/null 2>&1; then
    ufw allow 80/tcp comment 'acme-http-01' || true
  fi
  certbot certonly --standalone -d "`$DOMAIN" --non-interactive --agree-tos $certbotExtra --preferred-challenges http
else
  echo '=== LE уже есть, certbot пропускаем ==='
fi

test -f "`$LE_FULL" && test -f "`$LE_KEY"

if command -v ufw >/dev/null 2>&1; then
  ufw allow `$AUTH_PORT/tcp comment 'masque-auth-lab auth' || true
  ufw allow `$AUTH_PORT/udp comment 'masque-auth-lab auth udp' || true
  ufw allow `$OPEN_PORT/tcp comment 'masque-auth-lab open' || true
  ufw allow `$OPEN_PORT/udp comment 'masque-auth-lab open udp' || true
fi

chmod 755 "`$REMOTE_DIR" || true
chmod 755 "`$REMOTE_DIR/pki" || true

/usr/local/bin/sing-box check -c "`$REMOTE_DIR/config.json"

cat > /etc/systemd/system/sing-box-masque-auth-lab.service <<'UNIT'
[Unit]
Description=sing-box MASQUE auth lab (ACL + open)
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
systemctl enable sing-box-masque-auth-lab.service
systemctl restart sing-box-masque-auth-lab.service
sleep 2
systemctl is-active sing-box-masque-auth-lab.service
ss -lntu | grep -E ':($AuthPort|$OpenPort)' || true
"@
}

$tmpSh = Join-Path ([System.IO.Path]::GetTempPath()) ("masque-auth-lab-deploy-" + [Guid]::NewGuid().ToString("n") + ".sh")
$utf8NoBom = New-Object System.Text.UTF8Encoding $false
[System.IO.File]::WriteAllText($tmpSh, ($bash -replace "`r`n", "`n"), $utf8NoBom)

try {
    Write-Host "=== ssh: каталоги ==="
    ssh -o StrictHostKeyChecking=accept-new $SshTarget "mkdir -p $remotePki"
    if (-not [string]::IsNullOrWhiteSpace($SingBoxBinary) -and (Test-Path $SingBoxBinary)) {
        Write-Host "=== scp sing-box ==="
        scp -o StrictHostKeyChecking=accept-new $SingBoxBinary "${SshTarget}:/tmp/sing-box-linux"
        ssh -o StrictHostKeyChecking=accept-new $SshTarget "install -m 0755 /tmp/sing-box-linux /usr/local/bin/sing-box && rm -f /tmp/sing-box-linux"
    }
    Write-Host "=== scp PKI + config + bootstrap ==="
    scp -o StrictHostKeyChecking=accept-new $tmpSh "${SshTarget}:/tmp/masque-auth-lab-deploy.sh"
    scp -o StrictHostKeyChecking=accept-new $ServerJson "${SshTarget}:$remoteDir/config.json"
    scp -o StrictHostKeyChecking=accept-new (Join-Path $PkiLocal "ca.crt") "${SshTarget}:$remotePki/"
    scp -o StrictHostKeyChecking=accept-new (Join-Path $PkiLocal "client-good.crt") "${SshTarget}:$remotePki/"
    scp -o StrictHostKeyChecking=accept-new (Join-Path $PkiLocal "client-good.key") "${SshTarget}:$remotePki/"
    scp -o StrictHostKeyChecking=accept-new (Join-Path $PkiLocal "client-bad.crt") "${SshTarget}:$remotePki/"
    scp -o StrictHostKeyChecking=accept-new (Join-Path $PkiLocal "client-bad.key") "${SshTarget}:$remotePki/"
    if ($LabSelfSigned) {
        scp -o StrictHostKeyChecking=accept-new (Join-Path $PkiLocal "server-tls.crt") "${SshTarget}:$remotePki/"
        scp -o StrictHostKeyChecking=accept-new (Join-Path $PkiLocal "server-tls.key") "${SshTarget}:$remotePki/"
    }
    Write-Host "=== remote: certbot + check + systemd ==="
    ssh -o StrictHostKeyChecking=accept-new $SshTarget "chmod +x /tmp/masque-auth-lab-deploy.sh && bash /tmp/masque-auth-lab-deploy.sh && rm -f /tmp/masque-auth-lab-deploy.sh"
}
finally {
    Remove-Item -Force $tmpSh -ErrorAction SilentlyContinue
}

Write-Host "=== done: server $Domain :$AuthPort (ACL) :$OpenPort (open) ==="
