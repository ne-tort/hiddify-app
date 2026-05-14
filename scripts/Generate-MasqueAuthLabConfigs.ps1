# Генерация sing-box JSON: лаборатория ACL (два server masque + матрица client masque).
# 1) Сначала: scripts/Generate-MasqueAuthLabCerts.ps1
# 2) Потом этот скрипт.
#
#   powershell -NoProfile -File scripts/Generate-MasqueAuthLabConfigs.ps1 -PublicHost masque.example.com
# Локально (lab TLS из PKI, клиенты insecure): -UseLabTls; по умолчанию tls_server_name = -PublicHost (для 127.0.0.1 задайте -TlsServerName masque-auth-lab.local при CN серта).
#
# Файлы:
#   experiments/router/stand/l3router/configs/masque-auth-lab-server.json
#   experiments/router/stand/l3router/configs/masque-auth-lab-client.json
#
# На VPS пути к PKI и бинарнику задаёт Deploy-MasqueAuthLab.ps1 (по умолчанию /etc/sing-box/masque-auth-lab/pki).

param(
    [Parameter(Mandatory = $true)][string]$PublicHost,
    [int]$AuthPort = 18710,
    [int]$OpenPort = 18711,
    [string]$CertPath = "",
    [string]$KeyPath = "",
    [string]$PkiDirOnVps = "/etc/sing-box/masque-auth-lab/pki",
    [string]$RepoRoot = "",
    [switch]$UseLabTls,
    [string]$TlsServerName = ""
)

$ErrorActionPreference = "Stop"
if ([string]::IsNullOrWhiteSpace($RepoRoot)) {
    $RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
}
$PkiLocal = Join-Path $RepoRoot "experiments\router\stand\l3router\masque-auth-lab\pki"
if (-not (Test-Path (Join-Path $PkiLocal "ca.crt"))) {
    Write-Error "Missing PKI under $PkiLocal. Run scripts/Generate-MasqueAuthLabCerts.ps1"
}
$caPem = (Get-Content (Join-Path $PkiLocal "ca.crt") -Raw).TrimEnd("`r", "`n")

