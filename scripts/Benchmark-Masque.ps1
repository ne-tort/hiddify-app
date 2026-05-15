# MASQUE matrix bench: профили h3/h2/connect-ip-*; опционально два входа (TUN vs SOCKS).
#
#   powershell -NoProfile -File scripts\Benchmark-Masque.ps1
#   powershell -NoProfile -File scripts\Benchmark-Masque.ps1 -SkipBuild -Profile h3
#   powershell -NoProfile -File scripts\Benchmark-Masque.ps1 -BenchVia all   # каждый профиль: tun затем socks
param(
    [switch]$SkipBuild,
    [ValidateSet("", "h3", "h2", "connect-ip-h3", "connect-ip-h2")]
    [string]$Profile = "",
    [ValidateSet("", "tun", "socks", "all")]
    [string]$BenchVia = ""
)

$ErrorActionPreference = "Stop"
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$LabRoot = Join-Path $RepoRoot "docker\masque-perf-lab"
$genScript = Join-Path $RepoRoot "scripts\Gen-MasquePerfRemoteConfig.ps1"
$hiddifyScript = Join-Path $RepoRoot "scripts\Import-MasqueHiddifyBenchConfig.ps1"
$buildScript = Join-Path $RepoRoot "scripts\Build-MasquePerfLab.ps1"
$container = "masque-perf-remote-client"

function Read-DotEnv([string]$path) {
    $h = @{}
    if (-not (Test-Path $path)) { return $h }
    Get-Content $path -Encoding UTF8 | ForEach-Object {
        $line = $_.Trim()
        if ($line.Length -eq 0 -or $line.StartsWith("#")) { return }
        $eq = $line.IndexOf("=")
        if ($eq -lt 1) { return }
        $h[$line.Substring(0, $eq).Trim()] = $line.Substring($eq + 1).Trim()
    }
    return $h
}

function Merge-Env([hashtable]$a, [hashtable]$b) {
    $m = @{}
    foreach ($k in $a.Keys) { $m[$k] = $a[$k] }
    foreach ($k in $b.Keys) { $m[$k] = $b[$k] }
    return $m
}

function Write-MergedEnv([hashtable]$m, [string]$path) {
    $m.GetEnumerator() | Sort-Object Name | ForEach-Object { "$($_.Key)=$($_.Value)" } |
        Set-Content -Encoding UTF8 $path
}

function Test-IperfPreflight([hashtable]$r) {
    $iperfHost = $r["BENCH_IPERF_TARGET_HOST"]
    $iperfPort = $r["BENCH_IPERF_TARGET_PORT"]
    $vps = $r["MASQUE_SERVER"]
    $old = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    try {
        & ssh -o BatchMode=yes -o ConnectTimeout=12 $iperfHost "systemctl is-active iperf3-masque-${iperfPort}; ss -tln | grep -q ':${iperfPort} '" 2>&1 | Out-Null
        if ($LASTEXITCODE -ne 0) { throw "iperf not ready on ${iperfHost}:${iperfPort}" }
        & ssh -o BatchMode=yes -o ConnectTimeout=12 $vps "nc -z -w 5 ${iperfHost} ${iperfPort}" 2>&1 | Out-Null
        if ($LASTEXITCODE -ne 0) { throw "no TCP from ${vps} to ${iperfHost}:${iperfPort}" }
    }
    finally { $ErrorActionPreference = $old }
}

# Локальный UDP iperf на самой машине с iperf3 -s: подтверждает, что демон принимает UDP на порту (ss -ulnp LISTEN для iperf3 часто пустой).
function Test-IperfUdpLocalhostOnIperfHost([hashtable]$r) {
    $iperfHost = $r["BENCH_IPERF_TARGET_HOST"]
    $iperfPort = $r["BENCH_IPERF_TARGET_PORT"]
    $ms = if ($r.ContainsKey("IPERF_CONNECT_TIMEOUT_MS")) { [string]$r["IPERF_CONNECT_TIMEOUT_MS"] } else { "5000" }
    $old = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    try {
        $remote = 'iperf3 -u -c 127.0.0.1 -p ' + $iperfPort + ' -t 1 -b 768k -l 1400 --connect-timeout ' + $ms + ' 2>&1; ec=$?; exit $ec'
        $out = & ssh -o BatchMode=yes -o ConnectTimeout=15 $iperfHost $remote 2>&1
        $code = $LASTEXITCODE
        $blob = "$out"
        if (($code -ne 0) -or ($blob -match 'unable to receive|unable to read|iperf3: error|Connection refused')) {
            throw "iperf UDP localhost failed on ${iperfHost}:${iperfPort} (ssh exit=${code}). Server must accept UDP on same port as TCP (-s). Output:`n${blob}"
        }
        if ($blob -match 'receiver.*\(100%\)' -or $blob -match '\(100%\).*receiver') {
            throw "iperf UDP localhost 100% loss on ${iperfHost}:${iperfPort}. Output:`n${blob}"
        }
        Write-Host "MASQUE bench: iperf host ${iperfHost}:${iperfPort} UDP OK (localhost iperf3 -u; ss -ulnp may omit iperf until a session runs)."
    }
    finally { $ErrorActionPreference = $old }
}

