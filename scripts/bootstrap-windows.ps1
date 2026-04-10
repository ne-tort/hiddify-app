# Первичная настройка среды Windows для сборки hiddify-app через make (Git Bash / тот же sh).
# Запуск (один раз, из корня репозитория): powershell -ExecutionPolicy Bypass -File scripts/bootstrap-windows.ps1
$ErrorActionPreference = "Stop"

function Add-UserPathFront([string]$Dir) {
    if (-not (Test-Path -LiteralPath $Dir)) { return }
    $cur = [Environment]::GetEnvironmentVariable("Path", "User")
    if ($null -eq $cur) { $cur = "" }
    $parts = $cur -split ";" | Where-Object { $_ -and ($_ -ne $Dir) }
    $newPath = ($Dir + ";" + ($parts -join ";")).TrimEnd(";")
    [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
    $env:Path = $Dir + ";" + $env:Path
    Write-Host "PATH (User): добавлено в начало: $Dir"
}

$repoRoot = Split-Path -Parent $PSScriptRoot
Set-Location $repoRoot

# sh для GNU Make — обычно из Git for Windows
$gitUsrBin = Join-Path ${env:ProgramFiles} "Git\usr\bin"
if (Test-Path (Join-Path $gitUsrBin "sh.exe")) {
    Add-UserPathFront $gitUsrBin
} else {
    Write-Warning "Не найден $gitUsrBin\sh.exe. Установите Git for Windows: https://git-scm.com/download/win"
}

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Warning "Go не в PATH. Установите с https://go.dev/dl/ (версия из hiddify-core/go.mod)."
} else {
    Write-Host "go: OK ($(go version))"
    & go install github.com/akavel/rsrc@latest
    Write-Host "rsrc установлен в GOPATH/bin — добавьте $(go env GOPATH)\bin в PATH при необходимости."
}

$mingwOk = (Get-Command "x86_64-w64-mingw32-gcc" -ErrorAction SilentlyContinue) -or
    (Get-Command "x86_64-w64-mingw32-gcc-15-posix" -ErrorAction SilentlyContinue)
if (-not $mingwOk) {
    Write-Warning "MinGW-w64 не найден. Варианты: choco install mingw (или msys2: pacman -S mingw-w64-x86_64-gcc)."
}

if (-not (Get-Command make -ErrorAction SilentlyContinue)) {
    if (Get-Command winget -ErrorAction SilentlyContinue) {
        Write-Host "Установка GNU Make через winget..."
        winget install --id GnuWin32.Make --accept-package-agreements --accept-source-agreements -e
    } else {
        Write-Warning "make не найден и winget недоступен. Установите: choco install make / scoop install make / MSYS2 pacman -S make"
    }
} else {
    Write-Host "make: OK"
}

Write-Host ""
Write-Host "Дальше: откройте Git Bash, cd $(($repoRoot -replace '\\','/')), затем:"
Write-Host "  make windows-env-check"
Write-Host "  make windows-portable"