$labTlsCrt = Join-Path $PkiLocal "server-tls.crt"
$labTlsKey = Join-Path $PkiLocal "server-tls.key"
if ($UseLabTls) {
    if (-not (Test-Path $labTlsCrt) -or -not (Test-Path $labTlsKey)) {
        Write-Error "UseLabTls requires $labTlsCrt and $labTlsKey. Run scripts/Generate-MasqueAuthLabCerts.ps1"
    }
    # На VPS те же файлы кладутся в $PkiDirOnVps (см. Deploy-MasqueAuthLab.ps1 -LabSelfSigned); не Windows-пути.
    $pkiUnix = ($PkiDirOnVps.TrimEnd('/').Replace('\', '/') + "/")
    $CertPath = "${pkiUnix}server-tls.crt"
    $KeyPath = "${pkiUnix}server-tls.key"
} elseif ([string]::IsNullOrWhiteSpace($CertPath)) {
    $CertPath = "/etc/letsencrypt/live/$PublicHost/fullchain.pem"
}
if (-not $UseLabTls -and [string]::IsNullOrWhiteSpace($KeyPath)) {
    $KeyPath = "/etc/letsencrypt/live/$PublicHost/privkey.pem"
}

$clientTlsInsecure = [bool]$UseLabTls
if ([string]::IsNullOrWhiteSpace($TlsServerName)) {
    # Для IP + lab TLS SNI обычно совпадает с IP (сертификат с SAN IP); иначе задайте -TlsServerName вручную.
    $TlsServerName = $PublicHost
}

$goodPem = (Get-Content (Join-Path $PkiLocal "client-good.crt") -Raw).TrimEnd("`r", "`n")
$goodKeyPem = (Get-Content (Join-Path $PkiLocal "client-good.key") -Raw).TrimEnd("`r", "`n")
$badPem = (Get-Content (Join-Path $PkiLocal "client-bad.crt") -Raw).TrimEnd("`r", "`n")
$badKeyPem = (Get-Content (Join-Path $PkiLocal "client-bad.key") -Raw).TrimEnd("`r", "`n")

function LabTemplates([string]$hostName, [int]$port, [string]$prefix) {
    $base = "https://${hostName}:${port}${prefix}"
    return @{
        template_udp = "${base}/udp/{target_host}/{target_port}"
        template_ip  = "${base}/ip"
        template_tcp = "${base}/tcp/{target_host}/{target_port}"
    }
}

$tAuth = LabTemplates $PublicHost $AuthPort "/lab/a"
$tOpen = LabTemplates $PublicHost $OpenPort "/lab/o"

$epAuth = @{
    type                   = "masque"
    tag                    = "lab-auth"
    mode                   = "server"
    listen                 = "0.0.0.0"
    listen_port            = $AuthPort
    certificate            = $CertPath
    key                    = $KeyPath
    allow_private_targets  = $true
    server_auth            = @{
        policy = "first_match"
        bearer = @("lab-bearer-alpha", "lab-bearer-beta")
        basics = @(
            @{ user = "alice"; pass = "alice-secret-1" }
            @{ user = "bob"; pass = "bob-secret-2" }
        )
        mtls   = @{ ca = @($caPem) }
    }
}
$epAuth += $tAuth

$epOpen = @{
    type                   = "masque"
    tag                    = "lab-open"
    mode                   = "server"
    listen                 = "0.0.0.0"
    listen_port            = $OpenPort
    certificate            = $CertPath
    key                    = $KeyPath
    allow_private_targets  = $true
}
$epOpen += $tOpen

$serverDoc = @{
    log        = @{ level = "warn"; timestamp = $false }
    endpoints  = @($epAuth, $epOpen)
}
$outDir = Join-Path $RepoRoot "experiments\router\stand\l3router\configs"
New-Item -ItemType Directory -Force -Path $outDir | Out-Null
$serverPath = Join-Path $outDir "masque-auth-lab-server.json"
$utf8NoBom = New-Object System.Text.UTF8Encoding $false
[System.IO.File]::WriteAllText($serverPath, (($serverDoc | ConvertTo-Json -Depth 30) -replace "`r`n", "`n"), $utf8NoBom)
Write-Host "Wrote $serverPath"

function CliEp([string]$tag, [int]$port, [hashtable]$t, [hashtable]$extra, [bool]$insecure = $false) {
    $m = @{
        type              = "masque"
        tag               = $tag
        mode              = "client"
        server            = $PublicHost
        server_port       = $port
        tls_server_name   = $TlsServerName
        insecure          = $insecure
        http_layer        = "h3"
        transport_mode    = "connect_udp"
        tcp_transport     = "connect_stream"
        fallback_policy   = "strict"
        tcp_mode          = "strict_masque"
    }
    $m += $t
    foreach ($k in $extra.Keys) { $m[$k] = $extra[$k] }
    return $m
}

$clients = [System.Collections.ArrayList]@()

# Auth server matrix: inline client PEM (subscription-friendly); optional mTLS via good cert rows.
[void]$clients.Add((CliEp "cl-auth-bearer-a" $AuthPort $tAuth @{ server_token = "lab-bearer-alpha"; client_tls_cert_pem = $goodPem; client_tls_key_pem = $goodKeyPem } $clientTlsInsecure))
[void]$clients.Add((CliEp "cl-auth-bearer-b" $AuthPort $tAuth @{ server_token = "lab-bearer-beta"; client_tls_cert_pem = $goodPem; client_tls_key_pem = $goodKeyPem } $clientTlsInsecure))
[void]$clients.Add((CliEp "cl-auth-tls-only" $AuthPort $tAuth @{ client_tls_cert_pem = $goodPem; client_tls_key_pem = $goodKeyPem } $clientTlsInsecure))
[void]$clients.Add((CliEp "cl-auth-bad-bearer" $AuthPort $tAuth @{ server_token = "wrong-bearer-zzzz"; client_tls_cert_pem = $goodPem; client_tls_key_pem = $goodKeyPem } $clientTlsInsecure))
[void]$clients.Add((CliEp "cl-auth-basic-alice" $AuthPort $tAuth @{ client_basic_username = "alice"; client_basic_password = "alice-secret-1"; client_tls_cert_pem = $goodPem; client_tls_key_pem = $goodKeyPem } $clientTlsInsecure))
[void]$clients.Add((CliEp "cl-auth-basic-bob" $AuthPort $tAuth @{ client_basic_username = "bob"; client_basic_password = "bob-secret-2"; client_tls_cert_pem = $goodPem; client_tls_key_pem = $goodKeyPem } $clientTlsInsecure))
[void]$clients.Add((CliEp "cl-auth-basic-wrong-pass" $AuthPort $tAuth @{ client_basic_username = "alice"; client_basic_password = "wrong"; client_tls_cert_pem = $goodPem; client_tls_key_pem = $goodKeyPem } $clientTlsInsecure))
[void]$clients.Add((CliEp "cl-auth-mtls-badcert" $AuthPort $tAuth @{ client_tls_cert_pem = $badPem; client_tls_key_pem = $badKeyPem } $clientTlsInsecure))
[void]$clients.Add((CliEp "cl-auth-mtls-plus-bearer" $AuthPort $tAuth @{ client_tls_cert_pem = $goodPem; client_tls_key_pem = $goodKeyPem; server_token = "lab-bearer-alpha" } $clientTlsInsecure))
[void]$clients.Add((CliEp "cl-neg-no-cert-bearer-only" $AuthPort $tAuth @{ server_token = "lab-bearer-alpha" } $clientTlsInsecure))

# Open server: plain ok; creds ignored at HTTP ACL layer.
[void]$clients.Add((CliEp "cl-open-plain" $OpenPort $tOpen @{} $clientTlsInsecure))
[void]$clients.Add((CliEp "cl-open-with-bearer" $OpenPort $tOpen @{ server_token = "lab-bearer-alpha" } $clientTlsInsecure))
[void]$clients.Add((CliEp "cl-open-with-basic" $OpenPort $tOpen @{ client_basic_username = "alice"; client_basic_password = "alice-secret-1" } $clientTlsInsecure))
[void]$clients.Add((CliEp "cl-open-with-mtls" $OpenPort $tOpen @{ client_tls_cert_pem = $goodPem; client_tls_key_pem = $goodKeyPem } $clientTlsInsecure))

# Client: masque mode=client lives under endpoints[] (same semantic layer as server masque).
# outbounds keeps only direct for route.final / minimal box; matrix tags are for manual dial tests.
$clientDoc = @{
    log        = @{ level = "warn"; timestamp = $false }
    inbounds   = @()
    endpoints  = [object[]]$clients
    outbounds  = @(@{ type = "direct"; tag = "direct" })
    route      = @{ final = "direct" }
}
$clientPath = Join-Path $outDir "masque-auth-lab-client.json"
[System.IO.File]::WriteAllText($clientPath, (($clientDoc | ConvertTo-Json -Depth 30) -replace "`r`n", "`n"), $utf8NoBom)
Write-Host "Wrote $clientPath (endpoints: $($clients.Count) masque client + outbounds: direct)"
