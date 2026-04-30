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

- `experiments/router/stand/l3router/runtime/smoke_10kb_latest.json`
- `experiments/router/stand/l3router/runtime/smoke_tcp_connect_stream_latest.json`

Automated smoke gate script:

- `experiments/router/stand/l3router/scripts/masque_smoke_10kb_gate.sh`

## Observed Limits

- `tcp_transport=connect_ip` is intentionally fail-fast blocked for production profiles until dedicated TCP-over-IP-plane implementation is completed.
- `tcp_transport=connect_ip` is allowed only for staged testing with `MASQUE_EXPERIMENTAL_TCP_CONNECT_IP=1` and still not production-ready.
- `tcp_transport=connect_stream` is the supported TCP MASQUE path in the current production track.
- `tcp_mode=masque_or_direct` now exists and is policy-gated, but for the current stand topology (client without backend-network reachability) it cannot substitute true MASQUE TCP stream semantics.
- `iperf3` full matrix remains blocked by control-channel dependence on TCP path over MASQUE.

## Config Notes

- `tcp_mode` is available in MASQUE endpoint config:
  - `strict_masque`
  - `masque_or_direct` (requires `fallback_policy=direct_explicit`)
- Unsupported tunables (`udp_timeout`, `mtu`, `workers`) now fail fast at endpoint validation time.
