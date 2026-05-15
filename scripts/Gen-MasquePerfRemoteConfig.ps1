# Генерирует docker/masque-perf-lab/configs/masque-client-remote.generated.json из env-файла.
param(
    [string]$RepoRoot = "",
    [string]$EnvPath = "",
    [string]$OutputFileName = "masque-client-remote.generated.json"
)

$ErrorActionPreference = "Stop"
if ([string]::IsNullOrWhiteSpace($RepoRoot)) {
    $RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
}
$LabRoot = Join-Path $RepoRoot "docker\masque-perf-lab"
if ([string]::IsNullOrWhiteSpace($EnvPath)) {
    $EnvPath = Join-Path $LabRoot "remote.stand.env"
}
if (-not (Test-Path $EnvPath)) {
    throw "Missing env file: $EnvPath"
}

$kv = @{}
Get-Content $EnvPath -Encoding UTF8 | ForEach-Object {
    $line = $_.Trim()
    if ($line.Length -eq 0 -or $line.StartsWith("#")) { return }
    $eq = $line.IndexOf("=")
    if ($eq -lt 1) { return }
    $kv[$line.Substring(0, $eq).Trim()] = $line.Substring($eq + 1).Trim()
}

function Req([string]$name) {
    if (-not $kv.ContainsKey($name) -or [string]::IsNullOrWhiteSpace($kv[$name])) {
        throw "Missing or empty required key in env: $name"
    }
    return $kv[$name]
}

function Opt([string]$name, [string]$default) {
    if (-not $kv.ContainsKey($name)) { return $default }
    $x = $kv[$name]
    if ($null -eq $x) { return $default }
    return $x
}

function Set-MasqueHttpLayerFallbackFromEnv([hashtable]$ep, [hashtable]$envKv) {
    if (-not $envKv.ContainsKey("MASQUE_HTTP_LAYER_FALLBACK")) { return }
    $v = [string]$envKv["MASQUE_HTTP_LAYER_FALLBACK"]
    if ($v -eq "false" -or $v -eq "0" -or $v -ieq "no") {
        $ep["http_layer_fallback"] = $false
    }
    elseif ($v -eq "true" -or $v -eq "1" -or $v -ieq "yes") {
        $ep["http_layer_fallback"] = $true
    }
}

$server = Req "MASQUE_SERVER"
$port = [int](Req "MASQUE_SERVER_PORT")
$token = Req "MASQUE_SERVER_TOKEN"
$user = Req "MASQUE_BASIC_USERNAME"
$pass = Req "MASQUE_BASIC_PASSWORD"

$tag = Opt "MASQUE_ENDPOINT_TAG" "masque-PEX"
$profile = Opt "MASQUE_PROFILE" "minimal"
$httpLayer = Opt "MASQUE_HTTP_LAYER" ""
$logLevel = Opt "MASQUE_LOG_LEVEL" "warn"
$benchVia = (Opt "MASQUE_BENCH_VIA" "tun").ToLowerInvariant()
$tunAddr = Opt "MASQUE_TUN_ADDRESS" "172.19.0.1/30"
$tunIface = Opt "MASQUE_TUN_INTERFACE" "tun0"

$ep = [ordered]@{
    type                  = "masque"
    tag                   = $tag
    mode                  = "client"
    server                = $server
    server_port           = $port
    server_token          = $token
    client_basic_username = $user
    client_basic_password = $pass
    outbound_tls          = @{
        insecure = $true
    }
}

