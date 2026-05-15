#requires -Version 5.1
<#
.SYNOPSIS
  Windows portable build: core (Go/MinGW) + Flutter + copy to portable/, no Git Bash or GNU make.

.DESCRIPTION
  PowerShell-only: builds hiddify-core.dll and HiddifyCli.exe, runs pub get / build_runner / slang,
  flutter build windows --release, then robocopy /E from Release to portable (no full rm -rf of
  destination so hiddify_portable_data stays usable when the app was running).

.PARAMETER Mode
  Full, Core, Prepare, Sync, CoreRefresh (see script body).

.EXAMPLE
  powershell -ExecutionPolicy Bypass -File scripts/build-windows-portable.ps1
#>
[CmdletBinding()]
param(
    [ValidateSet('Full', 'Core', 'Prepare', 'Sync', 'CoreRefresh')]
    [string] $Mode = 'Full',

    [string] $PortableDst = 'portable\windows-x64\Hiddify',

    [string] $FlutterExe = $(if ($env:FLUTTER_ROOT) { Join-Path $env:FLUTTER_ROOT 'bin\flutter.bat' } else { 'flutter' }),

    [string] $MinGwGcc = 'x86_64-w64-mingw32-gcc',

    [string] $GoToolchain = $(if ($env:GOTOOLCHAIN) { $env:GOTOOLCHAIN } else { 'go1.25.6' })
)

$ErrorActionPreference = 'Stop'

function Assert-File([string] $Path, [string] $Message) {
    if (-not (Test-Path -LiteralPath $Path)) { throw $Message }
}

function Assert-Command([string] $Name, [string] $Hint) {
    if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
        throw "Command not in PATH: $Name. $Hint"
    }
}

function Get-HiddifyCoreBuildTags {
    param([string] $CoreRoot)
    # Fallback = CoreSingBoxBaseTags + with_naive (см. cmd/internal/build_shared/core_build_tags.go)
    $fallback = 'with_gvisor,with_quic,with_wireguard,with_awg,with_l3router,with_utls,with_clash_api,with_grpc,with_acme,with_masque,with_conntrack,badlinkname,tfogo_checklinkname0,with_tailscale,ts_omit_logtail,ts_omit_ssh,ts_omit_drive,ts_omit_taildrop,ts_omit_webclient,ts_omit_doctor,ts_omit_capture,ts_omit_kube,ts_omit_aws,ts_omit_synology,ts_omit_bird,with_naive_outbound'
    $helper = Join-Path $CoreRoot 'bin\print_core_build_tags.exe'
    Push-Location $CoreRoot
    try {
        if (-not (Test-Path -LiteralPath $helper)) {
            & go build -o $helper ./cmd/print_core_build_tags 2>&1 | Out-Null
        }
        if (Test-Path -LiteralPath $helper) {
            $t = (& $helper 2>&1 | Out-String).Trim()
            if ($t -and $LASTEXITCODE -eq 0) { return $t }
        }
        $t2 = (& go run ./cmd/print_core_build_tags 2>&1 | Out-String).Trim()
        if ($t2 -and $LASTEXITCODE -eq 0) { return $t2 }
    }
    finally { Pop-Location }
    Write-Host "[build-windows-portable] WARN: using fallback build tags (go run helper blocked?)"
    return $fallback
}

