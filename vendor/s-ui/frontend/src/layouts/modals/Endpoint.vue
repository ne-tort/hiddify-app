<template>
  <v-dialog transition="dialog-bottom-transition" width="800">
    <v-card class="rounded-lg">
      <v-card-title>
        {{ $t('actions.' + title) + " " + $t('objects.endpoint') }}
      </v-card-title>
      <v-divider></v-divider>
      <v-card-text style="padding: 0 16px; overflow-y: scroll;">
        <v-row>
          <v-col cols="12" sm="6" md="4">
            <v-select
            hide-details
            :disabled="endpoint.id > 0"
            :label="$t('type')"
            :items="Object.keys(epTypes).map((key,index) => ({title: key, value: Object.values(epTypes)[index]}))"
            v-model="endpoint.type"
            @update:modelValue="changeType">
            </v-select>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-text-field v-model="endpoint.tag" :label="$t('objects.tag')" hide-details></v-text-field>
          </v-col>
          <v-col cols="12" sm="6" md="4" v-if="endpoint.type == epTypes.L3Router" class="d-flex align-center">
            <v-switch
              v-model="endpoint.packet_filter"
              color="primary"
              :label="$t('l3router.packetFilter')"
              hide-details
              density="compact"
            />
          </v-col>
        </v-row>
        <Wireguard v-if="endpoint.type == epTypes.Wireguard"
          :data="endpoint"
          :user-groups="userGroups"
          :clients="clients"
          @getWgPubKey="getWgPubKey"
          @newWgKey="newWgKey" />
        <Awg v-else-if="endpoint.type == epTypes.Awg"
          :data="endpoint"
          :user-groups="userGroups"
          :clients="clients"
          @getWgPubKey="getWgPubKey"
          @newWgKey="newWgKey" />
        <Warp v-if="endpoint.type == epTypes.Warp" :data="endpoint" />
        <TailscaleVue v-if="endpoint.type == epTypes.Tailscale" :data="endpoint" />
        <L3Router v-if="endpoint.type == epTypes.L3Router" :data="endpoint" :user-groups="userGroups" :clients="clients" :is-new="Number(id) === 0" />
        <Dial v-if="endpoint.type != epTypes.L3Router" :dial="endpoint" />
      </v-card-text>
      <v-card-actions>
        <v-spacer></v-spacer>
        <v-btn
          color="primary"
          variant="outlined"
          @click="closeModal"
        >
          {{ $t('actions.close') }}
        </v-btn>
        <v-btn
          color="primary"
          variant="tonal"
          :loading="loading"
          :disabled="Boolean(endpointValidationError)"
          @click="saveChanges"
        >
          {{ $t('actions.save') }}
        </v-btn>
      </v-card-actions>
      <v-alert
        v-if="endpointValidationError"
        type="error"
        variant="tonal"
        density="compact"
        class="ma-4 mt-0"
      >
        {{ endpointValidationError }}
      </v-alert>
    </v-card>
  </v-dialog>
</template>

