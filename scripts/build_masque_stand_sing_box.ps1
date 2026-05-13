# Сборка sing-box (linux/amd64) для VPS-стенда MASQUE из текущего hiddify-sing-box (replace в go.mod).
# Запускать из каталога hiddify-core. Без naive/tailscale — кросс-компиляция с CGO_ENABLED=0.
$ErrorActionPreference = "Stop"
$core = Resolve-Path (Join-Path $PSScriptRoot "..\hiddify-core")
Set-Location $core
$outDir = Join-Path $PSScriptRoot "..\dist\masque-stand"
New-Item -ItemType Directory -Force -Path $outDir | Out-Null
$out = Join-Path $outDir "sing-box-linux-amd64"
$env:GOOS = "linux"
$env:GOARCH = "amd64"
$env:CGO_ENABLED = "0"
$tags = "with_gvisor,with_quic,with_wireguard,with_awg,with_masque,with_utls,with_clash_api,with_grpc,with_acme"
go build -tags $tags -trimpath "-ldflags=-s -w" -o $out github.com/sagernet/sing-box/cmd/sing-box
Write-Host "OK: $out"
