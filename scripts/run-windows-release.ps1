# Run GUI with CWD = build\...\Release (same layout as after flutter build windows / CMake install).
Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
$releaseDir = Join-Path $repoRoot "build\windows\x64\runner\Release"
$exe = Join-Path $releaseDir "Hiddify.exe"

if (-not (Test-Path -LiteralPath $exe)) {
    throw "Not found: $exe. Run first: flutter build windows [--no-pub]"
}

Set-Location -LiteralPath $releaseDir
Write-Host "CWD: $releaseDir" -ForegroundColor DarkGray
& $exe @args
