param(
    [Parameter(Mandatory=$true)][string]$Target,
    [string]$RemotePath = "~/warp-masque-stand-bundle-with-binary.tgz",
    [switch]$SkipPack
)

$ErrorActionPreference = "Stop"
$RepoRoot = Split-Path -Parent $PSScriptRoot
if (-not (Test-Path (Join-Path $RepoRoot "masque_stand_runner.py"))) {
    Write-Error "Ожидался каталог l3router около $($RepoRoot)."
}
$Bash = "C:\Program Files\Git\bin\bash.exe"
if (-not (Test-Path $Bash)) { $Bash = "bash" }

function To-GitUnixPath([string]$WinPath) {
    if ($WinPath -match '^([A-Za-z]):\\(.*)$') {
        return "/" + $Matches[1].ToLower() + "/" + ($Matches[2] -replace '\\', '/')
    }
    return ($WinPath -replace '\\', '/')
}

$StandParent = Split-Path -Parent $RepoRoot
$Tarball = Join-Path $StandParent "warp-masque-stand-bundle-with-binary.tgz"
$UnixRoot = To-GitUnixPath $RepoRoot
$UnixTarball = To-GitUnixPath $Tarball
$UnixPack = To-GitUnixPath $PSScriptRoot

if (-not $SkipPack) {
    $bin = Join-Path $RepoRoot "artifacts\sing-box-linux-amd64"
    if (-not (Test-Path $bin)) {
        Write-Error "Нет artifacts/sing-box-linux-amd64. Соберите из hiddify-core/hiddify-sing-box (Linux amd64)." 
    }
    & $Bash -lc "cd '$UnixRoot' && WITH_SINGBOX=1 '$UnixPack/pack_warp_masque_server_bundle.sh' '$UnixTarball'"
}

if (-not (Test-Path $Tarball)) {
    Write-Error "Нет архива $Tarball (уберите -SkipPack или создайте вручную)."
}

Write-Host "scp -> ${Target}:${RemotePath}"
& scp -o BatchMode=yes -o StrictHostKeyChecking=accept-new $Tarball $Target`:$RemotePath
Write-Host "OK. На сервере (пример): mkdir -p ~/warp-masque-stand && tar xzf $RemotePath -C ~/warp-masque-stand"
