type AnyRecord = Record<string, any>

const DEFAULT_PEER_PORT = 51820

export function ensurePeersArray(endpoint: AnyRecord): AnyRecord[] {
  if (!Array.isArray(endpoint.peers)) {
    endpoint.peers = []
  }
  return endpoint.peers
}

export function sanitizeWgAwgByMode(endpoint: AnyRecord): void {
  const peers = ensurePeersArray(endpoint)
  if (endpoint.system !== true) {
    delete endpoint.gso_enabled
    delete endpoint.kernel_path_enabled
  }
  if (endpoint.hub_client_mode === true) {
    endpoint.listen_port = undefined
    endpoint.member_group_ids = []
    endpoint.member_client_ids = []
    for (const raw of peers) {
      if (raw && typeof raw === 'object') {
        delete raw.peer_exit
      }
    }
    if (peers.length === 0) {
      peers.push(createPeerDraft())
      return
    }
    const normalized = pickPrimaryPeer([peers[0]])
    Object.assign(peers[0], normalized)
    if (peers.length > 1) {
      peers.splice(1)
    }
    return
  }
  if (typeof endpoint.listen_port !== 'number' || endpoint.listen_port <= 0) {
    endpoint.listen_port = randomPort()
  }
}

export function createPeerDraft(seed?: Partial<AnyRecord>): AnyRecord {
  return {
    public_key: '',
    private_key: '',
    address: '',
    port: DEFAULT_PEER_PORT,
    allowed_ips: [],
    persistent_keepalive_interval: 25,
    managed: false,
    ...(seed ?? {}),
  }
}

export function pickPrimaryPeer(peers: AnyRecord[]): AnyRecord {
  const first = peers[0] ?? {}
  const base = createPeerDraft(first)
  base.managed = false
  if (!Array.isArray(base.allowed_ips)) {
    base.allowed_ips = []
  }
  if (typeof base.address !== 'string') {
    base.address = ''
  }
  if (typeof base.public_key !== 'string') {
    base.public_key = ''
  }
  if (typeof base.private_key !== 'string') {
    base.private_key = ''
  }
  if (typeof base.port !== 'number' || base.port <= 0) {
    base.port = DEFAULT_PEER_PORT
  }
  return base
}

function randomPort(): number {
  return 10000 + Math.floor(Math.random() * 50000)
}
