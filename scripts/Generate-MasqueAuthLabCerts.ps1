# Генерация локальной PKI для лаборатории MASQUE ACL (mTLS): CA + client-good (подписан CA) + client-bad (self-signed).
# Требуется openssl в PATH.
# Выход: experiments/router/stand/l3router/masque-auth-lab/pki/
#
#   powershell -NoProfile -File scripts/Generate-MasqueAuthLabCerts.ps1

param(
    [string]$RepoRoot = "",
    [string]$ServerTLSCommonName = "masque-auth-lab.local",
    # Если задан (например 193.x.x.x), в server-tls будет subjectAltName=IP:… (доступ по IP без домена).
    [string]$ServerTlsIpSan = "",
    [switch]$ForceServerTls
)

$ErrorActionPreference = "Stop"
if ([string]::IsNullOrWhiteSpace($RepoRoot)) {
    $RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
}
$PkiDir = Join-Path $RepoRoot "experiments\router\stand\l3router\masque-auth-lab\pki"
New-Item -ItemType Directory -Force -Path $PkiDir | Out-Null

$openssl = Get-Command openssl -ErrorAction SilentlyContinue
if (-not $openssl) {
    Write-Error "openssl не найден в PATH"
}

Push-Location $PkiDir
try {
    if (-not (Test-Path "ca.crt")) {
        & openssl ecparam -name prime256v1 -genkey -noout -out ca.key
        & openssl req -new -x509 -days 3650 -key ca.key -out ca.crt -subj "/CN=masque-auth-lab-ca/O=lab/C=ZZ"
    }
    if (-not (Test-Path "client-good.crt")) {
        & openssl ecparam -name prime256v1 -genkey -noout -out client-good.key
        & openssl req -new -key client-good.key -out client-good.csr -subj "/CN=masque-client-good/O=lab/C=ZZ"
        & openssl x509 -req -days 3650 -in client-good.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out client-good.crt
        Remove-Item client-good.csr -ErrorAction SilentlyContinue
    }
    if (-not (Test-Path "client-bad.crt")) {
        & openssl ecparam -name prime256v1 -genkey -noout -out client-bad.key
        & openssl req -new -x509 -days 30 -key client-bad.key -out client-bad.crt -subj "/CN=masque-client-bad-self/O=lab/C=ZZ"
    }
    $needServerTls = $ForceServerTls -or (-not (Test-Path "server-tls.crt"))
    if ($needServerTls) {
        if (Test-Path "server-tls.crt") { Remove-Item "server-tls.crt", "server-tls.key" -Force -ErrorAction SilentlyContinue }
        & openssl ecparam -name prime256v1 -genkey -noout -out server-tls.key
        $cn = if ([string]::IsNullOrWhiteSpace($ServerTlsIpSan)) { $ServerTLSCommonName } else { $ServerTlsIpSan }
        & openssl req -new -key server-tls.key -out server-tls.csr -subj "/CN=$cn/O=lab/C=ZZ"
        $ext = Join-Path $PWD "server-tls-ext.cnf"
        $sanLine = if (-not [string]::IsNullOrWhiteSpace($ServerTlsIpSan)) {
            "subjectAltName = IP:$ServerTlsIpSan"
        }
        else {
            "subjectAltName = DNS:$ServerTLSCommonName"
        }
        @"
[v3_req]
basicConstraints = CA:FALSE
keyUsage = digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth
$sanLine
"@ | Set-Content -LiteralPath $ext -Encoding ASCII
        & openssl x509 -req -days 3650 -in server-tls.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out server-tls.crt -extfile $ext -extensions v3_req
        Remove-Item server-tls.csr, $ext -ErrorAction SilentlyContinue
    }
}
finally {
    Pop-Location
}

Write-Host "PKI готово: $PkiDir"
