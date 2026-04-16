[CmdletBinding()]
param(
  [int]$FileSizeMB = 100,
  [int]$TcpPort = 29001,
  [int]$UdpPort = 29002,
  [int]$UdpDatagramBytes = 1200
)

$ErrorActionPreference = "Stop"
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$standRoot = Split-Path -Parent $scriptDir
$runtimeDir = Join-Path $standRoot "runtime"
New-Item -ItemType Directory -Force -Path $runtimeDir | Out-Null
$reportPath = Join-Path $runtimeDir "tcp_udp_100mb_latest.json"

$tmpDir = Join-Path $runtimeDir "tcp-udp-data"
New-Item -ItemType Directory -Force -Path $tmpDir | Out-Null

$bytesExpected = [int64]$FileSizeMB * 1024 * 1024
$srcA = Join-Path $tmpDir "owner-a-$($FileSizeMB)mb.bin"
$srcC = Join-Path $tmpDir "owner-c-$($FileSizeMB)mb.bin"
$dstATcp = Join-Path $tmpDir "tcp-owner-a-recv.bin"
$dstCTcp = Join-Path $tmpDir "tcp-owner-c-recv.bin"
$dstAUdp = Join-Path $tmpDir "udp-owner-a-recv.bin"
$dstCUdp = Join-Path $tmpDir "udp-owner-c-recv.bin"

