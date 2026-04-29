$ErrorActionPreference = "Stop"

$StandDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$NaiveExe = Join-Path $StandDir "naive\naiveproxy-v147.0.7727.49-1-win-x64\naive.exe"
$ConfigPath = Join-Path $StandDir "config.json"

if (-not (Test-Path $NaiveExe)) {
  throw "naive.exe not found: $NaiveExe"
}

if (-not (Test-Path $ConfigPath)) {
  throw "config.json not found: $ConfigPath"
}

Set-Location $StandDir
& $NaiveExe $ConfigPath
