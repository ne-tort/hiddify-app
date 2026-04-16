param(
  [string]$ControllerUrl = "http://127.0.0.1:9090",
  [string]$ProxyTag = "l3router",
  [string]$RuntimeDir = "",
  [switch]$Offline,
  [switch]$EnforceNoHotRouteUpsert = $true
)

$ErrorActionPreference = "Stop"

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$rootDir = Resolve-Path (Join-Path $scriptDir "..")
if ([string]::IsNullOrWhiteSpace($RuntimeDir)) {
  $RuntimeDir = Join-Path $rootDir "runtime"
}
New-Item -ItemType Directory -Path $RuntimeDir -Force | Out-Null

$stamp = Get-Date -Format "yyyyMMddTHHmmssZ"
$outFile = Join-Path $RuntimeDir ("smoke_" + $stamp + ".json")

$configDir = Join-Path $rootDir "configs"
$serverConfig = Join-Path $configDir "server.l3router.static.json"
$clientAConfig = Join-Path $configDir "client-a.static.json"
$clientBConfig = Join-Path $configDir "client-b.static.json"

if ($Offline) {
  $server = Get-Content -Path $serverConfig -Raw | ConvertFrom-Json
  $clientA = Get-Content -Path $clientAConfig -Raw | ConvertFrom-Json
  $clientB = Get-Content -Path $clientBConfig -Raw | ConvertFrom-Json
  $peers = @($server.endpoints[0].peers)
  $result = [ordered]@{
    timestamp_utc = (Get-Date).ToUniversalTime().ToString("o")
    mode = "static-only-offline-smoke"
    controller = $ControllerUrl
    proxy_tag = $ProxyTag
    server_peers = $peers.Count
    peer_users = @($peers.user)
    has_overlay_destination = -not [string]::IsNullOrWhiteSpace($server.endpoints[0].overlay_destination)
    client_a_outbound = $clientA.outbounds[0].type
    client_b_outbound = $clientB.outbounds[0].type
    runtime_route_api_used = $false
  }
  $result | ConvertTo-Json -Depth 8 | Set-Content -Path $outFile -Encoding UTF8
  Write-Host "Offline smoke result saved to $outFile"
  exit 0
}

$metricsUrl = "$ControllerUrl/proxies/$ProxyTag/metrics"
$configsUrl = "$ControllerUrl/configs"

$configs = Invoke-RestMethod -Method Get -Uri $configsUrl
$metrics = Invoke-RestMethod -Method Get -Uri $metricsUrl

$result = [ordered]@{
  timestamp_utc = (Get-Date).ToUniversalTime().ToString("o")
  mode = "static-only"
  controller = $ControllerUrl
  proxy_tag = $ProxyTag
  static_load_ok = $metrics.metrics.StaticLoadOK
  static_load_error = $metrics.metrics.StaticLoadError
  control_upsert_ok = $metrics.metrics.ControlUpsertOK
  control_remove_ok = $metrics.metrics.ControlRemoveOK
  control_errors = $metrics.metrics.ControlErrors
  ingress_packets = $metrics.metrics.IngressPackets
  forward_packets = $metrics.metrics.ForwardPackets
  drop_packets = $metrics.metrics.DropPackets
  l3router_totals = $configs.l3router.totals
}

if ($EnforceNoHotRouteUpsert -and ($result.control_upsert_ok -gt 0 -or $result.control_remove_ok -gt 0)) {
  throw "Detected runtime route mutation (ControlUpsertOK/ControlRemoveOK > 0). Static-only stand is violated."
}

$result | ConvertTo-Json -Depth 8 | Set-Content -Path $outFile -Encoding UTF8
Write-Host "Smoke result saved to $outFile"
Write-Host ("StaticLoadOK={0}, StaticLoadError={1}, ControlUpsertOK={2}" -f $result.static_load_ok, $result.static_load_error, $result.control_upsert_ok)
