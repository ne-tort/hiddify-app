# Генерация sing-box JSON: много MASQUE server endpoints на VPS + клиентский профиль со всеми вариантами.
# Сервер: на каждом порту один процесс слушает и QUIC/H3, и TCP/TLS+H2 (это sing-box); «h2/h3» задаётся только на клиенте (http_layer).
# Запуск из корня репо:
#   powershell -NoProfile -File scripts/Generate-MasqueMultiVpsConfigs.ps1
# Параметры:
#   -PublicHost   хост в URL шаблонах и у клиента (server / tls_server_name); по умолчанию masque.ai-qwerty.ru (LE)
#   -CertPath/-KeyPath пути к PEM на сервере (по умолчанию Let's Encrypt live)
#   -PortStart    первый порт (по умолчанию 18610)
#   -ServerCount  число серверных инстансов (по умолчанию 18)
#   -Token        server_token / ACL bearer для части инстансов; пусто = сгенерировать

param(
    [string]$PublicHost = "masque.ai-qwerty.ru",
    [string]$CertPath = "",
    [string]$KeyPath = "",
    [int]$PortStart = 18610,
    [int]$ServerCount = 18,
    [string]$Token = ""
)

$ErrorActionPreference = "Stop"
$RepoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")

if ([string]::IsNullOrWhiteSpace($Token)) {
    $rb = New-Object byte[] 16
    [System.Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($rb)
    $Token = -join ($rb | ForEach-Object { $_.ToString('x2') })
}

if ([string]::IsNullOrWhiteSpace($CertPath)) {
    $CertPath = "/etc/letsencrypt/live/$PublicHost/fullchain.pem"
}
if ([string]::IsNullOrWhiteSpace($KeyPath)) {
    $KeyPath = "/etc/letsencrypt/live/$PublicHost/privkey.pem"
}
$certPath = $CertPath
$keyPath = $KeyPath

$serverEndpoints = [System.Collections.ArrayList]@()
$clientEndpoints = [System.Collections.ArrayList]@()

for ($k = 0; $k -lt $ServerCount; $k++) {
    $port = [uint16]($PortStart + $k)
    $sfx = "$port"
    $udp = "https://${PublicHost}:$port/s$sfx/udp/{target_host}/{target_port}"
    $tcp = "https://${PublicHost}:$port/s$sfx/tcp/{target_host}/{target_port}"

    # Три класса CONNECT-IP шаблона на сервере (все пути уникальны внутри инстанса)
    $mod3 = $k % 3
    $scopedIP = ($mod3 -eq 1)
    if ($scopedIP) {
        $ipT = "https://${PublicHost}:$port/s$sfx/ip/{target}/{ipproto}"
    } else {
        $ipT = "https://${PublicHost}:$port/s$sfx/ip"
    }

    $srvTag = "masque-srv-$sfx"
    $srv = [ordered]@{
        type            = "masque"
        tag             = $srvTag
        mode            = "server"
        listen          = "0.0.0.0"
        listen_port     = $port
        certificate     = $certPath
        key             = $keyPath
        template_udp    = $udp
        template_ip     = $ipT
        template_tcp    = $tcp
    }

    if ($scopedIP -or ($k % 5 -eq 0)) {
        $srv["allow_private_targets"] = $true
    }
    # Только ACL через server_auth (эквивалент одному bearer)
    elseif ($k % 11 -eq 1) {
        $srv["server_auth"] = [ordered]@{
            policy        = "first_match"
            bearer_tokens = @($Token)
        }
    }
    # Токен + allowlist
    if ($k % 4 -eq 3) {
        $srv["server_token"] = $Token
        $srv["allowed_target_ports"] = @(22, 25, 53, 80, 110, 143, 443, 465, 587, 853, 993, 8080, 8443)
    }
    # Только блок SMTP
    elseif ($k % 7 -eq 2) {
        $srv["blocked_target_ports"] = @(25)
    }
    # Только токен
    elseif ($k % 6 -eq 0) {
        $srv["server_token"] = $Token
    }

    [void]$serverEndpoints.Add($srv)

    $needTok = ($null -ne $srv["server_token"]) -or ($null -ne $srv["server_auth"])
    $clientTok = if ($needTok) { @{ server_token = $Token } } else { @{} }

    # --- клиентские вариации (один и тот же сервер:порт, разные http_layer / transport / tcp) ---
    $baseClient = [ordered]@{
        type              = "masque"
        server            = $PublicHost
        server_port       = $port
        insecure          = $false
        tls_server_name   = $PublicHost
        template_udp      = $udp
        template_tcp      = $tcp
        tcp_transport     = "connect_stream"
    }
    foreach ($kv in $clientTok.GetEnumerator()) { $baseClient[$kv.Key] = $kv.Value }

    # 1) H3 + CONNECT-UDP (явный transport)
    $c1 = [ordered]@{}
    foreach ($x in $baseClient.Keys) { $c1[$x] = $baseClient[$x] }
    $c1["tag"] = "cl-$sfx-h3-udp-strict"
    $c1["http_layer"] = "h3"
    $c1["transport_mode"] = "connect_udp"
    $c1["fallback_policy"] = "strict"
    $c1["tcp_mode"] = "strict_masque"
    [void]$clientEndpoints.Add($c1)

    # 2) H2 + CONNECT-UDP
    $c2 = [ordered]@{}
    foreach ($x in $baseClient.Keys) { $c2[$x] = $baseClient[$x] }
    $c2["tag"] = "cl-$sfx-h2-udp-strict"
    $c2["http_layer"] = "h2"
    $c2["transport_mode"] = "connect_udp"
    $c2["fallback_policy"] = "strict"
    $c2["tcp_mode"] = "strict_masque"
    [void]$clientEndpoints.Add($c2)

    # 3) auto + fallback по HTTP-слою
    $c3 = [ordered]@{}
    foreach ($x in $baseClient.Keys) { $c3[$x] = $baseClient[$x] }
    $c3["tag"] = "cl-$sfx-auto-udp-fb"
    $c3["http_layer"] = "auto"
    $c3["http_layer_fallback"] = $true
    $c3["transport_mode"] = "connect_udp"
    $c3["fallback_policy"] = "strict"
    $c3["tcp_mode"] = "strict_masque"
    [void]$clientEndpoints.Add($c3)

    # 4) H3 + CONNECT-IP + TCP через connect_stream (без template_udp — иначе валидатор клиента отклонит)
    $c4 = [ordered]@{}
    foreach ($x in $baseClient.Keys) { $c4[$x] = $baseClient[$x] }
    $null = $c4.Remove("template_udp")
    $c4["tag"] = "cl-$sfx-h3-ip-tcps"
    $c4["http_layer"] = "h3"
    $c4["transport_mode"] = "connect_ip"
    $c4["template_ip"] = $ipT
    $c4["mtu"] = 1380
    $c4["fallback_policy"] = "strict"
    $c4["tcp_mode"] = "strict_masque"
    $c4["tcp_transport"] = "connect_stream"
    [void]$clientEndpoints.Add($c4)

    # 5) H2 + CONNECT-IP + TCP через connect_ip (userspace TCP поверх CONNECT-IP)
    $c5 = [ordered]@{}
    foreach ($x in $baseClient.Keys) { $c5[$x] = $baseClient[$x] }
    $null = $c5.Remove("template_udp")
    $c5["tag"] = "cl-$sfx-h2-ip-tcpi"
    $c5["http_layer"] = "h2"
    $c5["transport_mode"] = "connect_ip"
    $c5["template_ip"] = $ipT
    $c5["mtu"] = 1340
    $c5["fallback_policy"] = "strict"
    $c5["tcp_mode"] = "strict_masque"
    $c5["tcp_transport"] = "connect_ip"
    [void]$clientEndpoints.Add($c5)

    # 6) H3 + connect_ip + masque_or_direct + direct_explicit (редкий клиентский режим)
    if ($k % 2 -eq 0) {
        $c6 = [ordered]@{}
        foreach ($x in $baseClient.Keys) { $c6[$x] = $baseClient[$x] }
        $null = $c6.Remove("template_udp")
        $c6["tag"] = "cl-$sfx-h3-ip-mix"
        $c6["http_layer"] = "h3"
        $c6["transport_mode"] = "connect_ip"
        $c6["template_ip"] = $ipT
        $c6["mtu"] = 1400
        $c6["tcp_mode"] = "masque_or_direct"
        $c6["fallback_policy"] = "direct_explicit"
        $c6["tcp_transport"] = "connect_stream"
        [void]$clientEndpoints.Add($c6)
    }
}

$serverDoc = [ordered]@{
    log       = @{ level = "warn"; timestamp = $true }
    endpoints = @($serverEndpoints)
    outbounds = @(@{ type = "direct"; tag = "direct" })
    route     = @{ final = "direct"; auto_detect_interface = $true }
}

$firstClientTag = $clientEndpoints[0]["tag"]
$clientDoc = [ordered]@{
    log       = @{ level = "warn"; timestamp = $true }
    endpoints = @($clientEndpoints)
    outbounds = @(@{ type = "direct"; tag = "direct" })
    route     = @{ final = $firstClientTag; auto_detect_interface = $true }
}

$outServer = Join-Path $RepoRoot "experiments\router\stand\l3router\configs\masque-server-multi-vps.json"
$outClient = Join-Path $RepoRoot "scripts\examples\masque_multi_vps_client.all-endpoints.json"

function Write-Utf8NoBom([string]$Path, [string]$Text) {
    $enc = New-Object System.Text.UTF8Encoding $false
    [System.IO.File]::WriteAllText($Path, $Text, $enc)
}

$jsonServer = ($serverDoc | ConvertTo-Json -Depth 20)
$jsonClient = ($clientDoc | ConvertTo-Json -Depth 20)
$outClientMin = Join-Path $RepoRoot "scripts\examples\masque_multi_vps_client.all-endpoints.min.json"
$clientObj = $jsonClient | ConvertFrom-Json
$jsonClientMin = ($clientObj | ConvertTo-Json -Depth 25 -Compress)
Write-Utf8NoBom $outServer $jsonServer
Write-Utf8NoBom $outClient $jsonClient
Write-Utf8NoBom $outClientMin $jsonClientMin

Write-Host "Wrote server: $outServer"
Write-Host "Wrote client: $outClient"
Write-Host "Wrote client (min): $outClientMin"
Write-Host "MASQUE_MULTI_VPS_TOKEN=$Token"
Write-Host "Ports: $PortStart .. $($PortStart + $ServerCount - 1) ($ServerCount servers, $($clientEndpoints.Count) client endpoints)"
