<template>
  <v-card :subtitle="$t('masque.title')">
    <v-tabs v-model="masqueSideTab" density="compact" fixed-tabs align-tabs="center" class="mb-2">
      <v-tab value="server">{{ $t('masque.tabServer') }}</v-tab>
      <v-tab value="client">{{ $t('masque.tabClient') }}</v-tab>
    </v-tabs>
    <v-window v-model="masqueSideTab">
      <v-window-item value="server">
        <v-row>
          <v-col cols="12" sm="6">
            <v-text-field v-model="data.listen" :label="$t('masque.serverListen')" hide-details />
          </v-col>
          <v-col cols="12" sm="6">
            <v-text-field v-model.number="data.listen_port" type="number" min="1" max="65535" :label="$t('in.port')" hide-details />
          </v-col>
        </v-row>
        <v-row>
          <v-col cols="12">
            <v-select
              v-model="suiAuthModesServer"
              :items="authModeItems"
              :label="$t('masque.authModes')"
              multiple
              chips
              closable-chips
              hide-details
            />
          </v-col>
        </v-row>
        <v-row class="mt-2">
          <v-col cols="12" sm="6">
            <GroupMultiSelect v-model="data.member_group_ids" :user-groups="userGroups" :label="$t('l3router.groups')" />
          </v-col>
          <v-col cols="12" sm="6">
            <v-select
              v-model="data.member_client_ids"
              :items="clientItems"
              item-title="title"
              item-value="value"
              multiple
              chips
              closable-chips
              :label="$t('l3router.clients')"
              density="compact"
            />
          </v-col>
        </v-row>
        <v-card :subtitle="$t('objects.tls')" style="background-color: inherit;" class="mt-2">
          <v-row>
            <v-col cols="12" sm="6" md="4">
              <v-select
                v-model.number="data.sui_tls_id"
                :items="tlsSelectItems"
                :label="$t('template')"
                clearable
                hide-details
              />
            </v-col>
          </v-row>
        </v-card>
      </v-window-item>
      <v-window-item value="client">
        <v-row>
          <v-col cols="12">
            <v-select
              v-model="suiClientAuthModes"
              :items="authModeItems"
              :label="$t('masque.subscriptionAuthModes')"
              multiple
              chips
              closable-chips
              hide-details
            />
          </v-col>
        </v-row>
        <v-row>
          <v-col cols="12" sm="6">
            <v-select
              v-model="transportModeClient"
              :items="transportModeItems"
              :label="$t('masque.transportMode')"
              hide-details
            />
          </v-col>
          <v-col cols="12" sm="6">
            <v-select v-model="httpLayerClient" :items="httpLayerItems" :label="$t('masque.httpLayer')" hide-details />
          </v-col>
        </v-row>
        <v-card class="mt-2" style="background-color: inherit;">
          <v-card-text>
            <v-card-subtitle>
              {{ $t('in.multiDomain') }}
              <v-chip color="primary" density="compact" variant="elevated" class="ms-2" @click="addAddr">
                <v-icon icon="mdi-plus" />
              </v-chip>
            </v-card-subtitle>
            <template v-for="(addr, index) in clientAddrs" :key="'mq-addr-' + index">
              <div class="d-flex align-center my-1">
                <span>{{ $t('in.addr') }} #{{ index + 1 }}</span>
                <v-icon class="ms-2" icon="mdi-delete" color="error" @click="removeAddr(index)" />
              </div>
              <v-divider />
              <AddrVue :addr="addr" :has-tls="true" />
            </template>
          </v-card-text>
        </v-card>
      </v-window-item>
    </v-window>
  </v-card>
</template>

<script lang="ts">
import GroupMultiSelect from '@/components/GroupMultiSelect.vue'
import AddrVue from '@/components/Addr.vue'

const emptySelect = { title: '\u00a0', value: '' }

