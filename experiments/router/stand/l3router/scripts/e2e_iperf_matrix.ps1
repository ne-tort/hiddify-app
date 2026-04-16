[CmdletBinding()]
param(
  [int]$DurationSec = 20,
  [string]$ServerA = "l3router-smb-client1",
  [string]$ServerC = "l3router-smb-client2",
  [string]$IpA = "10.0.0.2",
  [string]$IpC = "10.0.0.4",
  [int]$Port = 5201
)

$ErrorActionPreference = "Stop"
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$standRoot = Split-Path -Parent $scriptDir
$runtimeDir = Join-Path $standRoot "runtime"
New-Item -ItemType Directory -Force -Path $runtimeDir | Out-Null
$rawDir = Join-Path $runtimeDir "iperf-raw"
New-Item -ItemType Directory -Force -Path $rawDir | Out-Null
$reportPath = Join-Path $runtimeDir "iperf_matrix_latest.json"

function Kill-Iperf([string]$container) {
  docker exec $container sh -lc "pkill iperf3 >/dev/null 2>&1 || true" | Out-Null
}

function Start-IperfServer([string]$container, [int]$port) {
  Kill-Iperf $container
  docker exec -d $container sh -lc "iperf3 -s -1 -p $port >/tmp/iperf-server.log 2>&1"
  Start-Sleep -Milliseconds 800
}

function Run-IperfCase(
  [string]$name,
  [string]$clientContainer,
  [string]$serverContainer,
  [string]$serverIp,
  [string]$clientArgs
) {
  Write-Host "[iperf] case=$name"
  Start-IperfServer $serverContainer $Port
  $cmd = "iperf3 -c $serverIp -p $Port $clientArgs -J"
  try {
    $raw = docker exec $clientContainer sh -lc $cmd
    if ($LASTEXITCODE -ne 0) {
      throw "docker exec failed with exit code $LASTEXITCODE"
    }
    $jsonText = ($raw -join "`n")
    if (-not $jsonText.TrimStart().StartsWith("{")) {
      throw "iperf3 did not return JSON output"
    }
    $obj = $jsonText | ConvertFrom-Json
    $rawPath = Join-Path $rawDir ("$name.json")
    Set-Content -Path $rawPath -Value $jsonText -Encoding UTF8

    $proto = $obj.start.test_start.protocol
    $duration = $obj.start.test_start.duration
    $status = "ok"
    $result = [ordered]@{
      name = $name
      protocol = $proto
      duration_sec = $duration
      args = $clientArgs
      raw_file = $rawPath
      status = $status
    }

    if (-not $proto) {
      throw "missing protocol in iperf3 JSON"
    }

    if ($proto -eq "TCP") {
      $sumRecv = $obj.end.sum_received
      $sumSent = $obj.end.sum_sent
      $bps = if ($sumRecv.bits_per_second) { $sumRecv.bits_per_second } else { $sumSent.bits_per_second }
      if (-not $bps) { throw "missing throughput in TCP result" }
      $result.throughput_mbit_per_sec = [Math]::Round($bps / 1000000.0, 2)
      $result.retransmits = if ($sumSent.retransmits -ne $null) { $sumSent.retransmits } else { 0 }
    } else {
      $sum = $obj.end.sum
      if (-not $sum) { $sum = $obj.end.sum_received }
      $bps = $sum.bits_per_second
      if (-not $bps) { throw "missing throughput in UDP result" }
      $result.throughput_mbit_per_sec = [Math]::Round($bps / 1000000.0, 2)
      $result.jitter_ms = if ($sum.jitter_ms -ne $null) { [double]$sum.jitter_ms } else { 0.0 }
      $result.lost_percent = if ($sum.lost_percent -ne $null) { [double]$sum.lost_percent } else { 0.0 }
      $result.packets = if ($sum.packets -ne $null) { [int]$sum.packets } else { 0 }
      $result.lost_packets = if ($sum.lost_packets -ne $null) { [int]$sum.lost_packets } else { 0 }
    }
    return $result
  } catch {
    return [ordered]@{
      name = $name
      protocol = "unknown"
      duration_sec = 0
      args = $clientArgs
      status = "failed"
      error = $_.Exception.Message
    }
  } finally {
    Kill-Iperf $serverContainer
    Kill-Iperf $clientContainer
  }
}

