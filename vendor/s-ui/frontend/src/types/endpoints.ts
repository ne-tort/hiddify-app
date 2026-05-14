import { Dial } from "./dial"

export const EpTypes = {
  Wireguard: 'wireguard',
  Awg: 'awg',
  Warp: 'warp',
  Tailscale: 'tailscale',
  L3Router: 'l3router',
  Masque: 'masque',
  WarpMasque: 'warp_masque',
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
  /** UI-only: hub server peer is internet exit (merged to 0.0.0.0/0 + ::/0 in sing-box). Stripped in runtime JSON. */
  peer_exit?: boolean
}

export interface WireGuard extends EndpointBasics, Dial {
  system?: boolean
  name?: string
  /** UI-only: this node dials upstream hub (no listen_port). Stripped in runtime JSON. */
  hub_client_mode?: boolean
  forward_allow?: boolean
  internet_allow?: boolean
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
  gso_enabled?: boolean
  kernel_path_enabled?: boolean
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

/** MASQUE sing-box endpoint (server or client); member_* stripped in runtime JSON. */
export interface Masque extends EndpointBasics, Dial {
  mode?: string
  listen?: string
  listen_port?: number
  server?: string
  server_port?: number
  transport_mode?: string
  template_udp?: string
  template_ip?: string
  template_tcp?: string
  tls_server_name?: string
  insecure?: boolean
  http_layer?: string
  server_auth?: Record<string, unknown>
  member_group_ids?: number[]
  member_client_ids?: number[]
  /** Reference to TLS row in panel DB; merged into certificate/key at runtime (stripped in MarshalJSON). */
  sui_tls_id?: number
  ext?: unknown
}

/** WARP over MASQUE client profile; sensitive profile keys may live in ext (merged server-side). */
export interface WarpMasque extends EndpointBasics, Dial {
  mode?: string
  server?: string
  server_port?: number
  transport_mode?: string
  template_udp?: string
  template_ip?: string
  template_tcp?: string
  tls_server_name?: string
  http_layer?: string
  profile?: Record<string, unknown>
  member_group_ids?: number[]
  member_client_ids?: number[]
  ext?: unknown
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
  wireguard: { type: EpTypes.Wireguard, address: ['10.0.0.2/32'], private_key: '', listen_port: 0, peers: [], member_group_ids: [], member_client_ids: [], forward_allow: false, internet_allow: true, cloak_enabled: false },
  awg: { type: EpTypes.Awg, address: ['10.1.0.1/24'], private_key: '', listen_port: 0, peers: [], member_group_ids: [], member_client_ids: [], forward_allow: false, internet_allow: true, cloak_enabled: false, gso_enabled: true, kernel_path_enabled: false },
  warp: { type: EpTypes.Warp, address: [], private_key: '', listen_port: 0, mtu: 1420, peers: [{ address: '', port: 0, public_key: ''}] },
  tailscale: { type: EpTypes.Tailscale, domain_resolver: 'local' },
  l3router: { type: EpTypes.L3Router, peers: [], private_subnet: '', overlay_destination: '198.18.0.1:33333', packet_filter: false },
  masque: {
    type: EpTypes.Masque,
    tag: '',
    mode: 'server',
    listen: '0.0.0.0',
    listen_port: 443,
    transport_mode: 'connect_udp',
    tls_server_name: '',
    sui_tls_id: 0,
    member_group_ids: [],
    member_client_ids: [],
    server_auth: { policy: 'first_match' },
    ext: {},
  },
  warp_masque: {
    type: EpTypes.WarpMasque,
    tag: '',
    mode: 'client',
    server: '',
    server_port: 443,
    transport_mode: 'connect_udp',
    tls_server_name: '',
    member_group_ids: [],
    member_client_ids: [],
    profile: { compatibility: 'consumer' },
    ext: {},
  },
}

export function createEndpoint<T extends Endpoint>(type: string,json?: Partial<T>): Endpoint {
  const defaultObject: Endpoint = { ...defaultValues[type], ...(json || {}) }
  return defaultObject
}