function Build-HiddifyCoreWindows {
    param(
        [string] $CoreRoot,
        [string] $MinGwGccName,
        [string] $GoToolchainVer
    )
    Assert-Command $MinGwGccName 'Install MinGW-w64 and add to PATH (e.g. ProgramData\mingw64\mingw64\bin).'
    Assert-Command 'go' 'Install Go for Windows.'

    Remove-Item Env:CGO_LDFLAGS -ErrorAction SilentlyContinue

    Push-Location $CoreRoot
    try {
        $env:GOOS = 'windows'
        $env:GOARCH = 'amd64'
        $env:CGO_ENABLED = '1'
        $env:CC = $MinGwGccName
        $env:GOTOOLCHAIN = $GoToolchainVer

        $cronetDir = (& go list -m -f '{{.Dir}}' 'github.com/sagernet/cronet-go/lib/windows_amd64').Trim()
        if (-not $cronetDir) { throw 'go list cronet: empty Dir' }

        $binDir = Join-Path $CoreRoot 'bin'
        New-Item -ItemType Directory -Force -Path $binDir | Out-Null
        Remove-Item (Join-Path $CoreRoot 'hiddify-core.dll') -ErrorAction SilentlyContinue

        $cronetDll = Join-Path $cronetDir 'libcronet.dll'
        Assert-File $cronetDll "Missing libcronet.dll: $cronetDll"
        Copy-Item -LiteralPath $cronetDll -Destination (Join-Path $binDir 'libcronet.dll') -Force

        $tags = Get-HiddifyCoreBuildTags -CoreRoot $CoreRoot
        $tagsWithPure = if ($tags -match 'with_purego') { $tags } else { "$tags,with_purego" }

        Write-Host "[build-windows-portable] go build hiddify-core.dll (tags: $tagsWithPure)..."
        & go build -trimpath -ldflags='-w -s -checklinkname=0' -buildmode=c-shared -tags $tagsWithPure `
            -o (Join-Path $binDir 'hiddify-core.dll') ./platform/desktop
        if ($LASTEXITCODE -ne 0) { throw 'go build hiddify-core.dll failed' }

        $gopath = (& go env GOPATH).Trim()
        $rsrc = Join-Path $gopath 'bin\rsrc.exe'
        if (-not (Test-Path -LiteralPath $rsrc)) {
            Write-Host '[build-windows-portable] go install rsrc...'
            & go install github.com/akavel/rsrc@latest
            if ($LASTEXITCODE -ne 0) { throw 'go install rsrc failed' }
        }
        $ico = Join-Path $CoreRoot 'assets\hiddify-cli.ico'
        $syso = Join-Path $CoreRoot 'cmd\bydll\cli.syso'
        & $rsrc -ico $ico -o $syso
        if ($LASTEXITCODE -ne 0) { throw 'rsrc failed' }

        $env:CGO_LDFLAGS = "bin/hiddify-core.dll"
        Write-Host '[build-windows-portable] go build HiddifyCli.exe...'
        & go build -ldflags='-w -s -checklinkname=0' -trimpath -tags $tagsWithPure `
            -o (Join-Path $binDir 'HiddifyCli.exe') ./cmd/bydll
        if ($LASTEXITCODE -ne 0) { throw 'go build HiddifyCli.exe failed' }
    }
    finally {
        Remove-Item Env:CGO_LDFLAGS -ErrorAction SilentlyContinue
        Remove-Item Env:GOOS, Env:GOARCH -ErrorAction SilentlyContinue
        Pop-Location
    }

    Assert-File (Join-Path $CoreRoot 'bin\hiddify-core.dll') 'hiddify-core.dll missing'
    Assert-File (Join-Path $CoreRoot 'bin\HiddifyCli.exe') 'HiddifyCli.exe missing'
    Assert-File (Join-Path $CoreRoot 'bin\libcronet.dll') 'libcronet.dll missing'
    Write-Host '[build-windows-portable] core OK: hiddify-core\bin\'
}

function Invoke-FlutterCommonPrepare {
    param([string] $RepoRoot, [string] $Flutter)
    Push-Location $RepoRoot
    try {
        Assert-Command $Flutter 'Add Flutter to PATH or pass -FlutterExe / set FLUTTER_ROOT.'
        & $Flutter pub get
        if ($LASTEXITCODE -ne 0) { throw 'flutter pub get failed' }
        & $Flutter 'pub' 'run' 'build_runner' 'build' '--delete-conflicting-outputs'
        if ($LASTEXITCODE -ne 0) { throw 'build_runner failed' }
        & $Flutter 'pub' 'run' 'slang'
        if ($LASTEXITCODE -ne 0) { throw 'slang failed' }
    }
    finally { Pop-Location }
}