# UDP не проверяется в Test-IperfPreflight (только TCP nc). Отдельный короткий iperf3 -u с VPS:
# если он падает, а TCP с VPS жив, чаще всего на хосте iperf закрыт UDP в firewall — тогда FAIL UDP probe в бенче
# не доказывает поломку CONNECT-UDP в клиенте sing-box.
function Test-IperfUdpBaselineFromVps([hashtable]$r) {
    $iperfHost = $r["BENCH_IPERF_TARGET_HOST"]
    $iperfPort = $r["BENCH_IPERF_TARGET_PORT"]
    $vps = $r["MASQUE_SERVER"]
    $ms = if ($r.ContainsKey("IPERF_CONNECT_TIMEOUT_MS")) { [string]$r["IPERF_CONNECT_TIMEOUT_MS"] } else { "5000" }
    $old = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    try {
        # Single-quoted pieces so PowerShell does not interpret $? / $ec (bash variables on remote).
        $remote = 'command -v iperf3 >/dev/null 2>&1 || { echo MASQUE_BENCH_UDP_BASELINE_SKIP=no_iperf3_on_vps; exit 0; }; iperf3 -u -c ' +
            $iperfHost + ' -p ' + $iperfPort + ' -t 2 -b 768k -l 1400 --connect-timeout ' + $ms + ' 2>&1; ec=$?; exit $ec'
        $out = & ssh -o BatchMode=yes -o ConnectTimeout=18 $vps $remote 2>&1
        $code = $LASTEXITCODE
        if ("$out" -match "MASQUE_BENCH_UDP_BASELINE_SKIP") {
            Write-Host "MASQUE bench: VPS has no iperf3 - skipped UDP baseline (install iperf3 on VPS for UDP firewall check)."
            return
        }
        if (($code -ne 0) -or ("$out" -match "unable to receive|unable to read|iperf3: error|Connection refused")) {
            Write-Warning "MASQUE bench: UDP iperf VPS=${vps} -> ${iperfHost}:${iperfPort} failed (exit=$code). Open UDP on iperf host/firewall or UDP probe FAIL may be infra, not VPN CONNECT-UDP."
            return
        }
        Write-Host "MASQUE bench: UDP baseline VPS->iperf OK (if container UDP probe fails, suspect TUN/MASQUE CONNECT-UDP, not empty UDP listen on iperf host)."
    }
    finally { $ErrorActionPreference = $old }
}

function Get-TunIfaceFromConfig {
    $cfgPath = Join-Path $LabRoot "configs\masque-client-remote.generated.json"
    if (-not (Test-Path $cfgPath)) { return "tun0" }
    try {
        $j = Get-Content $cfgPath -Raw | ConvertFrom-Json
        foreach ($ib in $j.inbounds) {
            if ($ib.type -eq "tun" -and $ib.interface_name) { return [string]$ib.interface_name }
        }
    }
    catch { }
    return "tun0"
}

function Get-BenchInboundType {
    $cfgPath = Join-Path $LabRoot "configs\masque-client-remote.generated.json"
    if (-not (Test-Path $cfgPath)) { return "unknown" }
    try {
        $j = Get-Content $cfgPath -Raw | ConvertFrom-Json
        foreach ($ib in $j.inbounds) {
            if ($ib.type -eq "tun") { return "tun" }
            if ($ib.type -eq "socks") { return "socks" }
        }
    }
    catch { }
    return "unknown"
}

function Wait-SocksListen([int]$maxSec) {
    for ($i = 0; $i -lt $maxSec; $i++) {
        & docker exec $container sh -c "nc -z 127.0.0.1 1080" 2>$null | Out-Null
        if ($LASTEXITCODE -eq 0) { return }
        Start-Sleep -Seconds 1
    }
    throw "SOCKS 127.0.0.1:1080 not listening after ${maxSec}s"
}

