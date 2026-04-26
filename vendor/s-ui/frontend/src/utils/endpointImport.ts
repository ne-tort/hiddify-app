import { EpTypes } from '@/types/endpoints'
import { createPeerDraft, sanitizeWgAwgByMode } from '@/components/protocols/useWgAwgFormModel'

type AnyRecord = Record<string, any>

export interface EndpointImportResult {
  warnings: string[]
  type: string
}

const WG_FIELDS = new Set([
  'type', 'tag', 'address', 'private_key', 'listen_port', 'peers', 'mtu', 'workers',
  'persistent_keepalive_interval', 'system', 'name', 'forward_allow', 'internet_allow',
  'hub_client_mode', 'member_group_ids', 'member_client_ids', 'udp_timeout',
])

const AWG_FIELDS = new Set([
  ...WG_FIELDS,
  'jc', 'jmin', 'jmax', 's1', 's2', 's3', 's4',
  'h1', 'h2', 'h3', 'h4', 'i1', 'i2', 'i3', 'i4', 'i5',
  'obfuscation_profile_id',
])

export function parseEndpointImport(raw: string): AnyRecord {
  const parsed = JSON.parse(raw)
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    throw new Error('Expected JSON object')
  }
  return parsed
}

export function applyImportedEndpointToDraft(draft: AnyRecord, imported: AnyRecord): EndpointImportResult {
  const warnings: string[] = []
  const sourceType = normalizeType(imported.type)
  if (sourceType !== EpTypes.Wireguard && sourceType !== EpTypes.Awg) {
    throw new Error('Only WireGuard/AWG JSON import is supported')
  }
  if (draft.type !== sourceType) {
    warnings.push(`type '${sourceType}' ignored: current form is '${draft.type}'`)
  }
  const accepted = draft.type === EpTypes.Awg ? AWG_FIELDS : WG_FIELDS
  Object.keys(imported).forEach((k) => {
    if (!accepted.has(k)) {
      warnings.push(`unknown field ignored: ${k}`)
    }
  })

  if (Array.isArray(imported.address)) {
    draft.address = imported.address.map((x) => String(x).trim()).filter((x) => x.length > 0)
  } else if (typeof imported.address === 'string' && imported.address.trim().length > 0) {
    draft.address = [imported.address.trim()]
  }
  if (typeof imported.private_key === 'string') draft.private_key = imported.private_key.trim()
  if (typeof imported.listen_port === 'number') draft.listen_port = Math.floor(imported.listen_port)
  if (typeof imported.persistent_keepalive_interval === 'number') draft.persistent_keepalive_interval = Math.floor(imported.persistent_keepalive_interval)
  if (typeof imported.mtu === 'number') draft.mtu = Math.floor(imported.mtu)
  if (typeof imported.hub_client_mode === 'boolean') draft.hub_client_mode = imported.hub_client_mode

  if (Array.isArray(imported.peers)) {
    const peers = imported.peers
      .filter((p) => p && typeof p === 'object')
      .map((p) => createPeerDraft({
        address: str(p.address),
        port: num(p.port, 51820),
        public_key: str(p.public_key),
        private_key: str(p.private_key),
        allowed_ips: Array.isArray(p.allowed_ips) ? p.allowed_ips.map((x: unknown) => String(x).trim()).filter((x: string) => x.length > 0) : [],
        persistent_keepalive_interval: num(p.persistent_keepalive_interval, 25),
      }))
    draft.peers = peers
    if (peers.length === 1 && peers[0].address && peers[0].public_key) {
      draft.hub_client_mode = true
    } else if (peers.length > 1) {
      draft.hub_client_mode = false
    }
  }

  if (draft.type === EpTypes.Awg) {
    for (const f of ['jc', 'jmin', 'jmax', 's1', 's2', 's3', 's4']) {
      if (typeof imported[f] === 'number') draft[f] = Math.floor(imported[f])
    }
    for (const f of ['h1', 'h2', 'h3', 'h4', 'i1', 'i2', 'i3', 'i4', 'i5']) {
      if (typeof imported[f] === 'string') draft[f] = imported[f]
    }
  }

  sanitizeWgAwgByMode(draft)
  return { warnings, type: draft.type }
}

function normalizeType(v: unknown): string {
  const s = String(v ?? '').toLowerCase().trim()
  if (s === 'amneziawg') return EpTypes.Awg
  return s
}

function str(v: unknown): string {
  return typeof v === 'string' ? v.trim() : ''
}

function num(v: unknown, fallback: number): number {
  const n = Number(v)
  return Number.isFinite(n) && n > 0 ? Math.floor(n) : fallback
}
