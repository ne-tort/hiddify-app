# Деплой мульти-MASQUE на VPS (sing-box один процесс, много server endpoint на портах 18610+).
# Требуется: OpenSSH (ssh/scp), openssl в PATH, сгенерированный masque-server-multi-vps.json.
#
# Пример:
#   powershell -NoProfile -File scripts/Generate-MasqueMultiVpsConfigs.ps1 -Token "секрет"
#   powershell -NoProfile -File scripts/Deploy-MasqueMultiVps.ps1 -SshTarget "root@163.5.180.181"
#
# Генерирует TLS (CN + SAN = PublicIP), кладёт в /etc/sing-box/masque-multi/, systemd unit sing-box-masque-multi.

param(
    [string]$SshTarget = "root@163.5.180.181",
    [int]$PortStart = 18610,
    [int]$ServerCount = 18,
    [string]$PublicIP = "163.5.180.181"
)

$ErrorActionPreference = "Stop"
$RepoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
$ServerJson = Join-Path $RepoRoot "experiments\router\stand\l3router\configs\masque-server-multi-vps.json"
if (-not (Test-Path $ServerJson)) {
    Write-Error "Нет файла $ServerJson — сначала запустите scripts/Generate-MasqueMultiVpsConfigs.ps1"
}

$certDir = Join-Path ([System.IO.Path]::GetTempPath()) ("masque-multi-" + [Guid]::NewGuid().ToString("n"))
New-Item -ItemType Directory -Path $certDir | Out-Null
$certPem = Join-Path $certDir "cert.pem"
$keyPem = Join-Path $certDir "key.pem"

Push-Location $certDir
try {
    $oa = @(
        "req", "-x509", "-newkey", "rsa:2048", "-sha256", "-days", "3650", "-nodes",
        "-keyout", "key.pem", "-out", "cert.pem",
        "-subj", "/CN=$PublicIP",
        "-addext", "subjectAltName=IP:$PublicIP"
    )
    $p = Start-Process -FilePath "openssl" -ArgumentList $oa -Wait -PassThru -NoNewWindow `
        -WorkingDirectory $certDir `
        -RedirectStandardOutput (Join-Path $certDir "o.log") `
        -RedirectStandardError (Join-Path $certDir "e.log")
    if ($p.ExitCode -ne 0) { throw "openssl failed exit $($p.ExitCode)" }
}
finally {
    Pop-Location
}

$remoteDir = "/etc/sing-box/masque-multi"
$portEnd = $PortStart + $ServerCount - 1

Write-Host "=== scp config + tls ==="
ssh -o StrictHostKeyChecking=accept-new $SshTarget "mkdir -p $remoteDir"
scp -o StrictHostKeyChecking=accept-new $ServerJson "${SshTarget}:$remoteDir/config.json"
scp -o StrictHostKeyChecking=accept-new $certPem "${SshTarget}:$remoteDir/cert.pem"
scp -o StrictHostKeyChecking=accept-new $keyPem "${SshTarget}:$remoteDir/key.pem"

$unit = @"
[Unit]
Description=sing-box MASQUE multi (lab, $ServerCount listeners)
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
"@

$b64 = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($unit))

$remoteScript = @"
set -eu
chmod 644 $remoteDir/config.json $remoteDir/cert.pem
chmod 600 $remoteDir/key.pem
if command -v ufw >/dev/null 2>&1; then
  ufw allow $($PortStart):$($portEnd)/tcp comment 'masque-multi tcp' || true
  ufw allow $($PortStart):$($portEnd)/udp comment 'masque-multi udp' || true
fi
echo $b64 | base64 -d > /etc/systemd/system/sing-box-masque-multi.service
systemctl daemon-reload
systemctl enable sing-box-masque-multi.service
systemctl restart sing-box-masque-multi.service
sleep 2
systemctl is-active sing-box-masque-multi.service
ss -lun | grep 1861 || true
ss -lnt | grep 1861 || true
"@ -replace "`r`n", "`n"

Write-Host "=== remote: firewall + systemd ==="
$remoteScript | ssh -o StrictHostKeyChecking=accept-new $SshTarget "bash -s"

Remove-Item -Recurse -Force $certDir
Write-Host "=== done ==="
