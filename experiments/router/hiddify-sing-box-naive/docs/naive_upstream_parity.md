# Naive Upstream Parity Notes (experiment)

## Snapshot

- Active experimental core: `experiments/router/hiddify-sing-box-naive`
- Upstream naiveproxy: `klzgrad/naiveproxy@de64f616` (`CHROMIUM_VERSION=147.0.7727.49`)
- SagerNet naiveproxy (cronet-go fork): `sagernet/naiveproxy@2be061b6`
- cronet-go pinned naive submodule commit: `bd3768c5` (`CHROMIUM_VERSION=147.0.7727.49`)

## What this means for integration

- The active `cronet-go` path used by sing-box already targets the 147 Chromium/Naive generation.
- The large diff between full repositories mostly contains Chromium tree reshaping and build system deltas, not a direct API break for sing-box naive outbound integration.
- Current sing-box integration remains `cronet-go` API driven, so compatibility is preserved by keeping:
  - `protocol/naive/outbound.go` contract (`NaiveClientOptions`, `DNSResolver`, `DialEarly`, `Engine().Version()`)
  - feature gates (`with_naive_outbound`, `with_quic`)
  - option surface (`insecure_concurrency`, `quic`, `quic_congestion_control`, `udp_over_tcp`, `force_ipv4_dns`, TLS subset, ECH).

## Critical compatibility points retained

- Build-tag fallback behavior:
  - `include/naive_outbound.go`
  - `include/naive_outbound_stub.go`
- QUIC inbound hook behavior:
  - `include/quic.go`
  - `include/quic_stub.go`
- Naive outbound TLS guardrails and cronet startup behavior:
  - `protocol/naive/outbound.go`

## DNS/IPv6 alignment change introduced in this experiment

- New outbound option: `force_ipv4_dns` (`option/naive.go`).
- When enabled, naive outbound cronet DNS hook:
  - uses `ipv4_only` query strategy for DNS exchange,
  - short-circuits AAAA requests with success/empty answer,
  - keeps default behavior unchanged when option is disabled.

Rationale: avoid undesired IPv6 DNS attempts in environments where `prefer_ipv4` is insufficient.

The same `force_ipv4_dns` behavior is merged into production core [`hiddify-core/hiddify-sing-box`](../../../../hiddify-core/hiddify-sing-box) (option, outbound, EN/ZH outbound docs). This experiment tree can stay as a reference snapshot; prefer editing the production path for further naive work.

## test module (`hiddify-sing-box/test`)

`test/go.mod` must mirror the root module `replace` directives (`wireguard-go`, `amneziawg-go`, `sing-tun`, `tailscale`, Psiphon forks, `sing-dns`, `dnscrypt`, `vaydns`) so `go mod tidy` resolves `github.com/sagernet/wireguard-go/hiddify` and stays on the same `cronet-go` pseudo-version as the root `go.mod`.

## Verifying builds and tests

- **Compile (Windows):** `go test -tags "with_naive_outbound,with_purego" ./protocol/naive/...` from `hiddify-core/hiddify-sing-box` (cronet `all` needs `with_purego` on this platform).
- **Full binary:** `go build -tags "with_naive_outbound,with_purego" ./cmd/sing-box` from the same directory.
- **`TestNaiveSelf` / Docker naive tests:** require `libcronet.dll` (Windows) or `libcronet.so` (Linux) next to the test binary or on `PATH`, plus Docker for `naive_test.go`. Without the library, outbound construction fails at parse time with `cronet: library not found`.

## Upstream maintenance checklist

1. In [`experiments/router/reference-projects/naiveproxy-sagernet`](../../reference-projects/naiveproxy-sagernet): `git fetch origin` and `git fetch upstream`; compare `cronet-go` branch to tags on `klzgrad/naiveproxy`.
2. When bumping [`github.com/sagernet/cronet-go`](https://github.com/sagernet/cronet-go) in root `go.mod`, run `go mod tidy` in `hiddify-core/hiddify-sing-box` and in `hiddify-core/hiddify-sing-box/test`.
3. Re-run compile/test commands above; run Docker integration tests where CI allows.
4. Update the **Snapshot** section of this file with new commit hashes / `CHROMIUM_VERSION` when naive/cronet generations change.
