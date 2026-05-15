# Сборка linux/amd64 sing-box (with_masque) + Docker-образ masque-perf-lab.
# Запуск из корня репо:
#   powershell -NoProfile -File scripts/Build-MasquePerfLab.ps1
#
# Требует: Go, Docker, openssl в PATH (например Git for Windows).
$ErrorActionPreference = "Stop"
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$LabRoot = Join-Path $RepoRoot "docker\masque-perf-lab"
$Artifacts = Join-Path $LabRoot "artifacts"
$Certs = Join-Path $LabRoot "certs"
$core = Join-Path $RepoRoot "hiddify-core"

New-Item -ItemType Directory -Force -Path $Artifacts | Out-Null
New-Item -ItemType Directory -Force -Path $Certs | Out-Null

$out = Join-Path $Artifacts "sing-box-linux-amd64"
$tags = "with_gvisor,with_quic,with_wireguard,with_awg,with_masque,with_utls,with_clash_api,with_grpc,with_acme"
Push-Location $core
try {
    $env:GOOS = "linux"
    $env:GOARCH = "amd64"
    $env:CGO_ENABLED = "0"
    & go build -tags $tags -trimpath "-ldflags=-s -w" -o $out "github.com/sagernet/sing-box/cmd/sing-box"
    if ($LASTEXITCODE -ne 0) { throw "go build failed" }
}
finally {
    Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED -ErrorAction SilentlyContinue
    Pop-Location
}

$cert = Join-Path $Certs "masque-cert.pem"
$key = Join-Path $Certs "masque-key.pem"
if (-not (Test-Path $cert) -or -not (Test-Path $key)) {
    $openssl = Get-Command openssl -ErrorAction SilentlyContinue
    if (-not $openssl) { throw "openssl not in PATH (install Git for Windows or OpenSSL) and certs/ missing" }
    & openssl req -x509 -nodes -newkey rsa:2048 -keyout $key -out $cert -days 3650 -subj "/CN=masque-server-core"
    if ($LASTEXITCODE -ne 0) { throw "openssl failed" }
}

Push-Location $LabRoot
try {
    $stamp = (Get-Item $out).LastWriteTimeUtc.Ticks
    $oldEap = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    try {
        & docker compose build --build-arg "SINGBOX_ARTIFACT_STAMP=$stamp" 2>&1 | Out-Null
        if ($LASTEXITCODE -ne 0) { throw "docker compose build failed (exit $LASTEXITCODE)" }
    }
    finally {
        $ErrorActionPreference = $oldEap
    }
}
finally {
    Pop-Location
}

Write-Host "OK: image masque-perf-lab:local, binary $out"