export default {
  components: { GroupMultiSelect, AddrVue },
  props: {
    data: { type: Object, required: true },
    userGroups: { type: Array, default: () => [] },
    clients: { type: Array, default: () => [] },
    tlsConfigs: { type: Array, default: () => [] },
  },
  data() {
    return {
      masqueSideTab: 'server',
    }
  },
  computed: {
    clientAddrs(): any[] {
      const sub = this.ensureSuiSub()
      if (!Array.isArray(sub.addrs)) {
        sub.addrs = []
      }
      return sub.addrs
    },
    tlsSelectItems() {
      const rows = (this.$props.tlsConfigs ?? []) as any[]
      return [{ title: this.$t('none'), value: 0 }, ...rows.map((t) => ({ title: t.name, value: t.id }))]
    },
    transportModeItems() {
      return [
        emptySelect,
        { title: 'auto', value: 'auto' },
        { title: 'connect_udp', value: 'connect_udp' },
        { title: 'connect_ip', value: 'connect_ip' },
      ]
    },
    httpLayerItems() {
      return [
        emptySelect,
        { title: 'h3', value: 'h3' },
        { title: 'h2', value: 'h2' },
        { title: 'auto', value: 'auto' },
      ]
    },
    clientItems() {
      const cl = this.$props.clients ?? []
      return (cl as any[]).map((c) => ({ title: c.name, value: c.id }))
    },
    authModeItems() {
      return [
        { title: this.$t('masque.authBearer'), value: 'bearer' },
        { title: this.$t('masque.authBasic'), value: 'basic' },
        { title: this.$t('masque.authMtls'), value: 'mtls' },
      ]
    },
    suiAuthModesServer: {
      get(): string[] {
        if (!Array.isArray(this.data.sui_auth_modes)) {
          return []
        }
        return [...(this.data.sui_auth_modes as string[])]
      },
      set(v: string[]) {
        this.data.sui_auth_modes = v && v.length ? [...v] : []
      },
    },
    suiClientAuthModes: {
      get(): string[] {
        const sub = this.ensureSuiSub()
        if (!Array.isArray(sub.sui_client_auth_modes)) {
          return []
        }
        return [...(sub.sui_client_auth_modes as string[])]
      },
      set(v: string[]) {
        const sub = this.ensureSuiSub()
        if (v && v.length) {
          sub.sui_client_auth_modes = [...v]
        } else {
          delete sub.sui_client_auth_modes
          this.pruneEmptySuiSub()
        }
      },
    },
    transportModeClient: {
      get(): string {
        const sub = this.ensureSuiSub()
        return (sub.transport_mode as string) ?? ''
      },
      set(v: string) {
        const sub = this.ensureSuiSub()
        if (!v || !String(v).trim()) {
          delete sub.transport_mode
        } else {
          sub.transport_mode = v
        }
        this.pruneEmptySuiSub()
      },
    },
    httpLayerClient: {
      get(): string {
        const sub = this.ensureSuiSub()
        return (sub.http_layer as string) ?? ''
      },
      set(v: string) {
        const sub = this.ensureSuiSub()
        if (!v || !String(v).trim()) {
          delete sub.http_layer
        } else {
          sub.http_layer = v
        }
        this.pruneEmptySuiSub()
      },
    },
  },
  methods: {
    ensureSuiSub(): Record<string, any> {
      const d = this.data as any
      if (!d.sui_sub || typeof d.sui_sub !== 'object') {
        d.sui_sub = {}
      }
      return d.sui_sub
    },
    pruneEmptySuiSub() {
      const d = this.data as any
      const sub = d.sui_sub
      if (!sub || typeof sub !== 'object') return
      const keys = Object.keys(sub)
      if (keys.length === 0) {
        delete d.sui_sub
      }
    },
    addAddr() {
      const sub = this.ensureSuiSub()
      if (!Array.isArray(sub.addrs)) {
        sub.addrs = []
      }
      const d = this.data as any
      const lp = Number(d.listen_port) > 0 ? Number(d.listen_port) : 443
      const host = typeof location !== 'undefined' ? location.hostname : ''
      sub.addrs.push({ server: host, server_port: lp })
    },
    removeAddr(index: number) {
      const sub = this.ensureSuiSub()
      if (!Array.isArray(sub.addrs)) return
      sub.addrs.splice(index, 1)
      if (sub.addrs.length === 0) {
        delete sub.addrs
      }
      this.pruneEmptySuiSub()
    },
  },
}
</script>
