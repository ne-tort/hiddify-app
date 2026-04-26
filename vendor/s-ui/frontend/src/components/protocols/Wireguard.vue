<template>
  <v-card :subtitle="wireguardCardSubtitle">
    <v-row>
      <v-col cols="12" sm="8">
        <v-text-field v-model="data.private_key" :label="$t('types.wg.privKey')" append-icon="mdi-key-star" @click:append="newKey()" hide-details />
      </v-col>
      <v-col cols="12" sm="8">
        <v-text-field
          v-model="public_key"
          :readonly="!hubClientMode"
          :label="$t('tls.pubKey')"
          append-icon="mdi-refresh"
          @click:append="getWgPubKey()"
          hide-details
        />
      </v-col>
      <v-col cols="12" sm="8">
        <v-text-field v-model="address" :label="$t('types.wg.localIp') + ' ' + $t('commaSeparated')" hide-details />
      </v-col>
      <v-col v-if="!hubClientMode" cols="12" sm="6">
        <GroupMultiSelect v-model="data.member_group_ids" :user-groups="userGroups" :label="$t('l3router.groups')" />
      </v-col>
      <v-col v-if="!hubClientMode" cols="12" sm="6">
        <v-select v-model="data.member_client_ids" :items="clientItems" item-title="title" item-value="value" multiple chips closable-chips :label="$t('l3router.clients')" density="compact" />
      </v-col>
    </v-row>
    <v-row>
      <v-col v-if="!hubClientMode" cols="12" sm="6" md="4">
        <v-text-field :label="$t('in.port')" hide-details type="number" min="1" v-model.number="data.listen_port" />
      </v-col>
      <v-col cols="12" sm="6" md="4" v-if="data.udp_timeout != undefined">
        <v-text-field label="UDP Timeout" hide-details type="number" min="0" :suffix="$t('date.m')" v-model.number="udp_timeout" />
      </v-col>
    </v-row>
    <v-row>
      <v-col cols="12" sm="6" md="4" v-if="data.workers != undefined">
        <v-text-field :label="$t('types.wg.worker')" hide-details type="number" min="1" v-model.number="data.workers" />
      </v-col>
      <v-col cols="12" sm="6" md="4" v-if="data.mtu != undefined">
        <v-text-field label="MTU" hide-details type="number" min="0" v-model.number="data.mtu" />
      </v-col>
      <v-col cols="12" sm="6" md="4" v-if="data.persistent_keepalive_interval != undefined">
        <v-text-field label="Keepalive" hide-details type="number" min="1" suffix="s" v-model.number="keepalive" />
      </v-col>
    </v-row>
    <v-row>
      <v-col cols="12" sm="8">
        <v-text-field v-model="data.ext.dns" :label="$t('dns.title') + ' ' + $t('commaSeparated')" hide-details />
      </v-col>
    </v-row>
    <v-row>
      <v-col cols="12" sm="6" md="4">
        <v-switch v-model="data.system" color="primary" :label="$t('types.wg.sysIf')" hide-details />
      </v-col>
      <v-col cols="12" sm="6" md="4" v-if="data.system">
        <v-text-field :label="$t('types.wg.ifName')" hide-details v-model="ifName" />
      </v-col>
      <v-col cols="12" sm="6" md="4" v-if="optionCloak && !hideWgOnlyOptions">
        <v-select
          v-model="cloakDetourTag"
          :items="detourTags"
          label="WG Cloak Detour"
          clearable
          hide-details
        />
      </v-col>
    </v-row>
    <v-card-actions>
      <v-btn color="primary" variant="tonal" :disabled="hubClientMode" @click="addPeer">{{ $t('actions.add') }}</v-btn>
      <v-spacer />
      <v-menu v-model="menu" :close-on-content-click="false" location="start">
        <template #activator="{ props }">
          <v-btn v-bind="props" hide-details variant="tonal">{{ $t('types.wg.options') }}</v-btn>
        </template>
        <v-card>
          <v-list>
            <v-list-item><v-switch v-model="optionUdp" color="primary" label="UDP Timeout" hide-details /></v-list-item>
            <v-list-item><v-switch v-model="optionWorker" color="primary" :label="$t('types.wg.worker')" hide-details /></v-list-item>
            <v-list-item><v-switch v-model="optionMtu" color="primary" label="MTU" hide-details /></v-list-item>
            <v-list-item><v-switch v-model="optionKeepalive" color="primary" label="Keepalive" hide-details /></v-list-item>
            <v-list-item><v-switch v-model="optionIPv6" color="primary" :label="$t('types.wg.ipv6')" hide-details /></v-list-item>
            <v-list-item>
              <v-switch v-model="optionForwardAllow" color="primary" :label="$t('types.wg.peerToPeer')" hide-details />
            </v-list-item>
            <v-list-item>
              <v-switch v-model="optionInternetAllow" color="primary" :label="$t('types.wg.internet')" hide-details />
            </v-list-item>
            <v-list-item v-if="!hideWgOnlyOptions">
              <v-switch v-model="optionCloak" color="primary" label="WG Cloak" hide-details />
            </v-list-item>
            <v-list-item>
              <v-switch v-model="optionHubClientMode" color="primary" :label="$t('types.wg.hubClientMode')" hide-details />
            </v-list-item>
          </v-list>
        </v-card>
      </v-menu>
    </v-card-actions>
  </v-card>
  <v-card v-if="hubClientMode">
    <v-card-subtitle>{{ $t('types.wg.peers') }}</v-card-subtitle>
    <v-row class="px-3 pb-3">
      <v-col cols="12" sm="8">
        <v-text-field
          v-model="hubPeerAddress"
          :label="$t('types.wg.hubHost')"
          hide-details
        />
      </v-col>
      <v-col cols="12" sm="4">
        <v-text-field
          v-model.number="hubPeerPort"
          :label="$t('types.wg.hubPort')"
          type="number"
          min="1"
          max="65535"
          hide-details
        />
      </v-col>
      <v-col cols="12" sm="8">
        <v-text-field
          v-model="hubPeerPublicKey"
          :label="$t('types.wg.pubKey')"
          hide-details
        />
      </v-col>
      <v-col cols="12">
        <v-text-field
          :model-value="joinArray(upstreamPeer.allowed_ips)"
          @update:model-value="setAllowed(upstreamPeer, $event)"
          :label="$t('types.wg.allowedIp') + ' ' + $t('commaSeparated')"
          hide-details
        />
      </v-col>
    </v-row>
  </v-card>
  <v-card v-else-if="data.peers != undefined">
    <v-card-subtitle>{{ $t('types.wg.peers') }}</v-card-subtitle>
    <v-data-table :headers="peerHeaders" :items="data.peers" density="comfortable" class="elevation-2 rounded">
      <template #item.row_id="{ index }">{{ index + 1 }}</template>
      <template #header.peer_exit>
        <v-tooltip location="top">
          <template #activator="{ props: htip }">
            <v-icon v-bind="htip" size="small" class="ms-1">mdi-transit-connection-variant</v-icon>
          </template>
          <span>{{ $t('types.wg.exitNodeTooltip') }}</span>
        </v-tooltip>
      </template>
      <template #item.allowed_ips="{ item }">
        <v-text-field :model-value="joinArray(peerRow(item).allowed_ips)" @update:model-value="setAllowed(peerRow(item), $event)" density="compact" hide-details />
      </template>
      <template #item.peer_exit="{ item }">
        <v-tooltip location="top">
          <template #activator="{ props: tip }">
            <v-btn
              v-bind="tip"
              icon="mdi-transit-connection-variant"
              size="small"
              :color="peerRow(item).peer_exit === true ? 'primary' : undefined"
              :variant="peerRow(item).peer_exit === true ? 'tonal' : 'text'"
              @click="togglePeerExit(peerRow(item))"
            />
          </template>
          <span>{{ $t('types.wg.exitNodeTooltip') }}</span>
        </v-tooltip>
      </template>
      <template #item.client_name="{ item }">{{ peerRow(item).client_name || '-' }}</template>
      <template #item.private_key="{ item }">
        <v-tooltip :text="$t('copyToClipboard')" location="top">
          <template #activator="{ props }">
            <v-btn v-bind="props" icon="mdi-content-copy" size="small" variant="text" :disabled="!peerRow(item).private_key" @click="copyValue(peerRow(item).private_key)" />
          </template>
        </v-tooltip>
      </template>
      <template #item.public_key="{ item }">
        <v-tooltip :text="$t('copyToClipboard')" location="top">
          <template #activator="{ props }">
            <v-btn v-bind="props" icon="mdi-content-copy" size="small" variant="text" :disabled="!peerRow(item).public_key" @click="copyValue(peerRow(item).public_key)" />
          </template>
        </v-tooltip>
      </template>
      <template #item.managed="{ item }">
        <v-chip size="small" :color="peerRow(item).managed ? 'primary' : 'grey'">
          {{ peerRow(item).managed ? $t('types.wg.modeManaged') : $t('types.wg.modeManual') }}
        </v-chip>
      </template>
      <template #item.actions="{ index, item }">
        <v-btn
          icon="mdi-delete"
          variant="text"
          color="error"
          :disabled="peerRow(item).managed || hubClientMode"
          @click="delPeer(Number(index))"
        />
      </template>
    </v-data-table>
  </v-card>