function Wait-Tun([hashtable]$r, [int]$maxSec) {
    $iface = Get-TunIfaceFromConfig
    for ($i = 0; $i -lt $maxSec; $i++) {
        & docker exec $container sh -c "ip link show $iface 2>/dev/null | grep -q 'UP'" 2>$null | Out-Null
        if ($LASTEXITCODE -eq 0) { return }
        Start-Sleep -Seconds 1
    }
    throw "TUN $iface not UP after ${maxSec}s"
}

function Test-ConnectIpProfile([hashtable]$r) {
    if ($r.ContainsKey("MASQUE_TRANSPORT_MODE") -and [string]$r["MASQUE_TRANSPORT_MODE"] -eq "connect_ip") { return $true }
    if ($r.ContainsKey("MASQUE_PROFILE") -and [string]$r["MASQUE_PROFILE"] -eq "connect_ip") { return $true }
    return $false
}

function Wait-MasqueConnectIP([int]$maxSec) {
    for ($i = 0; $i -lt $maxSec; $i++) {
        $out = docker logs $container 2>&1 | Out-String
        if ($out -match 'open_ip_session_success|event_reason":"open_ip_session_success') { return }
        Start-Sleep -Seconds 1
    }
    throw ('CONNECT-IP session not ready after {0}s (check masque-perf-remote-client logs)' -f $maxSec)
}

function Invoke-BenchReport([hashtable]$r) {
    $benchSrc = (Resolve-Path (Join-Path $LabRoot "bench\run-bench-report.sh")).Path
    $tmp = Join-Path $env:TEMP "masque-bench-report.sh"
    [System.IO.File]::WriteAllText($tmp, ([System.IO.File]::ReadAllText($benchSrc) -replace "`r`n", "`n"), [Text.UTF8Encoding]::new($false))

    $cid = docker ps -qf "name=$container"
    if (-not $cid) { throw "container not running" }

    $probeSec = "5"
    if ($r.ContainsKey("BENCH_CONNECT_TIMEOUT_SEC")) { $probeSec = $r["BENCH_CONNECT_TIMEOUT_SEC"] }
    $iperfMs = "5000"
    if ($r.ContainsKey("IPERF_CONNECT_TIMEOUT_MS")) { $iperfMs = $r["IPERF_CONNECT_TIMEOUT_MS"] }
    $dur = [int]$r["IPERF_DURATION_SEC"]
    $wall = $dur + [int]$probeSec + 12
    $benchVia = "tun"
    if ($r.ContainsKey("MASQUE_BENCH_VIA")) { $benchVia = $r["MASQUE_BENCH_VIA"] }
    $envArgs = @(
        "-e", "BENCH_VIA=$benchVia",
        "-e", "BENCH_WAIT_SEC=2",
        "-e", "BENCH_CONNECT_TIMEOUT_SEC=$probeSec",
        "-e", "IPERF_TARGET_HOST=$($r['BENCH_IPERF_TARGET_HOST'])",
        "-e", "IPERF_TARGET_PORT=$($r['BENCH_IPERF_TARGET_PORT'])",
        "-e", "IPERF_DURATION_SEC=$dur",
        "-e", "BENCH_WALL_TIMEOUT_SEC=$wall",
        "-e", "IPERF_CONNECT_TIMEOUT_MS=$iperfMs"
    )
    $udpProbe = "0"
    if ($r.ContainsKey("BENCH_UDP_PROBE")) { $udpProbe = [string]$r["BENCH_UDP_PROBE"] }
    $envArgs += "-e", "BENCH_UDP_PROBE=$udpProbe"

    $old = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    try {
        $raw = & docker run --rm @envArgs --network "container:$cid" -v "${tmp}:/bench/run.sh:ro" masque-perf-lab:local /bin/sh /bench/run.sh 2>&1
        $lines = $raw | Where-Object { $_ -match '^RESULT_' }
    }
    finally { $ErrorActionPreference = $old }

    $parsed = @{ ok = $false; up = ""; down = ""; err = ""; udp_ok = ""; udp_err = ""; udp_mbit = "" }
    foreach ($line in $lines) {
        if ($line -match "^RESULT_OK=(.+)$") { $parsed.ok = ($Matches[1] -eq "1") }
        elseif ($line -match "^RESULT_UP_MBIT=(.+)$") { $parsed.up = $Matches[1] }
        elseif ($line -match "^RESULT_DOWN_MBIT=(.+)$") { $parsed.down = $Matches[1] }
        elseif ($line -match "^RESULT_ERR=(.+)$") { $parsed.err = $Matches[1] }
        elseif ($line -match "^RESULT_UDP_OK=(.+)$") { $parsed.udp_ok = $Matches[1] }
        elseif ($line -match "^RESULT_UDP_ERR=(.+)$") { $parsed.udp_err = $Matches[1] }
        elseif ($line -match "^RESULT_UDP_MBIT=(.+)$") { $parsed.udp_mbit = $Matches[1] }
    }
    if ($parsed.ok -and $r.ContainsKey("BENCH_UDP_PROBE") -and [string]$r["BENCH_UDP_PROBE"] -eq "1") {
        if ($parsed.udp_ok -eq "0") {
            $parsed.ok = $false
            $ue = if ($parsed.udp_err) { $parsed.udp_err } else { "UDP probe failed" }
            if (-not $parsed.err) { $parsed.err = $ue }
        }
    }
    return $parsed
}

