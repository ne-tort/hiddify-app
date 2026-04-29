$ErrorActionPreference = "Stop"

$Proxy = "socks5h://127.0.0.1:2080"
$Targets = @(
  "https://captive.apple.com/hotspot-detect.html",
  "https://www.google.com",
  "https://client-update.fastly.steamstatic.com"
)

foreach ($Url in $Targets) {
  Write-Host "==== $Url ===="
  curl.exe --proxy $Proxy --max-time 20 --silent --show-error --include $Url
  Write-Host ""
}
