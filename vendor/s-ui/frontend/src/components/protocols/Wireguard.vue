<template>
  <v-card subtitle="Wireguard">
    <v-row>
      <v-col cols="12" sm="8">
        <v-text-field v-model="data.private_key" :label="$t('types.wg.privKey')" append-icon="mdi-key-star" @click:append="newKey()" hide-details />
      </v-col>
      <v-col cols="12" sm="8">
        <v-text-field v-model="public_key" readonly :label="$t('tls.pubKey')" append-icon="mdi-refresh" @click:append="getWgPubKey()" hide-details />
      </v-col>
      <v-col cols="12" sm="8">
        <v-text-field v-model="address" :label="$t('types.wg.localIp') + ' ' + $t('commaSeparated')" hide-details />
      </v-col>
      <v-col cols="12" sm="6">
        <GroupMultiSelect v-model="data.member_group_ids" :user-groups="userGroups" :label="$t('l3router.groups')" />
      </v-col>
      <v-col cols="12" sm="6">
        <v-select v-model="data.member_client_ids" :items="clientItems" item-title="title" item-value="value" multiple chips closable-chips :label="$t('l3router.clients')" density="compact" />
      </v-col>
    </v-row>
    <v-row>
      <v-col cols="12" sm="6" md="4">
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
      <v-col cols="12" sm="6" md="4" v-if="optionCloak">
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
      <v-btn color="primary" variant="tonal" @click="addPeer">{{ $t('actions.add') }}</v-btn>
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
            <v-list-item>
              <v-switch v-model="optionForwardAllow" color="primary" label="FORWARD allow (wg peer-to-peer)" hide-details />
            </v-list-item>
            <v-list-item>
              <v-switch v-model="optionCloak" color="primary" label="WG Cloak" hide-details />
            </v-list-item>
          </v-list>
        </v-card>
      </v-menu>
    </v-card-actions>
  </v-card>
  <v-card v-if="data.peers != undefined">
    <v-card-subtitle>{{ $t('types.wg.peers') }}</v-card-subtitle>
    <v-data-table :headers="peerHeaders" :items="data.peers" density="comfortable" class="elevation-2 rounded">
      <template #item.row_id="{ index }">{{ index + 1 }}</template>
      <template #item.allowed_ips="{ item }">
        <v-text-field :model-value="joinArray(peerRow(item).allowed_ips)" @update:model-value="setAllowed(peerRow(item), $event)" density="compact" hide-details />
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
        <v-btn icon="mdi-delete" variant="text" color="error" :disabled="peerRow(item).managed" @click="delPeer(Number(index))" />
      </template>
    </v-data-table>
  </v-card>
</template>

<script lang="ts">
import GroupMultiSelect from '@/components/GroupMultiSelect.vue'
import HttpUtils from '@/plugins/httputil'
import { push } from 'notivue'
import Data from '@/store/modules/data'

export default {
  props: ['data', 'userGroups', 'clients'],
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
    peerSubnetOctet(): number {
      const list = Array.isArray(this.$props.data.address) ? this.$props.data.address : []
      for (const raw of list) {
        const m = String(raw).trim().match(/^10\.0\.(\d{1,3})\.\d{1,3}\/\d+$/)
        if (!m) continue
        const oct = Number(m[1])
        if (Number.isInteger(oct) && oct >= 0 && oct <= 254) return oct
      }
      return 1
    },
    findLowestFreePeerIP(): string {
      const subnet = this.peerSubnetOctet()
      const used = new Set<string>()
      const serverAddrs = Array.isArray(this.$props.data.address) ? this.$props.data.address : []
      serverAddrs.forEach((a: string) => used.add(String(a).trim()))
      const peers = Array.isArray(this.$props.data.peers) ? this.$props.data.peers : []
      peers.forEach((p: any) => {
        const arr = Array.isArray(p?.allowed_ips) ? p.allowed_ips : []
        arr.forEach((x: string) => used.add(String(x).trim()))
      })
      for (let host = 2; host < 255; host += 1) {
        const ip = `10.0.${subnet}.${host}/32`
        if (!used.has(ip)) return ip
      }
      return ''
    },
  },
  computed: {
    peerHeaders() {
      return [
        { title: this.$t('types.wg.colId'), key: 'row_id' },
        { title: this.$t('types.wg.colIP'), key: 'allowed_ips' },
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
      },
    },
  },
  components: { GroupMultiSelect },
}
</script>