# Автотест MASQUE ACL lab: локальный sing-box (server JSON) + HiddifyCli instance + curl через SOCKS.
# Требует: Windows, curl, Python 3, PKI/JSON из Generate-MasqueAuthLabCerts.ps1 + Generate-MasqueAuthLabConfigs.ps1 -UseLabTls.
#
#   powershell -NoProfile -File scripts/Test-MasqueAuthLabCli.ps1
#   powershell -NoProfile -File scripts/Test-MasqueAuthLabCli.ps1 -HiddifyCli "C:\path\HiddifyCli.exe" -WorkingDir "C:\path"  # рядом libcronet.dll
#
# Матрица (smoke через интернет; single-endpoint JSON через masque_lab_extract_one_endpoint.py):
#   OK: cl-auth-bearer-a, cl-auth-basic-alice, cl-neg-no-cert-bearer-only, cl-open-with-mtls, cl-open-plain
# Негативные ACL (wrong bearer при наличии валидного mTLS и first_match) покрываются unit-тестами server_auth, а не этим curl-прогоном.
#
# После каждого прогона HiddifyCli завершается через taskkill. В конце останавливается sing-box lab.

param(
    [string]$RepoRoot = "",
    [string]$HiddifyCli = "",
    [string]$SingBoxExe = "",
    [string]$CurlUrl = "http://cp.cloudflare.com/",
    [int]$PortWaitSeconds = 30,
    [int]$CliReadySeconds = 45
)

$ErrorActionPreference = "Stop"
if ([string]::IsNullOrWhiteSpace($RepoRoot)) {
    $RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
}
$serverJson = Join-Path $RepoRoot "experiments\router\stand\l3router\configs\masque-auth-lab-server.json"
$clientJson = Join-Path $RepoRoot "experiments\router\stand\l3router\configs\masque-auth-lab-client.json"
if (-not (Test-Path $serverJson)) { throw "Missing $serverJson" }
if (-not (Test-Path $clientJson)) { throw "Missing $clientJson" }

if ([string]::IsNullOrWhiteSpace($SingBoxExe)) {
    $SingBoxExe = Join-Path $RepoRoot "hiddify-core\bin\sing-box-masque.exe"
}
if (-not (Test-Path $SingBoxExe)) {
    throw "Missing sing-box: $SingBoxExe. Build from hiddify-core: go build -trimpath -tags with_masque -o bin/sing-box-masque.exe github.com/sagernet/sing-box/cmd/sing-box"
}

if ([string]::IsNullOrWhiteSpace($HiddifyCli)) {
    $HiddifyCli = Join-Path $RepoRoot "portable\windows-x64\Hiddify\HiddifyCli.exe"
}
if (-not (Test-Path $HiddifyCli)) {
    throw "Missing HiddifyCli: $HiddifyCli"
}
$workDir = Split-Path -Parent $HiddifyCli
if (-not (Get-Command python -ErrorAction SilentlyContinue)) {
    throw "python not in PATH (required to build single-endpoint profiles)"
}
$pyExtract = Join-Path $PSScriptRoot "masque_lab_extract_one_endpoint.py"
if (-not (Test-Path $pyExtract)) { throw "Missing $pyExtract" }
foreach ($dll in @("libcronet.dll", "hiddify-core.dll")) {
    $p = Join-Path $workDir $dll
    if (-not (Test-Path $p)) { throw "Missing $dll next to HiddifyCli in $workDir" }
}

function Stop-LabProcesses {
    Get-Process -Name "HiddifyCli" -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
    Get-Process -Name "sing-box-masque" -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
}

function Wait-TcpPort([string]$HostName, [int]$Port, [int]$TimeoutSec) {
    $dead = (Get-Date).AddSeconds($TimeoutSec)
    while ((Get-Date) -lt $dead) {
        try {
            $c = New-Object System.Net.Sockets.TcpClient
            $iar = $c.BeginConnect($HostName, $Port, $null, $null)
            if ($iar.AsyncWaitHandle.WaitOne(500)) {
                $c.EndConnect($iar)
                $c.Close()
                return
            }
            $c.Close()
        }
        catch { }
        Start-Sleep -Milliseconds 300
    }
    throw "Timeout waiting for $HostName`:$Port"
}