function Invoke-DockerCompose([string[]]$composeArgs) {
    $old = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    try {
        & docker compose @composeArgs 2>&1 | Out-Null
        if ($LASTEXITCODE -ne 0) { throw "docker compose failed exit=$LASTEXITCODE args=$($composeArgs -join ' ')" }
    }
    finally { $ErrorActionPreference = $old }
}

function Stop-RemoteStack {
    Push-Location $LabRoot
    try { Invoke-DockerCompose @("-f", "docker-compose.remote.yml", "down", "--remove-orphans") }
    finally { Pop-Location }
}

function Start-RemoteStack {
    Push-Location $LabRoot
    try { Invoke-DockerCompose @("-f", "docker-compose.remote.yml", "up", "-d", "masque-remote-client") }
    finally { Pop-Location }
}

function Invoke-GenBenchConfig([hashtable]$r) {
    $source = "gen"
    if ($r.ContainsKey("MASQUE_CONFIG_SOURCE")) { $source = $r["MASQUE_CONFIG_SOURCE"].ToLowerInvariant() }
    if ($source -eq "hiddify") {
        $src = Join-Path $RepoRoot "portable\windows-x64\Hiddify\hiddify_portable_data\data\current-config.json"
        if ($r.ContainsKey("MASQUE_HIDDIFY_CONFIG_PATH")) { $src = $r["MASQUE_HIDDIFY_CONFIG_PATH"] }
        if (-not [System.IO.Path]::IsPathRooted($src)) { $src = Join-Path $RepoRoot $src }
        $layer = ""
        if ($r.ContainsKey("MASQUE_HTTP_LAYER")) { $layer = $r["MASQUE_HTTP_LAYER"] }
        $tag = "masque-PEX"
        if ($r.ContainsKey("MASQUE_ENDPOINT_TAG")) { $tag = $r["MASQUE_ENDPOINT_TAG"] }
        $importArgs = @{
            RepoRoot        = $RepoRoot
            SourcePath      = $src
            MasqueTag       = $tag
            HttpLayer       = $layer
            TransportMode   = ""
            TemplateIP      = ""
            TemplateTCP     = ""
            TemplateUDP     = ""
            TcpTransport    = ""
            ServerToken     = ""
            BasicUsername   = ""
            BasicPassword   = ""
        }
        if ($r.ContainsKey("MASQUE_TRANSPORT_MODE")) { $importArgs["TransportMode"] = $r["MASQUE_TRANSPORT_MODE"] }
        if ($r.ContainsKey("MASQUE_TEMPLATE_IP")) { $importArgs["TemplateIP"] = $r["MASQUE_TEMPLATE_IP"] }
        if ($r.ContainsKey("MASQUE_TEMPLATE_TCP")) { $importArgs["TemplateTCP"] = $r["MASQUE_TEMPLATE_TCP"] }
        if ($r.ContainsKey("MASQUE_TEMPLATE_UDP")) { $importArgs["TemplateUDP"] = $r["MASQUE_TEMPLATE_UDP"] }
        if ($r.ContainsKey("MASQUE_TCP_TRANSPORT")) { $importArgs["TcpTransport"] = $r["MASQUE_TCP_TRANSPORT"] }
        if ($r.ContainsKey("MASQUE_SERVER_TOKEN")) { $importArgs["ServerToken"] = $r["MASQUE_SERVER_TOKEN"] }
        if ($r.ContainsKey("MASQUE_BASIC_USERNAME")) { $importArgs["BasicUsername"] = $r["MASQUE_BASIC_USERNAME"] }
        if ($r.ContainsKey("MASQUE_BASIC_PASSWORD")) { $importArgs["BasicPassword"] = $r["MASQUE_BASIC_PASSWORD"] }
        if ($r.ContainsKey("MASQUE_HTTP_LAYER_FALLBACK")) { $importArgs["HttpLayerFallback"] = $r["MASQUE_HTTP_LAYER_FALLBACK"] }
        $bvImp = ""
        if ($r.ContainsKey("MASQUE_BENCH_VIA")) { $bvImp = [string]$r["MASQUE_BENCH_VIA"] }
        $importArgs["BenchVia"] = $bvImp
        if ($r.ContainsKey("MASQUE_TUN_STACK")) { $importArgs["TunStack"] = [string]$r["MASQUE_TUN_STACK"] }
        & $hiddifyScript @importArgs
        return
    }
    $tmpEnv = Join-Path $env:TEMP "masque-bench-merged.env"
    Write-MergedEnv $r $tmpEnv
    & $genScript -RepoRoot $RepoRoot -EnvPath $tmpEnv
}