if ($profile -eq "minimal") {
    if (-not [string]::IsNullOrWhiteSpace($httpLayer)) {
        $ep["http_layer"] = $httpLayer
    }
}
elseif ($profile -eq "bench") {
    $transport = Opt "MASQUE_TRANSPORT_MODE" "connect_udp"
    $tcpTransport = Opt "MASQUE_TCP_TRANSPORT" "connect_stream"
    $fallback = Opt "MASQUE_FALLBACK_POLICY" "strict"
    $tcpMode = Opt "MASQUE_TCP_MODE" "strict_masque"
    $tUdp = Opt "MASQUE_TEMPLATE_UDP" "/masque/udp/{target_host}/{target_port}"
    $tTcp = Opt "MASQUE_TEMPLATE_TCP" "/masque/tcp/{target_host}/{target_port}"
    $sni = Opt "MASQUE_TLS_SERVER_NAME" $server

    $ep["transport_mode"] = $transport
    $ep["fallback_policy"] = $fallback
    $ep["tcp_mode"] = $tcpMode
    $ep["tcp_transport"] = $tcpTransport
    $ep["template_udp"] = $tUdp
    $ep["template_tcp"] = $tTcp
    if (-not [string]::IsNullOrWhiteSpace($httpLayer)) {
        $ep["http_layer"] = $httpLayer
    }
    Set-MasqueHttpLayerFallbackFromEnv $ep $kv
    if (-not [string]::IsNullOrWhiteSpace($sni)) {
        $ep["outbound_tls"]["server_name"] = $sni
        $ep["outbound_tls"]["enabled"] = $true
    }
}
elseif ($profile -eq "connect_ip") {
    $fallback = Opt "MASQUE_FALLBACK_POLICY" "strict"
    $tcpMode = Opt "MASQUE_TCP_MODE" "strict_masque"
    $tcpTransport = Opt "MASQUE_TCP_TRANSPORT" "connect_ip"
    $tIp = Opt "MASQUE_TEMPLATE_IP" "/masque/ip"
    $tTcp = Opt "MASQUE_TEMPLATE_TCP" "/masque/tcp/{target_host}/{target_port}"
    $sni = Opt "MASQUE_TLS_SERVER_NAME" $server

    $ep["transport_mode"] = "connect_ip"
    $ep["fallback_policy"] = $fallback
    $ep["tcp_mode"] = $tcpMode
    $ep["tcp_transport"] = $tcpTransport
    $ep["template_ip"] = $tIp
    $ep["template_tcp"] = $tTcp
    if (-not [string]::IsNullOrWhiteSpace($httpLayer)) {
        $ep["http_layer"] = $httpLayer
    }
    Set-MasqueHttpLayerFallbackFromEnv $ep $kv
    if (-not [string]::IsNullOrWhiteSpace($sni)) {
        $ep["outbound_tls"]["server_name"] = $sni
        $ep["outbound_tls"]["enabled"] = $true
    }
}
else {
    throw "Unknown MASQUE_PROFILE: $profile (minimal, bench, connect_ip)"
}

$inbounds = @()
if ($benchVia -eq "tun") {
    $tunInbound = [ordered]@{
        type           = "tun"
        tag            = "tun-in"
        interface_name = $tunIface
        address        = @($tunAddr)
        auto_route     = $true
        strict_route   = $false
        mtu            = 1500
    }
    $tunStack = Opt "MASQUE_TUN_STACK" "gvisor"
    if (-not [string]::IsNullOrWhiteSpace($tunStack)) {
        $tunInbound["stack"] = $tunStack
    }
    $inbounds += $tunInbound
}
elseif ($benchVia -eq "socks") {
    $inbounds += @{
        type        = "socks"
        tag         = "socks-in"
        listen      = "127.0.0.1"
        listen_port = 1080
    }
}
else {
    throw "Unknown MASQUE_BENCH_VIA: $benchVia (tun, socks)"
}

$route = [ordered]@{
    rules = @(
        @{
            ip_cidr  = @("$server/32")
            outbound = "direct"
        }
    )
    final = $tag
}

$cfg = [ordered]@{
    log       = @{ level = $logLevel }
    inbounds  = $inbounds
    endpoints = @($ep)
    outbounds = @(
        @{ type = "direct"; tag = "direct" }
    )
    route     = $route
}

$outDir = Join-Path $LabRoot "configs"
New-Item -ItemType Directory -Force -Path $outDir | Out-Null
$outFile = Join-Path $outDir $OutputFileName
$json = $cfg | ConvertTo-Json -Depth 20
[System.IO.File]::WriteAllText($outFile, $json, [System.Text.UTF8Encoding]::new($false))
Write-Host "Wrote $outFile (bench_via=$benchVia)"
