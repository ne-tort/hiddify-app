# Собирает docker/masque-perf-lab/configs/masque-client-remote.generated.json из живого Hiddify current-config.json.
# Убирает log-файлы, experimental, лишние inbounds/outbounds; маршрут final → masque-PEX (TUN).
param(
    [string]$RepoRoot = "",
    [string]$SourcePath = "",
    [string]$OutputFileName = "masque-client-remote.generated.json",
    [string]$HttpLayer = "",
    [string]$MasqueTag = "masque-PEX",
    [string]$TransportMode = "",
    [string]$TemplateIP = "",
    [string]$TemplateTCP = "",
    [string]$TemplateUDP = "",
    [string]$TcpTransport = "",
    [string]$ServerToken = "",
    [string]$BasicUsername = "",
    [string]$BasicPassword = "",
    [string]$HttpLayerFallback = "",
    [ValidateSet("", "tun", "socks")]
    [string]$BenchVia = "",
    [string]$TunStack = ""
)

$ErrorActionPreference = "Stop"
if ([string]::IsNullOrWhiteSpace($RepoRoot)) {
    $RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
}
if ([string]::IsNullOrWhiteSpace($SourcePath)) {
    $SourcePath = Join-Path $RepoRoot "portable\windows-x64\Hiddify\hiddify_portable_data\data\current-config.json"
}
if (-not (Test-Path $SourcePath)) {
    throw "Hiddify config not found: $SourcePath"
}

$LabRoot = Join-Path $RepoRoot "docker\masque-perf-lab"
$raw = Get-Content $SourcePath -Raw -Encoding UTF8 | ConvertFrom-Json

$masqueEp = $null
foreach ($ep in $raw.endpoints) {
    if ($ep.type -eq "masque" -and $ep.tag -eq $MasqueTag) {
        $masqueEp = $ep
        break
    }
}
if (-not $masqueEp) {
    throw "No masque endpoint tag=$MasqueTag in $SourcePath"
}

$masqueObj = @{}
$masqueEp.PSObject.Properties | ForEach-Object { $masqueObj[$_.Name] = $_.Value }
if (-not [string]::IsNullOrWhiteSpace($HttpLayer)) {
    $masqueObj["http_layer"] = $HttpLayer
}
if ($HttpLayerFallback -eq "false" -or $HttpLayerFallback -eq "0") {
    $masqueObj["http_layer_fallback"] = $false
}
elseif ($HttpLayerFallback -eq "true" -or $HttpLayerFallback -eq "1") {
    $masqueObj["http_layer_fallback"] = $true
}
if (-not [string]::IsNullOrWhiteSpace($ServerToken)) {
    $masqueObj["server_token"] = $ServerToken
}
if (-not [string]::IsNullOrWhiteSpace($BasicUsername)) {
    $masqueObj["client_basic_username"] = $BasicUsername
}
if (-not [string]::IsNullOrWhiteSpace($BasicPassword)) {
    $masqueObj["client_basic_password"] = $BasicPassword
}

$mode = ""
if (-not [string]::IsNullOrWhiteSpace($TransportMode)) {
    $mode = $TransportMode.Trim().ToLowerInvariant()
    $masqueObj["transport_mode"] = $mode
}
elseif ($masqueObj.ContainsKey("transport_mode") -and -not [string]::IsNullOrWhiteSpace([string]$masqueObj["transport_mode"])) {
    $mode = [string]$masqueObj["transport_mode"]
}
else {
    $mode = "connect_udp"
    $masqueObj["transport_mode"] = $mode
}

if (-not $masqueObj.ContainsKey("fallback_policy")) {
    $masqueObj["fallback_policy"] = "strict"
}
if (-not $masqueObj.ContainsKey("tcp_mode")) {
    $masqueObj["tcp_mode"] = "strict_masque"
}

$tcpTr = $TcpTransport
if ([string]::IsNullOrWhiteSpace($tcpTr)) {
    if ($masqueObj.ContainsKey("tcp_transport") -and -not [string]::IsNullOrWhiteSpace([string]$masqueObj["tcp_transport"])) {
        $tcpTr = [string]$masqueObj["tcp_transport"]
    }
    else {
        $tcpTr = "connect_stream"
    }
}
$masqueObj["tcp_transport"] = $tcpTr