$allProfiles = @(
    @{ id = "h3";            label = "connect_udp + http_layer=h3 + tcp/connect_stream"; file = "profiles\h3.env" }
    @{ id = "h2";            label = "connect_udp + http_layer=h2 + tcp/connect_stream"; file = "profiles\h2.env" }
    @{ id = "connect-ip-h3"; label = "connect_ip + http_layer=h3 + tcp/connect_ip";   file = "profiles\connect-ip-h3.env" }
    @{ id = "connect-ip-h2"; label = "connect_ip + http_layer=h2 + tcp/connect_ip";   file = "profiles\connect-ip-h2.env" }
)

if ($Profile) {
    $allProfiles = @($allProfiles | Where-Object { $_.id -eq $Profile })
}

if (-not (Test-Path (Join-Path $LabRoot "artifacts\sing-box-linux-amd64"))) {
    $SkipBuild = $false
}
if (-not $SkipBuild) {
    & $buildScript
    if ($LASTEXITCODE -ne 0) { throw "build failed" }
}

$basePath = Join-Path $LabRoot "remote.base.env"
$standPath = Join-Path $LabRoot "remote.stand.env"
$credPath = Join-Path $LabRoot "remote.credentials.env"
$base = Read-DotEnv $basePath
if (Test-Path $standPath) { $base = Merge-Env $base (Read-DotEnv $standPath) }
if (Test-Path $credPath) { $base = Merge-Env $base (Read-DotEnv $credPath) }

Test-IperfPreflight $base
Test-IperfUdpLocalhostOnIperfHost $base
Test-IperfUdpBaselineFromVps $base

$viaModes = @("tun")
if (-not [string]::IsNullOrWhiteSpace($BenchVia)) {
    switch ($BenchVia.ToLowerInvariant()) {
        "tun" { $viaModes = @("tun") }
        "socks" { $viaModes = @("socks") }
        "all" { $viaModes = @("tun", "socks") }
        default { throw "Unexpected BenchVia: $BenchVia" }
    }
}

$results = @()