function Sync-WindowsPortableFromRelease {
    param(
        [string] $RepoRoot,
        [string] $PortableRelative
    )
    $rel = Join-Path $RepoRoot 'build\windows\x64\runner\Release'
    $exe = Join-Path $rel 'Hiddify.exe'
    $fw = Join-Path $rel 'flutter_windows.dll'
    Assert-File $exe "Missing $exe - run flutter build windows --release first."
    Assert-File $fw "Missing $fw in Release."

    $dst = Join-Path $RepoRoot $PortableRelative
    New-Item -ItemType Directory -Force -Path $dst | Out-Null

    Write-Host "[build-windows-portable] robocopy Release -> $PortableRelative ..."
    # Robocopy exit is a bitmask; bit 3 (8) = copy failures; bit 4 (16) = serious error (MS docs).
    & robocopy $rel $dst /E /IS /IT /R:8 /W:2 /NFL /NDL /NJH /NJS /np
    $rc = $LASTEXITCODE
    if ((($rc -band 8) -ne 0) -or (($rc -band 16) -ne 0)) {
        throw "robocopy failed (exit=$rc). Close Hiddify.exe using this folder, or use -PortableDst for a new path. Locked DLLs cause ERROR_SHARING_VIOLATION (32)."
    }

    Write-Host "[build-windows-portable] portable: $dst"
    Write-Host "[build-windows-portable] run: $(Join-Path $dst 'Hiddify.exe')"
}

function Copy-CoreBinariesToPortable {
    param([string] $RepoRoot, [string] $PortableRelative)
    $bin = Join-Path $RepoRoot 'hiddify-core\bin'
    $dst = Join-Path $RepoRoot $PortableRelative
    if (-not (Test-Path -LiteralPath $dst)) {
        throw "Portable folder missing: $dst - run Full or Sync first."
    }
    foreach ($f in @('hiddify-core.dll', 'HiddifyCli.exe', 'libcronet.dll')) {
        Assert-File (Join-Path $bin $f) "Missing $f in hiddify-core\bin"
        Copy-Item -LiteralPath (Join-Path $bin $f) -Destination (Join-Path $dst $f) -Force
    }
    Write-Host "[build-windows-portable] updated in ${dst}: hiddify-core.dll HiddifyCli.exe libcronet.dll"
}

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$coreRoot = Join-Path $repoRoot 'hiddify-core'
Set-Location $repoRoot

Remove-Item Env:CGO_LDFLAGS -ErrorAction SilentlyContinue

$sentry = $env:SENTRY_DSN
if (-not $sentry) { $sentry = '' }

Write-Host "[build-windows-portable] Mode=$Mode Repo=$repoRoot"

switch ($Mode) {
    'Core' {
        Build-HiddifyCoreWindows -CoreRoot $coreRoot -MinGwGccName $MinGwGcc -GoToolchainVer $GoToolchain
    }
    'Prepare' {
        Assert-File (Join-Path $coreRoot 'bin\hiddify-core.dll') 'Need hiddify-core\bin\hiddify-core.dll (run -Mode Core or Full).'
        Invoke-FlutterCommonPrepare -RepoRoot $repoRoot -Flutter $FlutterExe
    }
    'Sync' {
        Sync-WindowsPortableFromRelease -RepoRoot $repoRoot -PortableRelative $PortableDst
    }
    'CoreRefresh' {
        Build-HiddifyCoreWindows -CoreRoot $coreRoot -MinGwGccName $MinGwGcc -GoToolchainVer $GoToolchain
        Copy-CoreBinariesToPortable -RepoRoot $repoRoot -PortableRelative $PortableDst
    }
    'Full' {
        Build-HiddifyCoreWindows -CoreRoot $coreRoot -MinGwGccName $MinGwGcc -GoToolchainVer $GoToolchain
        Invoke-FlutterCommonPrepare -RepoRoot $repoRoot -Flutter $FlutterExe

        Push-Location $repoRoot
        try {
            Write-Host '[build-windows-portable] flutter build windows --release (portable=true)...'
            & $FlutterExe 'build' 'windows' '--release' `
                "--dart-define=sentry_dsn=$sentry" `
                '--dart-define=portable=true'
            if ($LASTEXITCODE -ne 0) { throw 'flutter build windows failed' }
        }
        finally { Pop-Location }

        Sync-WindowsPortableFromRelease -RepoRoot $repoRoot -PortableRelative $PortableDst
    }
}

Write-Host '[build-windows-portable] done.'