$cases = @()
# TCP matrix
$cases += Run-IperfCase "tcp_c1_to_c2_single" $ServerA $ServerC $IpC "-t $DurationSec"
$cases += Run-IperfCase "tcp_c2_to_c1_single" $ServerC $ServerA $IpA "-t $DurationSec"
$cases += Run-IperfCase "tcp_c1_to_c2_reverse" $ServerA $ServerC $IpC "-t $DurationSec -R"
$cases += Run-IperfCase "tcp_c1_to_c2_p4" $ServerA $ServerC $IpC "-t $DurationSec -P 4"
$cases += Run-IperfCase "tcp_c1_to_c2_p8" $ServerA $ServerC $IpC "-t $DurationSec -P 8"
$cases += Run-IperfCase "tcp_c1_to_c2_bidir" $ServerA $ServerC $IpC "-t $DurationSec --bidir"

# UDP matrix
$cases += Run-IperfCase "udp_c1_to_c2_20m_l1200" $ServerA $ServerC $IpC "-t $DurationSec -u -b 20M -l 1200"
$cases += Run-IperfCase "udp_c1_to_c2_50m_l1200" $ServerA $ServerC $IpC "-t $DurationSec -u -b 50M -l 1200"
$cases += Run-IperfCase "udp_c1_to_c2_100m_l1200" $ServerA $ServerC $IpC "-t $DurationSec -u -b 100M -l 1200"
$cases += Run-IperfCase "udp_c1_to_c2_20m_p4_l1200" $ServerA $ServerC $IpC "-t $DurationSec -u -b 20M -P 4 -l 1200"
$cases += Run-IperfCase "udp_c2_to_c1_20m_l1200" $ServerC $ServerA $IpA "-t $DurationSec -u -b 20M -l 1200"
$cases += Run-IperfCase "udp_c2_to_c1_50m_l1200" $ServerC $ServerA $IpA "-t $DurationSec -u -b 50M -l 1200"

$tcpCases = @($cases | Where-Object { $_.name -like "tcp_*" -and $_.status -eq "ok" })
$udpCases = @($cases | Where-Object { $_.name -like "udp_*" -and $_.status -eq "ok" })
$tcpRates = @()
foreach ($c in $tcpCases) { if ($c.Contains("throughput_mbit_per_sec")) { $tcpRates += [double]$c["throughput_mbit_per_sec"] } }
$udpRates = @()
foreach ($c in $udpCases) { if ($c.Contains("throughput_mbit_per_sec")) { $udpRates += [double]$c["throughput_mbit_per_sec"] } }
$udpLosses = @()
foreach ($c in $udpCases) { if ($c.Contains("lost_percent")) { $udpLosses += [double]$c["lost_percent"] } }
$tcpAvg = if ($tcpRates.Count -gt 0) { [Math]::Round((($tcpRates | Measure-Object -Average).Average), 2) } else { 0.0 }
$udpAvg = if ($udpRates.Count -gt 0) { [Math]::Round((($udpRates | Measure-Object -Average).Average), 2) } else { 0.0 }
$udpLossAvg = if ($udpLosses.Count -gt 0) { [Math]::Round((($udpLosses | Measure-Object -Average).Average), 2) } else { 0.0 }

$report = [ordered]@{
  mode = "iperf3-matrix-over-l3router"
  duration_sec = $DurationSec
  timestamp_utc = (Get-Date).ToUniversalTime().ToString("o")
  summary = [ordered]@{
    tcp_cases_ok = $tcpCases.Count
    udp_cases_ok = $udpCases.Count
    tcp_cases_with_rate = $tcpRates.Count
    udp_cases_with_rate = $udpRates.Count
    tcp_avg_mbit_per_sec = $tcpAvg
    udp_avg_mbit_per_sec = $udpAvg
    udp_avg_lost_percent = $udpLossAvg
  }
  cases = $cases
}

$json = $report | ConvertTo-Json -Depth 8
Set-Content -Path $reportPath -Value $json -Encoding UTF8
Write-Host "[iperf] report: $reportPath"
Write-Output $json
