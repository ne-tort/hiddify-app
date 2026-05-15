# MASQUE perf lab: локальный Docker (server+client+iperf) или удалённый VPS (клиент → реальный MASQUE + iperf через SOCKS).
#
# Локально:
#   powershell -NoProfile -File scripts/Build-MasquePerfLab.ps1
#   powershell -NoProfile -File scripts/Run-MasquePerfLab.ps1 -Mode Local
#
# Реальный VPS (по умолчанию закоммиченный тестовый стенд remote.stand.env; свой файл — -RemoteEnvPath):
#   powershell -NoProfile -File scripts/Run-MasquePerfLab.ps1 -Mode Remote
#
# Остановка:
#   cd docker/masque-perf-lab && docker compose down && docker compose -f docker-compose.remote.yml down
param(
    [ValidateSet("Local", "Remote")]
    [string]$Mode = "Local",
    [string]$RemoteEnvPath = "",
    [string]$HttpLayer = ""
)

$ErrorActionPreference = "Stop"
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$LabRoot = Join-Path $RepoRoot "docker\masque-perf-lab"
$genScript = Join-Path $RepoRoot "scripts\Gen-MasquePerfRemoteConfig.ps1"

if (-not (Test-Path (Join-Path $LabRoot "artifacts\sing-box-linux-amd64"))) {
    throw "Missing sing-box binary. Run: powershell -NoProfile -File scripts/Build-MasquePerfLab.ps1"
}

$docker = Get-Command docker -ErrorAction SilentlyContinue
if (-not $docker) { throw "docker not in PATH" }

function Read-DotEnv([string]$path) {
    $h = @{}
    if (-not (Test-Path $path)) { return $h }
    Get-Content $path -Encoding UTF8 | ForEach-Object {
        $line = $_.Trim()
        if ($line.Length -eq 0 -or $line.StartsWith("#")) { return }
        $eq = $line.IndexOf("=")
        if ($eq -lt 1) { return }
        $k = $line.Substring(0, $eq).Trim()
        $v = $line.Substring($eq + 1).Trim()
        $h[$k] = $v
    }
    return $h
}

function Merge-RemoteEnv {
    param(
        [hashtable]$Base,
        [hashtable]$Override
    )
    $m = @{}
    foreach ($k in $Base.Keys) { $m[$k] = $Base[$k] }
    foreach ($k in $Override.Keys) { $m[$k] = $Override[$k] }
    return $m
}

function Test-MasquePerfPreflight {
    param(
        [string]$IperfHost,
        [string]$IperfPort,
        [string]$MasqueVps
    )
    Write-Host "[preflight] iperf3 на ${IperfHost}:${IperfPort} (SSH)..."
    $unit = "iperf3-masque-${IperfPort}"
    $sshIperf = @(
        "systemctl is-active ${unit} 2>/dev/null || systemctl is-active iperf3 2>/dev/null || true",
        "ss -tln | grep -q ':${IperfPort} '"
    ) -join "; "
    $oldEap = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    try {
        & ssh -o BatchMode=yes -o ConnectTimeout=12 $IperfHost $sshIperf 2>&1 | Write-Host
        if ($LASTEXITCODE -ne 0) {
            throw "Preflight: iperf on ${IperfHost}:${IperfPort} down (systemd/ss). See AGENTS 15.1b"
        }

        Write-Host "[preflight] TCP from MASQUE VPS ${MasqueVps} to ${IperfHost}:${IperfPort}..."
        & ssh -o BatchMode=yes -o ConnectTimeout=12 $MasqueVps "command -v nc >/dev/null && nc -z -w 5 ${IperfHost} ${IperfPort}" 2>&1 | Write-Host
        if ($LASTEXITCODE -ne 0) {
            throw "Preflight: no TCP from ${MasqueVps} to ${IperfHost}:${IperfPort} (MASQUE 502 / firewall)"
        }
    }
    finally {
        $ErrorActionPreference = $oldEap
    }
    Write-Host "[preflight] OK"
}