foreach ($p in $allProfiles) {
    $profEnv = Merge-Env $base (Read-DotEnv (Join-Path $LabRoot $p.file))
    foreach ($via in $viaModes) {
        if ($via -eq "socks" -and (Test-ConnectIpProfile $profEnv)) {
            $results += [pscustomobject]@{
                profile = $p.id
                via     = "socks"
                desc    = $p.label
                status  = "SKIP"
                up      = "-"
                down    = "-"
                udp     = "-"
                note    = "CONNECT-IP needs TUN packet plane; SOCKS branch skipped"
            }
            continue
        }
        $r = @{}
        foreach ($k in $profEnv.Keys) { $r[$k] = $profEnv[$k] }
        $r["MASQUE_BENCH_VIA"] = $via
        if ($p.id -match '^(h3|h2)$' -and $via -eq "tun") {
            $r["BENCH_UDP_PROBE"] = "1"
        }
        else {
            $r["BENCH_UDP_PROBE"] = "0"
        }

        Invoke-GenBenchConfig $r

        Stop-RemoteStack
        Start-Sleep -Seconds 2
        Start-RemoteStack
        try {
            $inboundWait = 22
            if ($r.ContainsKey("BENCH_TUN_WARMUP_SEC")) { $inboundWait = [int]$r["BENCH_TUN_WARMUP_SEC"] }
            $ibKind = Get-BenchInboundType
            if ($ibKind -eq "tun") {
                Wait-Tun $r $inboundWait
            }
            elseif ($ibKind -eq "socks") {
                Wait-SocksListen $inboundWait
            }
            else {
                throw 'Inbound type unknown after codegen (expected tun or socks)'
            }
            if (Test-ConnectIpProfile $r) {
                $cipWait = 60
                if ($r.ContainsKey("BENCH_CONNECT_IP_WARMUP_SEC")) { $cipWait = [int]$r["BENCH_CONNECT_IP_WARMUP_SEC"] }
                Wait-MasqueConnectIP $cipWait
            }
            Start-Sleep -Seconds 2
            $m = Invoke-BenchReport $r
            $udpCol = "-"
            if ($m.udp_ok -eq "1") { $udpCol = "OK $($m.udp_mbit)" }
            elseif ($m.udp_ok -eq "0") { $udpCol = "FAIL" }
            $row = [ordered]@{
                profile = $p.id
                via     = $via
                desc    = $p.label
                status  = if ($m.ok) { "OK" } else { "FAIL" }
                up      = if ($m.up) { "$($m.up)" } else { "-" }
                down    = if ($m.down) { "$($m.down)" } else { "-" }
                udp     = $udpCol
            }
            if (-not $m.ok -and $m.err) { $row["note"] = $m.err }
            $results += [pscustomobject]$row
        }
        catch {
            $results += [pscustomobject]@{
                profile = $p.id
                via     = $via
                desc    = $p.label
                status  = "FAIL"
                up      = "-"
                down    = "-"
                udp     = "-"
                note    = $_.Exception.Message
            }
        }
        Stop-RemoteStack
        Start-Sleep -Seconds 2
    }
}

Write-Host ""
$bvNote = if ([string]::IsNullOrWhiteSpace($BenchVia)) { "tun" } else { $BenchVia }
$hdrLine = 'MASQUE matrix  bench_via={0}  endpoint={1}  VPS={2}:{3}  iperf={4}:{5}  {6} sec TCP; UDP probe = dig @{7} port {8} (CONNECT-UDP)' -f `
    $bvNote, $base['MASQUE_ENDPOINT_TAG'], $base['MASQUE_SERVER'], $base['MASQUE_SERVER_PORT'], `
    $base['BENCH_IPERF_TARGET_HOST'], $base['BENCH_IPERF_TARGET_PORT'], $base['IPERF_DURATION_SEC'], `
    $base['BENCH_IPERF_TARGET_HOST'], $base['BENCH_IPERF_TARGET_PORT']
Write-Host $hdrLine
Write-Host ""
$results | Format-Table -Property profile, via, status, up, down, udp, desc, note -AutoSize
Write-Host 'up/down = Mbit/s TCP (iperf3). udp = CONNECT-UDP probe (dig UDP to same host:port as TCP; icmp/refused counts as delivered).'
Write-Host ""

$h2fail = @($results | Where-Object { $_.profile -eq 'h2' -and $_.status -eq 'FAIL' })
if ($h2fail.Count -gt 0) {
    Write-Host ('H2 profiles failed: ensure VPS sing-box image/core includes this fork (RFC 8441 Extended CONNECT is enabled at startup via internal/http2xconnect). Port {0} must terminate MASQUE HTTP/2 (no proxy stripping CONNECT-UDP).' -f $base['MASQUE_SERVER_PORT'])
    Write-Host '  Redeploy server after rebuilding core: cd vendor/s-ui; python run.py deploy — or systemd sing-box binary from hiddify-core + Build-MasquePerfLab.'
    Write-Host '  Docker bench client still accepts GODEBUG=http2xconnect=1 (compose remote.yml); redundant when core already patches startup.'
    Write-Host ""
}
$cipfail = @($results | Where-Object { $_.profile -match 'connect-ip' -and $_.status -eq 'FAIL' })
if ($cipfail.Count -gt 0) {
    Write-Host 'CONNECT-IP failed: check docker logs masque-perf-remote-client (open_ip_session, netstack) and s-ui-local (connect-ip policy drops).'
    Write-Host '  Server on 0.0.0.0 needs sing-box with relaxed CONNECT-IP :authority (deploy vendor/s-ui) or explicit template_ip with public host.'
    Write-Host ""
}

$failed = @($results | Where-Object { $_.status -eq 'FAIL' })
if ($failed.Count -gt 0) { exit 1 }
exit 0
