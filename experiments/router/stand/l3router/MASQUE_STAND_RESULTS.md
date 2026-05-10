# MASQUE Stand Results

## Stand Topology

- `masque-client-core`
- `masque-server-core`
- `masque-iperf-server`

Compose file:

- `experiments/router/stand/l3router/docker-compose.masque-e2e.yml`

## Core Build

Linux artifact used by stand image:

- `experiments/router/stand/l3router/artifacts/sing-box-linux-amd64`

Build command:

```powershell
$env:GOWORK='off'; $env:CGO_ENABLED='0'; $env:GOOS='linux'; $env:GOARCH='amd64'
go build -trimpath -ldflags='-s -w' -tags 'with_gvisor,with_clash_api,with_utls,with_l3router,with_masque' -o "experiments/router/stand/l3router/artifacts/sing-box-linux-amd64" ./cmd/sing-box
```

## Smoke Gate (mandatory)

Target:

- transfer at least `10KB` in at most `5s`

Command (host-side orchestration):

```powershell
$sw=[System.Diagnostics.Stopwatch]::StartNew()
docker exec -d masque-server-core sh -lc "rm -f /tmp/udp_received.bin; timeout 5 socat -u UDP-RECVFROM:9001,reuseaddr,fork SYSTEM:'cat >> /tmp/udp_received.bin'"
docker exec masque-client-core bash -lc "ip route add 10.200.0.0/24 dev tun0 2>/dev/null || true; for i in $(seq 1 10); do head -c 1024 /dev/zero > /dev/udp/10.200.0.3/9001; done"
$bytes=docker exec masque-server-core sh -lc "sleep 1; if [ -f /tmp/udp_received.bin ]; then wc -c < /tmp/udp_received.bin; else echo 0; fi"
$sw.Stop()
Write-Output "bytes=$bytes elapsed_ms=$($sw.ElapsedMilliseconds)"
```

Result:

- `bytes=10240`
- `elapsed_ms=1263`
- Status: `PASS`

Machine-readable smoke gate artifact (latest run path):

- `experiments/router/stand/l3router/runtime/smoke_100mib_udp_latest.json` (и смежные `smoke_tcp_*_latest.json` при прогоне 100 MiB)
- `experiments/router/stand/l3router/runtime/smoke_tcp_connect_stream_latest.json`

Automated smoke gate script:

- `python experiments/router/stand/l3router/masque_stand_runner.py` (по умолчанию **`--scenario all`**, 100 MiB, все ноги по очереди)

## Observed Limits

- `tcp_transport=connect_ip` is intentionally fail-fast blocked for TUN-only client profiles by endpoint validation; TCP-over-CONNECT-IP for production is provided via `transport_mode=connect_ip` + netstack packet-plane.
- `tcp_transport=connect_stream` is the supported TCP MASQUE path in the current production track.
- `tcp_mode=masque_or_direct` now exists and is policy-gated, but for the current stand topology (client without backend-network reachability) it cannot substitute true MASQUE TCP stream semantics.
- Real perf ceiling is measured via `python experiments/router/stand/l3router/masque_stand_runner.py --scenario real --mtu 1500`.

## Success Indicator Baseline (MTU 1500)

Baseline artifact (latest run):

- `experiments/router/stand/l3router/runtime/real_success_matrix_latest.json`

Run profile:

- `MASQUE_TCP_IP_DATAGRAM=1472`
- `MASQUE_TCP_IP_UDP_PAYLOAD_CAP=1472`
- scenarios: `tcp_ip` bulk `10/50/100/200/500MB` + control `rate=0` on `10MB`

Current matrix:

| Label | Rate | Size | Throughput (Mbps) | Loss (%) | Hash | Settled | Status |
|---|---:|---:|---:|---:|---|---|---|
| r0_10 | 0 | 10MB | 2.341 | 32.5864 | false | false | FAIL |
| r12_10 | 12m | 10MB | 28.746 | 0.0 | true | true | PASS |
| r12_50 | 12m | 50MB | 57.514 | 0.0 | true | true | PASS |
| r12_100 | 12m | 100MB | 65.756 | 0.0 | true | true | PASS |
| r12_200 | 12m | 200MB | 71.214 | 0.0 | true | true | PASS |
| r12_500 | 12m | 500MB | 74.792 | 0.0 | true | true | PASS |
| r16_50 | 16m | 50MB | 67.576 | 0.0 | true | true | PASS |
| r16_200 | 16m | 200MB | 87.455 | 0.0 | true | true | PASS |
| r20_50 | 20m | 50MB | 75.983 | 0.0 | true | true | PASS |
| r20_200 | 20m | 200MB | 101.222 | 0.0 | true | true | PASS |

## Config Notes

- `tcp_mode` is available in MASQUE endpoint config:
  - `strict_masque`
  - `masque_or_direct` (requires `fallback_policy=direct_explicit`)
- Unsupported tunables (`udp_timeout`, `workers`) fail fast at endpoint validation time.
- `mtu` is supported as CONNECT-IP datagram ceiling (`[1280, 65535]`) and is passed to runtime as `ConnectIPDatagramCeiling`.

## Scoped CONNECT-IP Contract (`P1#10`)

Canonical scoped scenario command:

```powershell
python experiments/router/stand/l3router/masque_stand_runner.py --scenario tcp_ip_scoped
```

Scoped contract (positive path):

- `transport_mode=connect_ip` and scoped template variables are used through `template_ip` (`{target}`, `{ipproto}`).
- Expected observability assert in runtime snapshot:
  - `connect_ip_scope_target=10.200.0.2/32`
  - `connect_ip_scope_ipproto=17`
- Dual-stack policy contract in stand: default full-flow scope remains IPv4 wildcard (`0.0.0.0/0`) unless explicit `connect_ip_scope_target` is provided; IPv6 scoped forwarding is opt-in only.

Malformed negative contract:

- Bad scoped target must fail deterministically with capability/policy class (`actual_error_class in ["capability","policy"]`).
- Startup failure before traffic phase (compose/client init) is still valid negative outcome and is exported as `error_source=compose_up`.

Machine-readable scoped artifact (latest run path):

- `experiments/router/stand/l3router/runtime/scoped_connect_ip_latest.json`
- Contains:
  - global result (`mode=connect_ip_scoped`, `result=true|false`)
  - positive scope observability fields (`scope_target`, `scope_ipproto`)
  - negative malformed target fields (`actual_error_class`, `error_source`)
