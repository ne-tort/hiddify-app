#requires -Version 5.1
<#
.SYNOPSIS
  Загружает linux/amd64 sing-box и deploy_masque_stand_vps.sh на VPS, запускает деплой под sudo.

.PARAMETER SshTarget
  user@host (OpenSSH).

.PARAMETER MasquePublicIp
  Публичный IP или DNS — совпадает с полем server у клиента и SAN сертификата (для IP — subjectAltName=IP:...).

.PARAMETER MasquePort
  Порт MASQUE (по умолчанию 18443).

.PARAMETER LocalSingBox
  Локальный путь к бинарнику sing-box linux/amd64 (см. dist/masque-stand после сборки из hiddify-core).

.EXAMPLE
  .\scripts\push_masque_stand.ps1 -SshTarget root@203.0.113.10 -MasquePublicIp 203.0.113.10
#>
param(
  [Parameter(Mandatory = $true)][string]$SshTarget,
  [Parameter(Mandatory = $true)][string]$MasquePublicIp,
  [int]$MasquePort = 18443,
  [string]$LocalSingBox = (Join-Path $PSScriptRoot "..\dist\masque-stand\sing-box-linux-amd64")
)

$ErrorActionPreference = "Stop"
$deployLocal = Join-Path $PSScriptRoot "deploy_masque_stand_vps.sh"
if (-not (Test-Path -LiteralPath $deployLocal)) { throw "Не найден $deployLocal" }
if (-not (Test-Path -LiteralPath $LocalSingBox)) {
  throw "Нет бинарника: $LocalSingBox. Сборка из каталога hiddify-core (пример):`n" +
    '  `$env:GOOS=''linux''; `$env:GOARCH=''amd64''; `$env:CGO_ENABLED=''0''`n' +
    '  go build -tags with_gvisor,with_quic,with_wireguard,with_awg,with_masque,with_utls,with_clash_api,with_grpc,with_acme -trimpath "-ldflags=-s -w" -o ..\dist\masque-stand\sing-box-linux-amd64 github.com/sagernet/sing-box/cmd/sing-box'
}

$rid = [Guid]::NewGuid().ToString("N").Substring(0, 8)
$remoteTmp = "/tmp/masque-stand-push-$rid"
$remoteBin = "$remoteTmp/sing-box-linux-amd64"
$remoteSh  = "$remoteTmp/deploy_masque_stand_vps.sh"

Write-Host "Copying to $SshTarget ..."
ssh $SshTarget "mkdir -p '$remoteTmp'"
scp -p "$LocalSingBox" "${SshTarget}:$remoteBin"
scp -p "$deployLocal" "${SshTarget}:$remoteSh"

$bashLc = "chmod +x $remoteBin $remoteSh && export MASQUE_PUBLIC_IP=$MasquePublicIp MASQUE_PORT=$MasquePort SING_BOX_STAGED_PATH=$remoteBin && bash $remoteSh && rm -rf $remoteTmp"
Write-Host "Starting remote deploy (sudo) ..."
ssh $SshTarget "sudo bash -lc '$bashLc'"

Write-Host "Done. Copy SERVER_TOKEN from deploy output into client profile (server_token)."
