/**
 * Empty = valid (server assigns). Non-empty must be a private IPv4 CIDR (RFC1918 / 100.64/10), mask /8–/30.
 */
export function isValidPrivateSubnetField(raw: string | undefined | null): boolean {
  const s = (raw ?? '').trim()
  if (s === '') return true
  return parsePrivateSubnet(s) !== null
}

export function parsePrivateSubnet(raw: string): { bits: number } | null {
  const s = raw.trim()
  const m = s.match(/^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})\/(\d{1,2})$/)
  if (!m) return null
  const o = [1, 2, 3, 4].map((i) => parseInt(m[i], 10))
  if (o.some((x) => x > 255 || Number.isNaN(x))) return null
  const bits = parseInt(m[5], 10)
  if (Number.isNaN(bits) || bits < 8 || bits > 30) return null
  const [a, b] = [o[0], o[1]]
  const is10 = a === 10
  const is172 = a === 172 && b >= 16 && b <= 31
  const is192 = a === 192 && o[1] === 168
  const is100 = a === 100 && b >= 64 && b <= 127
  if (!is10 && !is172 && !is192 && !is100) return null
  return { bits }
}

export function privateSubnetRuleMessage(): string {
  return 'Empty for auto, or private IPv4 CIDR (10/8, 172.16–31, 192.168/16, 100.64/10), /8–/30'
}

/** Single IPv4 CIDR string (e.g. 10.0.0.1/32). */
export function isValidIPv4CIDR(raw: string | undefined | null): boolean {
  const s = (raw ?? '').trim()
  if (!s) return false
  const m = s.match(/^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})\/(\d{1,2})$/)
  if (!m) return false
  const o = [1, 2, 3, 4].map((i) => parseInt(m[i], 10))
  if (o.some((x) => x > 255 || Number.isNaN(x))) return false
  const bits = parseInt(m[5], 10)
  return !Number.isNaN(bits) && bits >= 0 && bits <= 32
}