</template>

<script lang="ts">
import GroupMultiSelect from '@/components/GroupMultiSelect.vue'
import HttpUtils from '@/plugins/httputil'
import { push } from 'notivue'
import Data from '@/store/modules/data'
import { createPeerDraft, ensurePeersArray, sanitizeWgAwgByMode } from '@/components/protocols/useWgAwgFormModel'

export default {
  props: {
    data: { type: Object, required: true },
    userGroups: { type: Array, default: () => [] },
    clients: { type: Array, default: () => [] },
    hideWgOnlyOptions: { type: Boolean, default: false },
    /** Optional card subtitle (e.g. AWG uses WireGuard-compatible server block). */
    cardSubtitle: { type: String, default: '' },
  },
  emits: ['newWgKey', 'getWgPubKey'],
  data() {
    return {
      menu: false,
    }
  },
  methods: {
    async addPeer() {
      const nextIp = this.findLowestFreePeerIP()
      const newKeys = await this.genWgKeySafe()
      this.$props.data.peers.push({
        public_key: newKeys.public_key,
        private_key: newKeys.private_key,
        allowed_ips: nextIp ? [nextIp] : [],
        managed: false,
      })
    },
    delPeer(id: number) {
      this.$props.data.peers.splice(id, 1)
    },
    togglePeerExit(row: Record<string, unknown>) {
      if (row.peer_exit === true) {
        row.peer_exit = false
        return
      }
      const peers = this.$props.data.peers as unknown[]
      for (const p of peers) {
        const o = p && typeof p === 'object' ? (p as Record<string, unknown>) : null
        if (!o) {
          continue
        }
        o.peer_exit = false
      }
      row.peer_exit = true
    },
    newKey() {
      this.$emit('newWgKey')
    },
    getWgPubKey() {
      const privKey = this.$props.data.private_key
      if (privKey.length == 0) return
      this.$emit('getWgPubKey', privKey)
    },
    copyValue(v: string) {
      if (!v) return
      navigator.clipboard.writeText(v)
      push.success({ message: this.$t('copyToClipboard') as string })
    },
    peerRow(item: any) {
      return item?.raw ?? item
    },
    joinArray(v: unknown) {
      return Array.isArray(v) ? v.join(',') : ''
    },
    setAllowed(item: any, value: string) {
      item.allowed_ips = String(value ?? '').trim().length > 0 ? String(value).split(',').map((x) => x.trim()) : []
    },
    ensureUpstreamPeer() {
      ensurePeersArray(this.$props.data)
      if (!this.$props.data.peers[0]) {
        this.$props.data.peers.push(createPeerDraft())
      }
      return this.$props.data.peers[0]
    },
    async genWgKeySafe() {
      const fallback = { private_key: '', public_key: '' }
      const msg = await HttpUtils.get('api/keypairs', { k: 'wireguard' })
      if (!msg.success || !Array.isArray(msg.obj)) return fallback
      const out = { ...fallback }
      msg.obj.forEach((line: string) => {
        if (line.startsWith('PrivateKey')) out.private_key = line.substring(12)
        if (line.startsWith('PublicKey')) out.public_key = line.substring(11)
      })
      return out
    },
    peerIPv4Base(): string {
      const list = Array.isArray(this.$props.data.address) ? this.$props.data.address : []
      for (const raw of list) {
        const value = String(raw ?? '').trim()
        if (!value.includes('.')) continue
        const host = value.split('/')[0]?.trim() ?? ''
        const parts = host.split('.').map((x) => Number(x))
        if (parts.length !== 4) continue
        if (parts.some((n) => !Number.isInteger(n) || n < 0 || n > 255)) continue
        return `${parts[0]}.${parts[1]}.${parts[2]}`
      }
      return this.$props.data.type === 'awg' ? '10.1.1' : '10.0.1'
    },
    findLowestFreePeerIP(): string {
      const base = this.peerIPv4Base()
      const used = new Set<string>()
      const serverAddrs = Array.isArray(this.$props.data.address) ? this.$props.data.address : []
      serverAddrs.forEach((a: string) => {
        const raw = String(a).trim()
        used.add(raw)
        const host = raw.split('/')[0]?.trim() ?? ''
        if (host.includes('.')) {
          used.add(`${host}/32`)
        }
      })
      const peers = Array.isArray(this.$props.data.peers) ? this.$props.data.peers : []
      peers.forEach((p: any) => {
        const arr = Array.isArray(p?.allowed_ips) ? p.allowed_ips : []
        arr.forEach((x: string) => used.add(String(x).trim()))
      })
      for (let host = 2; host < 255; host += 1) {
        const ip = `${base}.${host}/32`
        if (!used.has(ip)) return ip
      }
      return ''
    },
  },
  computed: {
    wireguardCardSubtitle(): string {
      const s = String(this.$props.cardSubtitle ?? '').trim()
      return s.length > 0 ? s : 'Wireguard'
    },
    hubClientMode(): boolean {
      return this.$props.data.hub_client_mode === true
    },
    optionHubClientMode: {
      get(): boolean {
        return this.$props.data.hub_client_mode === true
      },
      set(v: boolean) {
        this.$props.data.hub_client_mode = v
        sanitizeWgAwgByMode(this.$props.data)
      },
    },
    peerHeaders() {
      return [
        { title: this.$t('types.wg.colId'), key: 'row_id' },
        { title: this.$t('types.wg.colIP'), key: 'allowed_ips' },
        { title: '', key: 'peer_exit', width: 52, sortable: false },
        { title: this.$t('types.wg.colUser'), key: 'client_name' },
        { title: this.$t('types.wg.colPrivKey'), key: 'private_key', sortable: false },
        { title: this.$t('types.wg.colPubKey'), key: 'public_key', sortable: false },
        { title: this.$t('types.wg.colMode'), key: 'managed' },
        { title: '', key: 'actions', sortable: false, width: 48 },
      ]
    },
    clientItems() {
      const cl = this.$props.clients ?? []
      return cl.map((c: any) => ({ title: c.name, value: c.id }))
    },
    detourTags() {
      const tags = (Data().inbounds ?? [])
        .map((inb: any) => String(inb?.tag ?? '').trim())
        .filter((t: string) => t.length > 0)
      return ['select', ...[...new Set(tags)].filter((t: string) => t !== 'select')]
    },
    optionUdp: {
      get(): boolean { return this.$props.data.udp_timeout != undefined },
      set(v:boolean) { this.$props.data.udp_timeout = v ? "5m" : undefined }
    },
    optionRsrv: {
      get(): boolean { return this.$props.data.reserved != undefined },
      set(v:boolean) { this.$props.data.reserved = v ? [0,0,0] : undefined }
    },
    optionWorker: {
      get(): boolean { return this.$props.data.workers != undefined },
      set(v:boolean) { this.$props.data.workers = v ? 2 : undefined }
    },
    optionMtu: {
      get(): boolean { return this.$props.data.mtu != undefined },
      set(v:boolean) { this.$props.data.mtu = v ? 1408 : undefined }
    },
    optionKeepalive: {
      get(): boolean { return this.$props.data.persistent_keepalive_interval != undefined },
      set(v:boolean) { this.$props.data.persistent_keepalive_interval = v ? 25 : undefined }
    },
    optionForwardAllow: {
      get(): boolean { return this.$props.data.forward_allow === true },
      set(v:boolean) { this.$props.data.forward_allow = v }
    },
    optionInternetAllow: {
      get(): boolean { return this.$props.data.internet_allow !== false },
      set(v:boolean) { this.$props.data.internet_allow = v }
    },
    optionIPv6: {
      get(): boolean {
        const list = Array.isArray(this.$props.data.address) ? this.$props.data.address : []
        return list.some((x: string) => String(x).includes(':'))
      },
      set(v:boolean) {
        const ula = this.$props.data.type === 'awg' ? 'fdac:0:0:2::1/64' : 'fdac:0:0:1::1/64'
        let list = Array.isArray(this.$props.data.address) ? [...this.$props.data.address] : []
        list = list.map((x: string) => {
          const s = String(x).trim()
          if (s.toLowerCase().startsWith('fe80:')) return ula
          return s
        })
        const v4 = list.filter((x: string) => !String(x).includes(':'))
        const v6 = list.filter((x: string) => String(x).includes(':'))
        if (!v) {
          this.$props.data.address = v4.length > 0 ? v4 : undefined
          return
        }
        if (v6.length === 0) {
          v4.push(ula)
        }
        this.$props.data.address = [...v4, ...v6]
      }
    },
    optionCloak: {
      get(): boolean { return this.$props.data.cloak_enabled === true },
      set(v:boolean) {
        this.$props.data.cloak_enabled = v
        if (!v) {
          this.$props.data.cloak_detour_tag = undefined
          return
        }
        if (!this.$props.data.cloak_detour_tag) {
          this.$props.data.cloak_detour_tag = 'select'
        }
      }
    },
    cloakDetourTag: {
      get() { return this.$props.data.cloak_detour_tag ?? undefined },
      set(v:string | null) {
        const next = String(v ?? '').trim()
        this.$props.data.cloak_detour_tag = next.length > 0 ? next : undefined
      }
    },
    ifName: {
      get() { return this.$props.data.name?? '' },
      set(v:string) { this.$props.data.name = v.length > 0 ? v : undefined }
    },
    address: {
      get() { return this.$props.data.address?.join(',') },
      set(v:string) {
        const next = String(v ?? '')
          .split(',')
          .map((x) => x.trim())
          .filter((x) => x.length > 0)
        this.$props.data.address = next.length > 0 ? next : undefined
      }
    },
    reserved: {
      get() { return this.$props.data.reserved?.join(',') },
      set(v:string) { 
        if(!v.endsWith(',')) {
          this.$props.data.reserved = v.length > 0 ? v.split(',').map(str => parseInt(str, 10)) : []
        }
      }
    },
    udp_timeout: {
      get() { return this.$props.data.udp_timeout ? parseInt(this.$props.data.udp_timeout.replace('m','')) : 5 },
      set(v:number) { this.$props.data.udp_timeout = v > 0 ? v + 'm' : '5m' }
    },
    keepalive: {
      get() { return this.$props.data.persistent_keepalive_interval ?? 25 },
      set(v:number) { this.$props.data.persistent_keepalive_interval = v > 0 ? Math.floor(v) : 25 }
    },
    public_key: {
      get() { return this.$props.data.ext?.public_key?? '' },
      set(v:string) { this.$props.data.ext.public_key = v }
    },
    upstreamPeer() {
      return this.ensureUpstreamPeer()
    },
    hubPeerAddress: {
      get() { return String(this.upstreamPeer.address ?? '') },
      set(v: string) { this.upstreamPeer.address = String(v ?? '').trim() }
    },
    hubPeerPort: {
      get() { return Number(this.upstreamPeer.port ?? 51820) },
      set(v: number) {
        const n = Number(v)
        this.upstreamPeer.port = Number.isFinite(n) && n > 0 ? Math.floor(n) : 51820
      }
    },
    hubPeerPublicKey: {
      get() { return String(this.upstreamPeer.public_key ?? '') },
      set(v: string) { this.upstreamPeer.public_key = String(v ?? '').trim() }
    }
  },
  watch: {
    'data.member_group_ids': {
      immediate: true,
      handler(v: unknown) {
        if (!Array.isArray(v)) this.$props.data.member_group_ids = []
      },
    },
    'data.member_client_ids': {
      immediate: true,
      handler(v: unknown) {
        if (!Array.isArray(v)) this.$props.data.member_client_ids = []
      },
    },
    'data.peers': {
      immediate: true,
      handler(v: unknown) {
        if (!Array.isArray(v)) this.$props.data.peers = []
        sanitizeWgAwgByMode(this.$props.data)
      },
    },
  },
  components: { GroupMultiSelect },
}
</script>