<script lang="ts">
import { EpTypes, createEndpoint } from '@/types/endpoints'
import RandomUtil from '@/plugins/randomUtil'
import Dial from '@/components/Dial.vue'
import Wireguard from '@/components/protocols/Wireguard.vue'
import Awg from '@/components/protocols/Awg.vue'
import Warp from '@/components/protocols/Warp.vue'
import TailscaleVue from '@/components/protocols/Tailscale.vue'
import L3Router from '@/components/protocols/L3Router.vue'
import HttpUtils from '@/plugins/httputil'
import { push } from 'notivue'
import { i18n } from '@/locales'
import Data from '@/store/modules/data'
import { isValidPrivateSubnetField, privateSubnetRuleMessage } from '@/utils/l3Subnet'
export default {
  props: ['visible', 'data', 'id', 'tags'],
  emits: ['close'],
  computed: {
    userGroups() {
      return Data().userGroups ?? []
    },
    clients() {
      return Data().clients ?? []
    },
    endpointValidationError(): string | null {
      if (this.endpoint.type !== EpTypes.Wireguard && this.endpoint.type !== EpTypes.Awg) return null
      return this.validateWireguardFields()
    },
  },
  data() {
    return {
      endpoint: createEndpoint("wireguard",{ "tag": "" }),
      title: "add",
      tab: "t1",
      loading: false,
      epTypes: EpTypes,
    }
  },
  methods: {
    async updateData(id: number) {
      if (id > 0) {
        const newData = JSON.parse(this.$props.data)
        this.endpoint = createEndpoint(newData.type, newData)
        this.title = "edit"
      }
      else {
        this.endpoint.type = "wireguard"
        this.endpoint.listen_port = RandomUtil.randomIntRange(10000, 60000)
        await this.changeType()
        this.title = "add"
      }
      this.tab = "t1"
    },
    async changeType() {
      // Tag change only in add endpoint
      const tag = this.endpoint.type + "-" + RandomUtil.randomSeq(3)
      
      // Use previous data
      let prevConfig = {}
      switch (this.endpoint.type) {
        case EpTypes.Wireguard:
          const wgKeys = (await this.genWgKey())
          const wgSubnet = this.nextWireguardSubnet()
          prevConfig = {
            tag: tag,
            listen_port: this.endpoint.listen_port ?? RandomUtil.randomIntRange(10000, 60000),
            address: [`10.0.${wgSubnet}.1/24`, 'fe80::1/128'],
            private_key: wgKeys.private_key,
            peers: [],
            member_group_ids: [],
            member_client_ids: [],
            ext: {
              public_key: wgKeys.public_key,
              keys: []
            }
          }
          break
        case EpTypes.Awg:
          const awgKeys = (await this.genWgKey())
          const awgSubnet = this.nextAwgSubnet()
          prevConfig = {
            tag: tag,
            type: EpTypes.Awg,
            listen_port: this.endpoint.listen_port ?? RandomUtil.randomIntRange(10000, 60000),
            address: [`10.1.${awgSubnet}.1/24`, 'fe80::1/128'],
            private_key: awgKeys.private_key,
            peers: [],
            member_group_ids: [],
            member_client_ids: [],
            ext: {
              public_key: awgKeys.public_key,
              keys: []
            }
          }
          break
        case EpTypes.Warp:
          prevConfig = {
            tag: tag,
          }
          break
        case EpTypes.Tailscale:
          prevConfig = { tag: tag }
          break
        case EpTypes.L3Router:
          prevConfig = {
            tag: tag,
            peers: [],
            packet_filter: false,
            private_subnet: '',
            overlay_destination: "198.18.0.1:33333",
            member_group_ids: [],
            member_client_ids: [],
          }
          break
      }
      this.endpoint = createEndpoint(this.endpoint.type, prevConfig)
      if (this.endpoint.type === EpTypes.L3Router && this.$props.id === 0) {
        await this.fetchNextL3PrivateSubnet()
      }
    },
    nextWireguardSubnet(): number {
      const used = new Set<number>()
      const endpoints = Data().endpoints ?? []
      endpoints.forEach((ep: any) => {
        if (ep?.type !== EpTypes.Wireguard) return
        if (Number(ep?.id ?? 0) === Number(this.$props.id ?? 0)) return
        let addrs: string[] = []
        if (Array.isArray(ep?.address)) {
          addrs = ep.address
        } else if (typeof ep?.address === 'string') {
          try {
            const parsed = JSON.parse(ep.address)
            if (Array.isArray(parsed)) addrs = parsed
          } catch {
            addrs = []
          }
        }
        addrs.forEach((addr: string) => {
          const m = String(addr).trim().match(/^10\.0\.(\d{1,3})\.\d{1,3}\/\d+$/)
          if (!m) return
          const oct = Number(m[1])
          if (Number.isInteger(oct) && oct >= 0 && oct <= 254) used.add(oct)
        })
      })
      for (let i = 0; i <= 254; i += 1) {
        if (!used.has(i)) return i
      }
      return 0
    },
    async fetchNextL3PrivateSubnet() {
      const excludeId = this.$props.id > 0 ? this.$props.id : 0
      const msg = await HttpUtils.get('api/nextL3PrivateSubnet', { excludeId })
      if (msg.success && msg.obj != null && typeof msg.obj === 'string' && msg.obj.length > 0) {
        this.endpoint.private_subnet = msg.obj
      }
    },
    closeModal() {
      this.updateData(0) // reset
      this.$emit('close')
    },
    async saveChanges() {
      if (!this.$props.visible) return
      
      // check duplicate tag
      const isDuplicatedTag = Data().checkTag("endpoint",this.endpoint.id, this.endpoint.tag)
      if (isDuplicatedTag) return

      if (this.endpoint.type === EpTypes.L3Router) {
        if (!isValidPrivateSubnetField(this.endpoint.private_subnet as string)) {
          push.error({
            message: privateSubnetRuleMessage(),
          })
          return
        }
      }
      if ((this.endpoint.type === EpTypes.Wireguard || this.endpoint.type === EpTypes.Awg) && this.endpointValidationError) {
        push.error({ message: this.endpointValidationError })
        return
      }

      // save data
      this.loading = true
      const success = await Data().save("endpoints", this.$props.id == 0 ? "new" : "edit", this.endpoint)
      if (success) this.closeModal()
      this.loading = false
    },
    normalizeCIDRToken(raw: string): string {
      return String(raw ?? '').trim().replaceAll('\\', '/')
    },
    isCIDROrIPToken(raw: string): boolean {
      const token = this.normalizeCIDRToken(raw)
      if (token.length === 0) return false
      if (token.includes('/')) {
        const parts = token.split('/')
        if (parts.length !== 2 || parts[0].length === 0 || parts[1].length === 0) return false
        return /^\d+$/.test(parts[1])
      }
      return true
    },
    validateWireguardFields(): string | null {
      const label = this.endpoint.type === EpTypes.Awg ? 'AWG' : 'WireGuard'
      const addrs = Array.isArray(this.endpoint.address) ? this.endpoint.address : []
      for (let i = 0; i < addrs.length; i += 1) {
        const n = this.normalizeCIDRToken(addrs[i])
        if (!this.isCIDROrIPToken(n)) {
          return `${label} address invalid: ${String(addrs[i])}`
        }
      }
      const peers = Array.isArray(this.endpoint.peers) ? this.endpoint.peers : []
      for (let pi = 0; pi < peers.length; pi += 1) {
        const allowed = Array.isArray(peers[pi]?.allowed_ips) ? peers[pi].allowed_ips : []
        for (let ai = 0; ai < allowed.length; ai += 1) {
          const n = this.normalizeCIDRToken(allowed[ai])
          if (!this.isCIDROrIPToken(n)) {
            return `${label} peer[${pi + 1}] allowed_ips invalid: ${String(allowed[ai])}`
          }
        }
      }
      return null
    },
    nextAwgSubnet(): number {
      const used = new Set<number>()
      const endpoints = Data().endpoints ?? []
      endpoints.forEach((ep: any) => {
        if (ep?.type !== EpTypes.Awg) return
        if (Number(ep?.id ?? 0) === Number(this.$props.id ?? 0)) return
        let addrs: string[] = []
        if (Array.isArray(ep?.address)) {
          addrs = ep.address
        } else if (typeof ep?.address === 'string') {
          try {
            const parsed = JSON.parse(ep.address)
            if (Array.isArray(parsed)) addrs = parsed
          } catch {
            addrs = []
          }
        }
        addrs.forEach((addr: string) => {
          const m = String(addr).trim().match(/^10\.1\.(\d{1,3})\.\d{1,3}\/\d+$/)
          if (!m) return
          const oct = Number(m[1])
          if (Number.isInteger(oct) && oct >= 0 && oct <= 254) used.add(oct)
        })
      })
      for (let i = 0; i <= 254; i += 1) {
        if (!used.has(i)) return i
      }
      return 0
    },
    async genWgKey(){
      this.loading = true
      const msg = await HttpUtils.get('api/keypairs', { k: "wireguard" })
      this.loading = false
      let result = { private_key: "", public_key: "" }
      if (msg.success) {
        msg.obj.forEach((line:string) => {
          if (line.startsWith("PrivateKey")){
            result.private_key = line.substring(12)
          }
          if (line.startsWith("PublicKey")){
            result.public_key = line.substring(11)
          }
        })
      } else {
        push.error({
          message: i18n.global.t('error') + ": " + msg.obj
        })
      }
      return result
    },
    async newWgKey(){
      this.loading = true
      const newKeys = await this.genWgKey()
      this.endpoint.private_key = newKeys.private_key
      if (!this.endpoint.ext) this.endpoint.ext = {keys: []}
      this.endpoint.ext.public_key = newKeys.public_key
      this.loading = false
    },
    async getWgPubKey(private_key: string) {
      if (!this.endpoint.ext) this.endpoint.ext = {keys: []}
      this.loading = true
      const msg = await HttpUtils.get('api/keypairs', { k: "wireguard", o: private_key })
      if (msg.success) {
        this.endpoint.ext.public_key = msg.obj[0]
      }
      this.loading = false
    },
  },
  watch: {
    async visible(v) {
      if (!v) return
      await this.updateData(this.$props.id)
      if (this.endpoint.type === EpTypes.Awg) {
        await Data().loadAwgObfuscationProfiles()
      }
    },
  },
  components: { Dial, Wireguard, Awg, Warp, TailscaleVue, L3Router }
}
</script>