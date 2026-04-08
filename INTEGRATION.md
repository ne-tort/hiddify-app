# Core Integration Notes (AWG Fork)

## Scope
- Desktop integration of custom `hiddify-core` with modified AWG endpoint.
- AWG/WG parsing path audited for JSON and INI import.
- `psiphon` outbound disabled in embedded `hiddify-sing-box` registry for this fork.

## Core wiring
- `hiddify-app` consumes native core artifacts from `hiddify-core/bin`.
- Desktop loaders use:
  - Windows: `hiddify-core.dll`
  - Linux: `lib/hiddify-core.so`
  - macOS: `hiddify-core.dylib`
- Integration remains in the original client flow (no external wrapper launcher).

## AWG endpoint source
- Core changes are inside `hiddify-core/hiddify-sing-box`:
  - `protocol/awg/endpoint.go`
  - `transport/awg/*` (`endpoint.go`, `endpoint_options.go`, `ipc.go`, `bind.go`)
- AWG IPC includes obfuscation fields:
  - `jc`, `jmin`, `jmax`
  - `s1..s4`, `h1..h4`, `i1..i5`
  - `protocol_version=1` for peer entries

## Parser/convert path
- `hiddify-core/ray2sing/ray2sing/awg.go`:
  - fixed AWG/WG split (removed forced WG fallback)
  - INI and URL parsing now keep AWG as `type: awg` when AWG obfuscation fields are present
- `hiddify-app/lib/features/profile/data/profile_parser.dart`:
  - INI protocol detection updated:
    - `[Interface]` + AWG fields -> `AWG`
    - plain WG INI -> `WireGuard`

## Validation checklist
1. Build core artifact for target desktop platform.
2. Verify app loads platform-native core library.
3. Import plain WG INI and AWG INI (`jc/jmin/...`) and ensure protocol type is preserved.
4. Confirm runtime connection for both WG baseline and AWG profile.
