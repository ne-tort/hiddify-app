$ErrorActionPreference = "Stop"
$img = "masipcat/wireguard-go:latest"
$dir = $PSScriptRoot
$wg = Join-Path $dir "wireguard"

docker pull $img | Out-Host

function Wg-Genkey {
    (docker run --rm $img wg genkey).Trim()
}

function Wg-Pubkey([string]$priv) {
    ($priv | docker run --rm -i $img wg pubkey).Trim()
}

$serverPriv = Wg-Genkey
$serverPub = Wg-Pubkey $serverPriv
$clientPriv = Wg-Genkey
$clientPub = Wg-Pubkey $clientPriv

$wg0 = @"
[Interface]
Address = 10.66.66.1/24
ListenPort = 51820
PrivateKey = $serverPriv

[Peer]
PublicKey = $clientPub
AllowedIPs = 10.66.66.2/32
"@

New-Item -ItemType Directory -Force -Path $wg | Out-Null
$utf8NoBom = New-Object System.Text.UTF8Encoding($false)
[System.IO.File]::WriteAllText((Join-Path $wg "wg0.conf"), $wg0.TrimEnd() + "`n", $utf8NoBom)

# Только то, что понимает `wg setconf` (без Address — адрес задаём через ip)
$wg0core = @"
[Interface]
PrivateKey = $serverPriv
ListenPort = 51820

[Peer]
PublicKey = $clientPub
AllowedIPs = 10.66.66.2/32
"@
[System.IO.File]::WriteAllText((Join-Path $wg "wg0.core.conf"), $wg0core.TrimEnd() + "`n", $utf8NoBom)

$peer = @"
[Interface]
PrivateKey = $clientPriv
Address = 10.66.66.2/32

[Peer]
PublicKey = $serverPub
Endpoint = 127.0.0.1:51820
AllowedIPs = 10.66.66.0/24
PersistentKeepalive = 25
"@
[System.IO.File]::WriteAllText((Join-Path $wg "peer-client.conf"), $peer.TrimEnd() + "`n", $utf8NoBom)

Write-Host ""
Write-Host "=== Пир (клиент), вставь в WireGuard ===" -ForegroundColor Green
Write-Host "[Interface]"
Write-Host "PrivateKey = $clientPriv"
Write-Host "Address = 10.66.66.2/32"
Write-Host ""
Write-Host "[Peer]"
Write-Host "PublicKey = $serverPub"
Write-Host "Endpoint = 127.0.0.1:51820"
Write-Host "AllowedIPs = 10.66.66.0/24"
Write-Host "PersistentKeepalive = 25"
Write-Host ""
Write-Host "С другого ПК в LAN: Endpoint = <IP_этого_Windows>:51820"
Write-Host ""
