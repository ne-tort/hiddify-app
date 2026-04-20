# Скачивает wgcf (ViRb3) и wgcf-teams (poscat0x04) под Windows amd64.
# Запуск из PowerShell: .\download-wgcf.ps1

$ErrorActionPreference = "Stop"
$root = $PSScriptRoot
$wgcfVer = "v2.2.30"
$teamsVer = "v0.2.1"

New-Item -ItemType Directory -Force -Path $root | Out-Null

$wgcfUrl = "https://github.com/ViRb3/wgcf/releases/download/$wgcfVer/wgcf_2.2.30_windows_amd64.exe"
$teamsUrl = "https://github.com/poscat0x04/wgcf-teams/releases/download/$teamsVer/wgcf-teams-0.2.1-windows.zip"

Write-Host "Downloading wgcf..."
curl.exe -fL -o "$root\wgcf.exe" $wgcfUrl

Write-Host "Downloading wgcf-teams..."
curl.exe -fL -o "$root\wgcf-teams.zip" $teamsUrl
Expand-Archive -Path "$root\wgcf-teams.zip" -DestinationPath "$root\wgcf-teams" -Force

Write-Host "Done. wgcf.exe and wgcf-teams\wgcf-teams.exe"
