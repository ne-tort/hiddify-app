param(
  [string]$RuntimeDir = "",
  [int]$Count = 5
)

$ErrorActionPreference = "Stop"

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$rootDir = Resolve-Path (Join-Path $scriptDir "..")
if ([string]::IsNullOrWhiteSpace($RuntimeDir)) {
  $RuntimeDir = Join-Path $rootDir "runtime"
}
New-Item -ItemType Directory -Path $RuntimeDir -Force | Out-Null

$benchCmd = 'go test -run "^$" -bench "Benchmark(MemEngineHandleIngress|L3RouterEndToEndSyntheticTransport|SyntheticTransportOnly)" -benchmem -count {0} ./experimental/l3router' -f $Count

$stamp = Get-Date -Format "yyyyMMddTHHmmssZ"
$benchOut = Join-Path $RuntimeDir ("phase0_synthetic_bench_" + $stamp + ".txt")
$summaryOut = Join-Path $RuntimeDir ("phase0_baseline_summary_" + $stamp + ".json")

Push-Location (Resolve-Path (Join-Path $rootDir "..\..\.."))
try {
  Invoke-Expression $benchCmd | Tee-Object -FilePath $benchOut | Out-Null
} finally {
  Pop-Location
}

$lines = Get-Content -Path $benchOut
$rows = @()
foreach ($line in $lines) {
  if ($line -match '^Benchmark(?<name>[A-Za-z0-9_\-/]+)-\d+\s+\d+\s+(?<ns>[0-9.]+) ns/op\s+(?<mb>[0-9.]+) MB/s\s+(?<b>[0-9]+) B/op\s+(?<a>[0-9]+) allocs/op$') {
    $rows += [pscustomobject]@{
      Name   = $matches.name
      Ns     = [double]$matches.ns
      MBps   = [double]$matches.mb
      Bytes  = [int]$matches.b
      Allocs = [int]$matches.a
    }
  }
}

$grouped = $rows | Group-Object Name | ForEach-Object {
  [pscustomobject]@{
    name             = $_.Name
    samples          = $_.Count
    avg_ns_per_op    = [math]::Round((($_.Group | Measure-Object Ns -Average).Average), 2)
    avg_mb_per_sec   = [math]::Round((($_.Group | Measure-Object MBps -Average).Average), 2)
    avg_bytes_per_op = [math]::Round((($_.Group | Measure-Object Bytes -Average).Average), 2)
    avg_allocs_per_op = [math]::Round((($_.Group | Measure-Object Allocs -Average).Average), 2)
  }
}

$e2e = @{}
$transport = @{}
$mem = $null
foreach ($g in $grouped) {
  if ($g.name -like "L3RouterEndToEndSyntheticTransport/*") {
    $profile = $g.name.Split("/")[1]
    $e2e[$profile] = $g
  } elseif ($g.name -like "SyntheticTransportOnly/*") {
    $profile = $g.name.Split("/")[1]
    $transport[$profile] = $g
  } elseif ($g.name -eq "MemEngineHandleIngress") {
    $mem = $g
  }
}

$profiles = @()
foreach ($p in $e2e.Keys) {
  $with = $e2e[$p]
  $without = $transport[$p]
  if (-not $without) {
    continue
  }
  $overNs = [math]::Round(($with.avg_ns_per_op - $without.avg_ns_per_op), 2)
  $overPct = 0
  if ($without.avg_ns_per_op -gt 0) {
    $overPct = [math]::Round(($overNs / $without.avg_ns_per_op) * 100, 2)
  }
  $profiles += [pscustomobject]@{
    profile                       = $p
    with_l3router_avg_ns          = $with.avg_ns_per_op
    without_l3router_avg_ns       = $without.avg_ns_per_op
    with_l3router_avg_mb_per_sec  = $with.avg_mb_per_sec
    without_l3router_avg_mb_per_sec = $without.avg_mb_per_sec
    l3router_overhead_ns          = $overNs
    l3router_overhead_pct         = $overPct
    with_allocs                   = $with.avg_allocs_per_op
    without_allocs                = $without.avg_allocs_per_op
  }
}

$result = [ordered]@{
  timestamp_utc               = (Get-Date).ToUniversalTime().ToString("o")
  benchmark_command           = $benchCmd
  source_benchmark_file       = (Split-Path -Leaf $benchOut)
  benchmark_samples_per_case  = $Count
  packet_size_bytes           = 32
  mem_engine_handle_ingress   = $mem
  synthetic_profiles          = $profiles | Sort-Object profile
}

$result | ConvertTo-Json -Depth 8 | Set-Content -Path $summaryOut -Encoding UTF8

Write-Host "Phase 0 raw benchmark: $benchOut"
Write-Host "Phase 0 summary:       $summaryOut"