if ($mode -eq "connect_ip") {
    $masqueObj.Remove("template_udp")
    if (-not [string]::IsNullOrWhiteSpace($TemplateIP)) {
        $masqueObj["template_ip"] = $TemplateIP
    }
    elseif ($masqueObj.ContainsKey("template_ip") -and -not [string]::IsNullOrWhiteSpace([string]$masqueObj["template_ip"])) {
        # keep endpoint template_ip from source
    }
    else {
        $masqueObj["template_ip"] = "/masque/ip"
    }
}
else {
    $tUdp = $TemplateUDP
    if ([string]::IsNullOrWhiteSpace($tUdp)) {
        if ($masqueObj.ContainsKey("template_udp") -and -not [string]::IsNullOrWhiteSpace([string]$masqueObj["template_udp"])) {
            $tUdp = [string]$masqueObj["template_udp"]
        }
        else {
            $tUdp = "/masque/udp/{target_host}/{target_port}"
        }
    }
    $masqueObj["template_udp"] = $tUdp
}

$tTcp = $TemplateTCP
if ([string]::IsNullOrWhiteSpace($tTcp)) {
    if ($masqueObj.ContainsKey("template_tcp") -and -not [string]::IsNullOrWhiteSpace([string]$masqueObj["template_tcp"])) {
        $tTcp = [string]$masqueObj["template_tcp"]
    }
    else {
        $tTcp = "/masque/tcp/{target_host}/{target_port}"
    }
}
$masqueObj["template_tcp"] = $tTcp

$tunInbound = $null
foreach ($ib in $raw.inbounds) {
    if ($ib.type -eq "tun") {
        $tunInbound = $ib
        break
    }
}

$bv = if ([string]::IsNullOrWhiteSpace($BenchVia)) { "tun" } else { $BenchVia.Trim().ToLowerInvariant() }

if ($bv -eq "socks") {
    $primaryInbound = @{
        type        = "socks"
        tag         = "socks-in"
        listen      = "127.0.0.1"
        listen_port = 1080
    }
}
else {
    if (-not $tunInbound) {
        throw "No tun inbound in $SourcePath (required when BenchVia=tun)"
    }
    $tunObj = @{}
    $tunInbound.PSObject.Properties | ForEach-Object { $tunObj[$_.Name] = $_.Value }
    $tunObj["mtu"] = 1500
    if ($mode -eq "connect_ip" -and $tunObj.ContainsKey("address")) {
        $v4only = @()
        foreach ($a in @($tunObj["address"])) {
            if ([string]$a -match '\.') { $v4only += [string]$a }
        }
        if ($v4only.Count -gt 0) { $tunObj["address"] = $v4only }
    }
    if (-not [string]::IsNullOrWhiteSpace($TunStack)) {
        $tunObj["stack"] = $TunStack.Trim()
    }
    $primaryInbound = $tunObj
}

$server = [string]$masqueObj["server"]
if ([string]::IsNullOrWhiteSpace($server)) {
    throw "masque endpoint missing server"
}

$cfg = [ordered]@{
    log       = @{ level = "warn" }
    dns       = [ordered]@{
        servers  = @(
            [ordered]@{
                type   = "udp"
                tag    = "dns-direct"
                server = "8.8.8.8"
            }
        )
        final    = "dns-direct"
        strategy = "prefer_ipv4"
    }
    inbounds  = @($primaryInbound)
    endpoints = @($masqueObj)
    outbounds = @(
        [ordered]@{ type = "direct"; tag = "direct" }
    )
    route     = [ordered]@{
        auto_detect_interface = $true
        default_domain_resolver = "dns-direct"
        rules                 = @(
            [ordered]@{
                ip_cidr  = @("$server/32")
                outbound = "direct"
            }
        )
        final                 = $MasqueTag
    }
}

$outDir = Join-Path $LabRoot "configs"
New-Item -ItemType Directory -Force -Path $outDir | Out-Null
$outFile = Join-Path $outDir $OutputFileName
$json = $cfg | ConvertTo-Json -Depth 30
[System.IO.File]::WriteAllText($outFile, $json, [System.Text.UTF8Encoding]::new($false))
Write-Host "Wrote $outFile from Hiddify config (bench_via=$bv + $MasqueTag, no experimental)"