function Get-NowMs {
  [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds()
}

function Get-Bytes([string]$path) {
  if (!(Test-Path $path)) { return [int64]0 }
  return (Get-Item $path).Length
}

function Get-Sha([string]$path) {
  if (!(Test-Path $path)) { return "" }
  return (Get-FileHash -Algorithm SHA256 -Path $path).Hash.ToLowerInvariant()
}

function Remove-ContainerIfExists([string]$name) {
  $exists = docker ps -a --format "{{.Names}}" | Where-Object { $_ -eq $name }
  if ($exists) {
    docker rm -f $name | Out-Null
  }
}

function Wait-FileSize([string]$path, [int64]$expected, [int]$timeoutSec = 90) {
  for ($i = 0; $i -lt $timeoutSec; $i++) {
    $got = Get-Bytes $path
    if ($got -ge $expected) { return $got }
    Start-Sleep -Seconds 1
  }
  return (Get-Bytes $path)
}

Write-Host "[tcp-udp] generate payloads $FileSizeMB MB"
docker run --rm -v "${tmpDir}:/data" alpine sh -lc "dd if=/dev/urandom of=/data/owner-a-${FileSizeMB}mb.bin bs=1M count=${FileSizeMB} status=none" | Out-Null
docker run --rm -v "${tmpDir}:/data" alpine sh -lc "dd if=/dev/urandom of=/data/owner-c-${FileSizeMB}mb.bin bs=1M count=${FileSizeMB} status=none" | Out-Null

$shaSrcA = Get-Sha $srcA
$shaSrcC = Get-Sha $srcC

Write-Host "[tcp-udp] TCP A->C"
Remove-ContainerIfExists "tcp-recv-a"
Remove-Item -Force -ErrorAction SilentlyContinue $dstATcp
docker run -d --name tcp-recv-a --network container:l3router-smb-client2 -v "${tmpDir}:/data" alpine/socat -T 20 -u TCP-LISTEN:${TcpPort},reuseaddr OPEN:/data/tcp-owner-a-recv.bin,creat,trunc | Out-Null
Start-Sleep -Seconds 1
$t0 = Get-NowMs
docker run --rm --network container:l3router-smb-client1 -v "${tmpDir}:/data" alpine/socat -u OPEN:/data/owner-a-${FileSizeMB}mb.bin TCP:10.0.0.4:${TcpPort} | Out-Null
$t1 = Get-NowMs
$durTcpA = [Math]::Max(1, ($t1 - $t0))
docker wait tcp-recv-a | Out-Null
Remove-ContainerIfExists "tcp-recv-a"
$bytesTcpA = Wait-FileSize $dstATcp $bytesExpected
$shaTcpA = Get-Sha $dstATcp
$okTcpA = ($shaSrcA -eq $shaTcpA)
$mbitTcpA = [Math]::Round(($bytesExpected * 8.0) / ($durTcpA / 1000.0) / 1000000.0, 2)

Write-Host "[tcp-udp] TCP C->A"
Remove-ContainerIfExists "tcp-recv-c"
Remove-Item -Force -ErrorAction SilentlyContinue $dstCTcp
docker run -d --name tcp-recv-c --network container:l3router-smb-client1 -v "${tmpDir}:/data" alpine/socat -T 20 -u TCP-LISTEN:${TcpPort},reuseaddr OPEN:/data/tcp-owner-c-recv.bin,creat,trunc | Out-Null
Start-Sleep -Seconds 1
$t0 = Get-NowMs
docker run --rm --network container:l3router-smb-client2 -v "${tmpDir}:/data" alpine/socat -u OPEN:/data/owner-c-${FileSizeMB}mb.bin TCP:10.0.0.2:${TcpPort} | Out-Null
$t1 = Get-NowMs
$durTcpC = [Math]::Max(1, ($t1 - $t0))
docker wait tcp-recv-c | Out-Null
Remove-ContainerIfExists "tcp-recv-c"
$bytesTcpC = Wait-FileSize $dstCTcp $bytesExpected
$shaTcpC = Get-Sha $dstCTcp
$okTcpC = ($shaSrcC -eq $shaTcpC)
$mbitTcpC = [Math]::Round(($bytesExpected * 8.0) / ($durTcpC / 1000.0) / 1000000.0, 2)

Write-Host "[tcp-udp] UDP A->C"
Remove-ContainerIfExists "udp-recv-a"
Remove-Item -Force -ErrorAction SilentlyContinue $dstAUdp
docker run -d --name udp-recv-a --network container:l3router-smb-client2 -v "${tmpDir}:/data" alpine/socat -T 5 -u UDP-RECV:${UdpPort},reuseaddr OPEN:/data/udp-owner-a-recv.bin,creat,trunc | Out-Null
Start-Sleep -Seconds 1
$t0 = Get-NowMs
docker run --rm --network container:l3router-smb-client1 -v "${tmpDir}:/data" alpine/socat -u -b ${UdpDatagramBytes} OPEN:/data/owner-a-${FileSizeMB}mb.bin UDP:10.0.0.4:${UdpPort} | Out-Null
$t1 = Get-NowMs
$durUdpA = [Math]::Max(1, ($t1 - $t0))
docker wait udp-recv-a | Out-Null
Remove-ContainerIfExists "udp-recv-a"
$bytesUdpA = Get-Bytes $dstAUdp
$shaUdpA = Get-Sha $dstAUdp
$okUdpA = ($shaSrcA -eq $shaUdpA)
$mbitUdpA = [Math]::Round(($bytesUdpA * 8.0) / ($durUdpA / 1000.0) / 1000000.0, 2)
$ratioUdpA = [Math]::Round(($bytesUdpA * 100.0) / $bytesExpected, 2)

Write-Host "[tcp-udp] UDP C->A"
Remove-ContainerIfExists "udp-recv-c"
Remove-Item -Force -ErrorAction SilentlyContinue $dstCUdp
docker run -d --name udp-recv-c --network container:l3router-smb-client1 -v "${tmpDir}:/data" alpine/socat -T 5 -u UDP-RECV:${UdpPort},reuseaddr OPEN:/data/udp-owner-c-recv.bin,creat,trunc | Out-Null
Start-Sleep -Seconds 1
$t0 = Get-NowMs
docker run --rm --network container:l3router-smb-client2 -v "${tmpDir}:/data" alpine/socat -u -b ${UdpDatagramBytes} OPEN:/data/owner-c-${FileSizeMB}mb.bin UDP:10.0.0.2:${UdpPort} | Out-Null
$t1 = Get-NowMs
$durUdpC = [Math]::Max(1, ($t1 - $t0))
docker wait udp-recv-c | Out-Null
Remove-ContainerIfExists "udp-recv-c"
$bytesUdpC = Get-Bytes $dstCUdp
$shaUdpC = Get-Sha $dstCUdp
$okUdpC = ($shaSrcC -eq $shaUdpC)
$mbitUdpC = [Math]::Round(($bytesUdpC * 8.0) / ($durUdpC / 1000.0) / 1000000.0, 2)
$ratioUdpC = [Math]::Round(($bytesUdpC * 100.0) / $bytesExpected, 2)

$report = [ordered]@{
  mode = "docker-clients-tcp-udp-over-l3router"
  file_size_mb = $FileSizeMB
  tcp = [ordered]@{
    a_to_c = [ordered]@{
      duration_ms = $durTcpA
      throughput_mbit_per_sec = $mbitTcpA
      bytes_received = $bytesTcpA
      sha256_match = $okTcpA
    }
    c_to_a = [ordered]@{
      duration_ms = $durTcpC
      throughput_mbit_per_sec = $mbitTcpC
      bytes_received = $bytesTcpC
      sha256_match = $okTcpC
    }
  }
  udp = [ordered]@{
    a_to_c = [ordered]@{
      duration_ms = $durUdpA
      throughput_mbit_per_sec = $mbitUdpA
      bytes_received = $bytesUdpA
      delivery_ratio_percent = $ratioUdpA
      sha256_match = $okUdpA
    }
    c_to_a = [ordered]@{
      duration_ms = $durUdpC
      throughput_mbit_per_sec = $mbitUdpC
      bytes_received = $bytesUdpC
      delivery_ratio_percent = $ratioUdpC
      sha256_match = $okUdpC
    }
  }
}

$json = $report | ConvertTo-Json -Depth 6
Set-Content -Path $reportPath -Value $json -Encoding UTF8
Write-Host "[tcp-udp] report: $reportPath"
Write-Output $json
