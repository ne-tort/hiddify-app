# Portable bundle: mirror runner\Release, then always overlay data/DLLs/plugins/core from flutter build
# (Release\data is often a junction; plain Copy-Item can leave an empty tree after move).
Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

function Stop-ProcessesLockingPortable {
    param([string]$PortableDirectory)
    $portableExe = (Join-Path $PortableDirectory "Hiddify.exe").ToLowerInvariant()
    $portableCli = (Join-Path $PortableDirectory "HiddifyCli.exe").ToLowerInvariant()

    Get-CimInstance Win32_Process -Filter "Name='Hiddify.exe' OR Name='HiddifyCli.exe'" -ErrorAction SilentlyContinue |
        ForEach-Object {
            $path = $_.ExecutablePath
            if ($null -eq $path) { return }
            $p = $path.ToLowerInvariant()
            if ($p -eq $portableExe -or $p -eq $portableCli) {
                Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue
            }
        }
}

function Invoke-RobocopyMirror {
    param([string]$From, [string]$To)
    if (-not (Test-Path -LiteralPath $From)) {
        throw "Robocopy source missing: $From"
    }
    New-Item -ItemType Directory -Path $To -Force | Out-Null
    $rc = Join-Path $env:SystemRoot "System32\robocopy.exe"
    & $rc $From $To /E /R:2 /W:1 /NFL /NDL /NJH /NJS
    if ($LASTEXITCODE -ge 8) {
        throw "robocopy failed (exit $LASTEXITCODE): $From -> $To"
    }
}

function Assert-FileExists {
    param([string]$Path, [string]$Hint)
    if (-not (Test-Path -LiteralPath $Path)) {
        throw "Missing $Path. $Hint"
    }
}

$repoRoot = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
$releaseDir = Join-Path $repoRoot "build\windows\x64\runner\Release"
$buildX64 = Join-Path $repoRoot "build\windows\x64"
$flutterEphemeral = Join-Path $repoRoot "windows\flutter\ephemeral"
$flutterAssets = Join-Path $repoRoot "build\flutter_assets"
$appSo = Join-Path $repoRoot "build\windows\app.so"
$nativeAssetsSrc = Join-Path $repoRoot "build\native_assets"
$coreDir = Join-Path $repoRoot "hiddify-core\bin"
$portableDir = Join-Path $repoRoot "portable\windows-x64"
$sentryDll = Join-Path $buildX64 "_deps\sentry-native-build\Release\sentry.dll"

$exe = Join-Path $releaseDir "Hiddify.exe"
Assert-FileExists $exe "Run: flutter build windows [--no-pub]"
Assert-FileExists (Join-Path $flutterEphemeral "flutter_windows.dll") "Flutter ephemeral missing; run flutter build windows."
Assert-FileExists (Join-Path $flutterEphemeral "icudtl.dat") "Flutter ephemeral missing; run flutter build windows."
Assert-FileExists $flutterAssets "Run: flutter build windows [--no-pub]"
Assert-FileExists $appSo "Run: flutter build windows [--no-pub]"
Assert-FileExists (Join-Path $coreDir "hiddify-core.dll") "Build hiddify-core or place DLLs in hiddify-core\bin"
Assert-FileExists (Join-Path $coreDir "libcronet.dll") "Build hiddify-core or place DLLs in hiddify-core\bin"
Assert-FileExists (Join-Path $coreDir "HiddifyCli.exe") "Build hiddify-core or place HiddifyCli.exe in hiddify-core\bin"

$staging = Join-Path $env:TEMP ("hiddify-portable-" + [guid]::NewGuid().ToString("n"))
New-Item -ItemType Directory -Path $staging -Force | Out-Null

# 1) Best-effort full tree from Release (exe, plugin CMake outputs, etc.)
Invoke-RobocopyMirror -From $releaseDir -To $staging

# 2) Real files for data/ and engine DLLs (do not rely on junctions in Release)
$dataDir = Join-Path $staging "data"
New-Item -ItemType Directory -Path $dataDir -Force | Out-Null