function Wait-RemoteMasqueSocks {
    param(
        [string]$ContainerName,
        [int]$MaxSec = 35
    )
    for ($i = 0; $i -lt $MaxSec; $i++) {
        $cid = (& docker ps -qf "name=$ContainerName" 2>$null)
        if ([string]::IsNullOrWhiteSpace($cid)) {
            throw "Client container not running: $ContainerName"
        }
        & docker exec $cid sh -c "ss -tln 2>/dev/null | grep -q ':1080 '" 2>$null | Out-Null
        if ($LASTEXITCODE -eq 0) {
            Write-Host "Remote: SOCKS 127.0.0.1:1080 ready (${i}s)"
            return
        }
        Start-Sleep -Seconds 1
    }
    throw "SOCKS :1080 not listening after ${MaxSec}s (MASQUE handshake / sing-box config)"
}

function Invoke-BenchAgainstClient([string]$clientContainerName, [string]$iperfHost, [string]$iperfPort) {
    $benchSrc = (Resolve-Path (Join-Path $LabRoot "bench\run-bench.sh")).Path
    $benchScript = Join-Path $env:TEMP "masque-perf-run-bench.sh"
    $raw = [System.IO.File]::ReadAllText($benchSrc) -replace "`r`n", "`n"
    [System.IO.File]::WriteAllText($benchScript, $raw, [System.Text.UTF8Encoding]::new($false))

    $cid = (& docker ps -qf "name=$clientContainerName")
    if ([string]::IsNullOrWhiteSpace($cid)) {
        throw "Client container not running: $clientContainerName"
    }

    $iperfArgs = if ($env:IPERF_ARGS) { $env:IPERF_ARGS } else { "" }
    $benchTimeout = if ($env:BENCH_TIMEOUT_SEC) { $env:BENCH_TIMEOUT_SEC } else { "120" }
    $envArgs = @(
        "-e", "BENCH_WAIT_SOCKS_SEC=2",
        "-e", "IPERF_TARGET_HOST=$iperfHost",
        "-e", "IPERF_TARGET_PORT=$iperfPort",
        "-e", "BENCH_TIMEOUT_SEC=$benchTimeout",
        "-e", "IPERF_CONNECT_TIMEOUT_MS=15000"
    )
    if ($iperfArgs) { $envArgs += "-e", "IPERF_ARGS=$iperfArgs" }

    $oldEap = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    try {
        & docker run --rm @envArgs `
            --network "container:$cid" `
            -v "${benchScript}:/bench/run-bench.sh:ro" `
            masque-perf-lab:local /bin/sh /bench/run-bench.sh 2>&1 | Write-Host
        return $LASTEXITCODE
    }
    finally {
        $ErrorActionPreference = $oldEap
    }
}

function Invoke-CurlBenchAgainstClient([string]$clientContainerName, [string]$curlUrl) {
    $benchSrc = (Resolve-Path (Join-Path $LabRoot "bench\run-bench-curl.sh")).Path
    $benchScript = Join-Path $env:TEMP "masque-perf-run-bench-curl.sh"
    $raw = [System.IO.File]::ReadAllText($benchSrc) -replace "`r`n", "`n"
    [System.IO.File]::WriteAllText($benchScript, $raw, [System.Text.UTF8Encoding]::new($false))

    $cid = (& docker ps -qf "name=$clientContainerName")
    if ([string]::IsNullOrWhiteSpace($cid)) {
        throw "Client container not running: $clientContainerName"
    }

    $envArgs = @(
        "-e", "BENCH_WAIT_SOCKS_SEC=2",
        "-e", "CURL_BENCH_URL=$curlUrl"
    )

    $oldEap = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    try {
        & docker run --rm @envArgs `
            --network "container:$cid" `
            -v "${benchScript}:/bench/run-bench-curl.sh:ro" `
            masque-perf-lab:local /bin/sh /bench/run-bench-curl.sh 2>&1 | Write-Host
        return $LASTEXITCODE
    }
    finally {
        $ErrorActionPreference = $oldEap
    }
}

Push-Location $LabRoot
try {
    $oldEap = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    try {
        & docker compose down --remove-orphans 2>&1 | Out-Null
        & docker compose -f "docker-compose.remote.yml" down --remove-orphans 2>&1 | Out-Null
    }
    finally {
        $ErrorActionPreference = $oldEap
    }

    if ($Mode -eq "Local") {
        if (-not (Test-Path (Join-Path $LabRoot "certs\masque-cert.pem"))) {
            throw "Missing certs for Local mode. Run: powershell -NoProfile -File scripts/Build-MasquePerfLab.ps1"
        }
        $ErrorActionPreference = "Continue"
        try {
            & docker compose up -d masque-server-core iperf-server masque-client-core 2>&1 | Out-Null
            if ($LASTEXITCODE -ne 0) { throw "docker compose up failed (exit $LASTEXITCODE)" }
        }
        finally {
            $ErrorActionPreference = $oldEap
        }

        $wait = if ($env:CLIENT_WARMUP_SEC) { [int]$env:CLIENT_WARMUP_SEC } else { 14 }
        Write-Host "Local mode: waiting ${wait}s for SOCKS..."
        Start-Sleep -Seconds $wait

        $benchExit = Invoke-BenchAgainstClient "masque-perf-masque-client" "172.30.99.2" "5201"

        Write-Host "--- compose logs (tail) ---"
        $ErrorActionPreference = "Continue"
        try { & docker compose logs --tail 80 2>&1 | Write-Host } finally { $ErrorActionPreference = $oldEap }

        if ($benchExit -ne 0) { throw "bench exit code $benchExit" }
        Write-Host "OK (Local). Stack running. Down: docker compose down (in $LabRoot)"
        return
    }

    # --- Remote: remote.stand.env + remote.credentials.env (credentials перекрывает stand) ---
    $standPath = Join-Path $LabRoot "remote.stand.env"
    $credPath = Join-Path $LabRoot "remote.credentials.env"
    if (-not [string]::IsNullOrWhiteSpace($RemoteEnvPath)) {
        $genEnvPath = $RemoteEnvPath
        $r = Read-DotEnv $genEnvPath
    }
    else {
        $r = Read-DotEnv $standPath
        $genEnvPath = $standPath
        if (Test-Path $credPath) {
            $r = Merge-RemoteEnv $r (Read-DotEnv $credPath)
            $genEnvPath = Join-Path $env:TEMP "masque-perf-remote-merged.env"
            $r.GetEnumerator() | Sort-Object Name | ForEach-Object { "$($_.Key)=$($_.Value)" } | Set-Content -Encoding UTF8 $genEnvPath
        }
    }
    if ($HttpLayer) {
        $r["MASQUE_HTTP_LAYER"] = $HttpLayer
        if ($genEnvPath -eq $standPath -and -not (Test-Path $credPath)) {
            $genEnvPath = Join-Path $env:TEMP "masque-perf-remote-merged.env"
            $r.GetEnumerator() | Sort-Object Name | ForEach-Object { "$($_.Key)=$($_.Value)" } | Set-Content -Encoding UTF8 $genEnvPath
        }
        elseif ($genEnvPath -like "*masque-perf-remote-merged.env") {
            $r.GetEnumerator() | Sort-Object Name | ForEach-Object { "$($_.Key)=$($_.Value)" } | Set-Content -Encoding UTF8 $genEnvPath
        }
    }
    elseif ($env:MASQUE_HTTP_LAYER) {
        $r["MASQUE_HTTP_LAYER"] = $env:MASQUE_HTTP_LAYER
        $genEnvPath = Join-Path $env:TEMP "masque-perf-remote-merged.env"
        $r.GetEnumerator() | Sort-Object Name | ForEach-Object { "$($_.Key)=$($_.Value)" } | Set-Content -Encoding UTF8 $genEnvPath
    }
    & $genScript -RepoRoot $RepoRoot -EnvPath $genEnvPath
    if ($LASTEXITCODE -ne 0) { throw "Gen-MasquePerfRemoteConfig failed" }

    $benchHost = if ($r["BENCH_IPERF_TARGET_HOST"]) { $r["BENCH_IPERF_TARGET_HOST"] } else { "163.5.180.181" }
    $benchPort = if ($r["BENCH_IPERF_TARGET_PORT"]) { $r["BENCH_IPERF_TARGET_PORT"] } else { "5201" }
    $masqueVps = if ($r["MASQUE_SERVER"]) { $r["MASQUE_SERVER"] } else { "193.233.216.26" }

    if ($env:MASQUE_PERF_SKIP_PREFLIGHT -ne "1") {
        Test-MasquePerfPreflight -IperfHost $benchHost -IperfPort $benchPort -MasqueVps $masqueVps
    }
    Write-Host "Remote bench target: ${benchHost}:${benchPort} (dedicated iperf, not public speedtest)"
    # Переменная окружения имеет приоритет над remote*.env (удобно для эталонных прогонов из CI/ручного вызова).
    if (-not $env:IPERF_ARGS -and $r["IPERF_ARGS"]) { $env:IPERF_ARGS = $r["IPERF_ARGS"] }

    $ErrorActionPreference = "Continue"
    try {
        & docker compose -f "docker-compose.remote.yml" up -d masque-remote-client 2>&1 | Out-Null
        if ($LASTEXITCODE -ne 0) { throw "docker compose remote up failed (exit $LASTEXITCODE)" }
    }
    finally {
        $ErrorActionPreference = $oldEap
    }

    $benchMode = if ($r["BENCH_MODE"]) { $r["BENCH_MODE"].Trim().ToLowerInvariant() } else { "iperf" }
    $curlUrl = if ($r["CURL_BENCH_URL"]) { $r["CURL_BENCH_URL"] } else { "https://speed.cloudflare.com/__down?bytes=80000000" }

    $maxSocksWait = if ($env:CLIENT_WARMUP_SEC) { [int]$env:CLIENT_WARMUP_SEC } else { 35 }
    Write-Host "Remote mode: wait SOCKS up to ${maxSocksWait}s (MASQUE → $masqueVps)... BENCH_MODE=$benchMode"
    Wait-RemoteMasqueSocks -ContainerName "masque-perf-remote-client" -MaxSec $maxSocksWait

    if ($benchMode -eq "curl") {
        $benchExit = Invoke-CurlBenchAgainstClient "masque-perf-remote-client" $curlUrl
    }
    else {
        $benchExit = Invoke-BenchAgainstClient "masque-perf-remote-client" $benchHost $benchPort
    }

    $curlFallback = $r["BENCH_IPERF_CURL_FALLBACK"]
    if ($benchExit -ne 0 -and $benchMode -eq "iperf" -and $curlFallback -eq "1") {
        Write-Host "WARN: iperf failed; BENCH_IPERF_CURL_FALLBACK=1 - curl smoke only (not AGENTS 15.1c baseline)"
        $benchExit = Invoke-CurlBenchAgainstClient "masque-perf-remote-client" $curlUrl
    }

    Write-Host "--- remote client logs (tail) ---"
    $ErrorActionPreference = "Continue"
    try { & docker compose -f "docker-compose.remote.yml" logs --tail 120 2>&1 | Write-Host } finally { $ErrorActionPreference = $oldEap }

    if ($benchExit -ne 0) { throw "bench exit code $benchExit" }
    Write-Host "OK (Remote). Client may still run. Down: docker compose -f docker-compose.remote.yml down (in $LabRoot)"
}
finally {
    Pop-Location
}