function Wait-HiddifyCliPort([string[]]$LogPaths, [int]$TimeoutSec) {
    $dead = (Get-Date).AddSeconds($TimeoutSec)
    while ((Get-Date) -lt $dead) {
        foreach ($LogPath in $LogPaths) {
            if (Test-Path $LogPath) {
                $m = Select-String -Path $LogPath -Pattern "Instance is running on port socks5://127\.0\.0\.1:(\d+)" -AllMatches | Select-Object -Last 1
                if ($m -and $m.Matches.Count -gt 0) {
                    return [int]$m.Matches[0].Groups[1].Value
                }
            }
        }
        Start-Sleep -Milliseconds 400
    }
    throw "Timeout: no SOCKS port in logs: $($LogPaths -join ', ')"
}

Stop-LabProcesses

$pSb = $null
$sbLog = Join-Path $env:TEMP "masque-lab-sing-box.log"
$sbErr = Join-Path $env:TEMP "masque-lab-sing-box.err.log"
Remove-Item $sbLog, $sbErr -ErrorAction SilentlyContinue
$pSb = Start-Process -FilePath $SingBoxExe -ArgumentList @("run", "-c", $serverJson) -WorkingDirectory $RepoRoot `
    -WindowStyle Hidden -PassThru -RedirectStandardOutput $sbLog -RedirectStandardError $sbErr
try {
    Wait-TcpPort "127.0.0.1" 18710 $PortWaitSeconds

    $matrix = @(
        @{ Tag = "cl-auth-bearer-a"; ExpectOk = $true },
        @{ Tag = "cl-auth-basic-alice"; ExpectOk = $true },
        @{ Tag = "cl-neg-no-cert-bearer-only"; ExpectOk = $true },
        @{ Tag = "cl-open-with-mtls"; ExpectOk = $true },
        @{ Tag = "cl-open-plain"; ExpectOk = $true }
    )

    foreach ($row in $matrix) {
        $tag = $row.Tag
        $wantOk = $row.ExpectOk
        $profilePath = Join-Path $env:TEMP "masque-lab-profile-$tag.json"
        & python $pyExtract $clientJson $tag $profilePath
        if ($LASTEXITCODE -ne 0) { throw "python extract failed for tag=$tag" }

        $cliLog = Join-Path $env:TEMP "masque-lab-hiddifycli-$tag.log"
        $cliErr = Join-Path $env:TEMP "masque-lab-hiddifycli-$tag.err.log"
        Remove-Item $cliLog, $cliErr -ErrorAction SilentlyContinue
        $cliArgs = @("instance", "--disable-color", "-c", $profilePath, "--log", "info")
        $pCli = Start-Process -FilePath $HiddifyCli -ArgumentList $cliArgs -WorkingDirectory $workDir `
            -WindowStyle Hidden -PassThru -RedirectStandardOutput $cliLog -RedirectStandardError $cliErr

        try {
            $port = Wait-HiddifyCliPort @($cliLog, $cliErr) $CliReadySeconds
            $curlHttp = (& curl.exe -x "socks5h://127.0.0.1:$port" --ipv4 -m 20 -sS -o "NUL" -w "%{http_code}" -I $CurlUrl 2>$null | Out-String).Trim()
            $curlExit = $LASTEXITCODE
            $httpOk = ($curlHttp -eq "200" -or $curlHttp -eq "204" -or $curlHttp -eq "301")
            $ok = ($curlExit -eq 0 -and $httpOk)
            if ($wantOk -ne $ok) {
                $tail = ""
                foreach ($lf in @($cliLog, $cliErr)) {
                    if (Test-Path $lf) {
                        $tail += "--- $lf ---`n" + (Get-Content $lf -Tail 30 -ErrorAction SilentlyContinue | Out-String)
                    }
                }
                throw "Tag=$tag expectOk=$wantOk curlExit=$curlExit httpCode=$curlHttp`n$tail"
            }
            Write-Host "OK  $tag (socks $port)"
        }
        finally {
            Stop-Process -Id $pCli.Id -Force -ErrorAction SilentlyContinue
            Start-Sleep -Milliseconds 400
            Get-Process -Name "HiddifyCli" -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
        }
    }
    Write-Host "All MASQUE lab CLI matrix checks passed."
}
finally {
    if ($pSb -and -not $pSb.HasExited) {
        Stop-Process -Id $pSb.Id -Force -ErrorAction SilentlyContinue
    }
    Get-Process -Name "sing-box-masque" -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
    Stop-LabProcesses
}