Copy-Item -LiteralPath (Join-Path $releaseDir "Hiddify.exe") -Destination (Join-Path $staging "Hiddify.exe") -Force
Copy-Item -LiteralPath $appSo -Destination (Join-Path $dataDir "app.so") -Force

$dstAssets = Join-Path $dataDir "flutter_assets"
if (Test-Path -LiteralPath $dstAssets) {
    Remove-Item -LiteralPath $dstAssets -Recurse -Force
}
Copy-Item -LiteralPath $flutterAssets -Destination $dstAssets -Recurse -Force

Copy-Item -LiteralPath (Join-Path $flutterEphemeral "flutter_windows.dll") -Destination (Join-Path $staging "flutter_windows.dll") -Force
Copy-Item -LiteralPath (Join-Path $flutterEphemeral "icudtl.dat") -Destination (Join-Path $dataDir "icudtl.dat") -Force

if (Test-Path -LiteralPath $nativeAssetsSrc) {
    $na = @(Get-ChildItem -LiteralPath $nativeAssetsSrc -Recurse -File -ErrorAction SilentlyContinue)
    if ($na.Count -gt 0) {
        $dstNa = Join-Path $dataDir "native_assets"
        if (Test-Path -LiteralPath $dstNa) {
            Remove-Item -LiteralPath $dstNa -Recurse -Force
        }
        Copy-Item -LiteralPath $nativeAssetsSrc -Destination $dstNa -Recurse -Force
    }
}

Copy-Item -LiteralPath (Join-Path $coreDir "hiddify-core.dll") -Destination (Join-Path $staging "hiddify-core.dll") -Force
Copy-Item -LiteralPath (Join-Path $coreDir "libcronet.dll") -Destination (Join-Path $staging "libcronet.dll") -Force
Copy-Item -LiteralPath (Join-Path $coreDir "HiddifyCli.exe") -Destination (Join-Path $staging "HiddifyCli.exe") -Force

$pluginDlls = Get-ChildItem -Path (Join-Path $buildX64 "plugins") -Filter "*.dll" -Recurse -File -ErrorAction SilentlyContinue |
    Where-Object { $_.DirectoryName -match "\\Release$" }
foreach ($dll in $pluginDlls) {
    Copy-Item -LiteralPath $dll.FullName -Destination (Join-Path $staging $dll.Name) -Force
}

if (Test-Path -LiteralPath $sentryDll) {
    Copy-Item -LiteralPath $sentryDll -Destination (Join-Path $staging "sentry.dll") -Force
}

# Sanity check
Assert-FileExists (Join-Path $staging "flutter_windows.dll") "Overlay failed."
Assert-FileExists (Join-Path $dataDir "app.so") "Overlay failed."
Assert-FileExists (Join-Path $dataDir "icudtl.dat") "Overlay failed."
$faFiles = @(Get-ChildItem -LiteralPath $dstAssets -File -ErrorAction SilentlyContinue)
if ($faFiles.Count -lt 1) {
    throw "flutter_assets looks empty after copy. Check build\flutter_assets."
}

if (Test-Path -LiteralPath $portableDir) {
    Stop-ProcessesLockingPortable -PortableDirectory $portableDir
    Start-Sleep -Milliseconds 500
    try {
        Remove-Item -LiteralPath $portableDir -Recurse -Force
    } catch {
        throw @"
Cannot remove (folder in use): $portableDir
Close Hiddify/HiddifyCli from that folder and Explorer on that path, then retry.

Staging copy is ready here:
$staging
"@
    }
}

New-Item -ItemType Directory -Path (Split-Path -Parent $portableDir) -Force | Out-Null
Move-Item -LiteralPath $staging -Destination $portableDir

Write-Host "Portable: $portableDir" -ForegroundColor Green
Write-Host "Run: .\scripts\run-windows-release.ps1   or   $portableDir\Hiddify.exe" -ForegroundColor Green
