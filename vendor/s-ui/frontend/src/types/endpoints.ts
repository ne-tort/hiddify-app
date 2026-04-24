import { Dial } from "./dial"

export const EpTypes = {
  Wireguard: 'wireguard',
  Awg: 'awg',
  Warp: 'warp',
  Tailscale: 'tailscale',
  L3Router: 'l3router',
}

type EpType = typeof EpTypes[keyof typeof EpTypes]

interface EndpointBasics {
  id: number
  type: EpType
  tag: string
}

export interface WgPeer {
  address: string
  port: number
  public_key: string
  private_key?: string
  pre_shared_key?: string
  allowed_ips?: string[]
  persistent_keepalive_interval?: number
  reserved?: number[]
  client_id?: number
  client_name?: string
  group_id?: number
  managed?: boolean
  user?: string
}

export interface WireGuard extends EndpointBasics, Dial {
  system?: boolean
  name?: string
  forward_allow?: boolean
  cloak_enabled?: boolean
  cloak_detour_tag?: string
  mtu?: number
  persistent_keepalive_interval?: number
  address: string[]
  private_key: string
  listen_port: number
  peers: WgPeer[]
  member_group_ids?: number[]
  member_client_ids?: number[]
  udp_timeout?: string
  workers?: number
  ext: any
}

/** AmneziaWG endpoint: same managed-peer model as WireGuard; obfuscation fields optional (merged with profiles on server). */
export interface AmneziaWG extends WireGuard {
  obfuscation_profile_id?: number
  jc?: number
  jmin?: number
  jmax?: number
  s1?: number
  s2?: number
  s3?: number
  s4?: number
  h1?: string
  h2?: string
  h3?: string
  h4?: string
  i1?: string
  i2?: string
  i3?: string
  i4?: string
  i5?: string
}

export interface Warp extends WireGuard {}

export interface Tailscale extends EndpointBasics, Dial {
  state_directory?: string
  auth_key?: string
  control_url?: string
  ephemeral?: boolean
  hostname?: string
  accept_routes?: boolean
  exit_node?: string
  exit_node_allow_lan_access?: boolean
  advertise_routes?: string[]
  advertise_exit_node?: boolean
  relay_server_port?: number
  relay_server_static_endpoints?: string[]
  system_interface?: boolean
  system_interface_name?: string
  system_interface_mtu?: number
  udp_timeout?: string
}

export interface L3RouterPeer {
  client_id?: number
  client_name?: string
  group_id?: number
  peer_id: number
  user: string
  allowed_ips: string[]
  filter_source_ips?: string[]
  filter_destination_ips?: string[]
}

export interface L3Router extends EndpointBasics {
  peers: L3RouterPeer[]
  member_group_ids?: number[]
  member_client_ids?: number[]
  /** IPv4 pool (RFC1918 / 100.64) for auto /32 per peer; empty = legacy 10.250.x.y/32 from peer_id */
  private_subnet?: string
  overlay_destination?: string
  packet_filter?: boolean
  fragment_policy?: string
  overflow_policy?: string
  telemetry_level?: string
  lookup_backend?: string
}

// Create interfaces dynamically based on EpTypes keys
type InterfaceMap = {
  [Key in keyof typeof EpTypes]: {
    type: string
    [otherProperties: string]: any // You can add other properties as needed
  }
}

// Create union type from InterfaceMap
export type Endpoint = InterfaceMap[keyof InterfaceMap]

// Create defaultValues object dynamically
const defaultValues: Record<EpType, Endpoint> = {
  wireguard: { type: EpTypes.Wireguard, address: ['10.0.0.2/32','fe80::2/128'], private_key: '', listen_port: 0, peers: [], member_group_ids: [], member_client_ids: [], forward_allow: false, cloak_enabled: false },
  awg: { type: EpTypes.Awg, address: ['10.1.0.1/24', 'fe80::1/128'], private_key: '', listen_port: 0, peers: [], member_group_ids: [], member_client_ids: [], forward_allow: false, cloak_enabled: false },
  warp: { type: EpTypes.Warp, address: [], private_key: '', listen_port: 0, mtu: 1420, peers: [{ address: '', port: 0, public_key: ''}] },
  tailscale: { type: EpTypes.Tailscale, domain_resolver: 'local' },
  l3router: { type: EpTypes.L3Router, peers: [], private_subnet: '', overlay_destination: '198.18.0.1:33333', packet_filter: false },
}

export function createEndpoint<T extends Endpoint>(type: string,json?: Partial<T>): Endpoint {
  const defaultObject: Endpoint = { ...defaultValues[type], ...(json || {}) }
  return defaultObject
}