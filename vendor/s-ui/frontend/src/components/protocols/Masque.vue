<template>
  <v-card :subtitle="$t('masque.title')">
    <v-row>
      <v-col cols="12" sm="6">
        <v-select
          v-model="data.mode"
          :items="modeItems"
          :label="$t('masque.mode')"
          hide-details
        />
      </v-col>
      <v-col cols="12" sm="6">
        <v-text-field v-model="data.transport_mode" :label="$t('masque.transportMode')" hide-details />
      </v-col>
    </v-row>
    <v-row v-if="isServer">
      <v-col cols="12" sm="6">
        <v-text-field v-model="data.listen" :label="$t('masque.serverListen')" hide-details />
      </v-col>
      <v-col cols="12" sm="6">
        <v-text-field v-model.number="data.listen_port" type="number" min="1" max="65535" :label="$t('in.port')" hide-details />
      </v-col>
    </v-row>
    <v-row v-if="isServer">
      <v-col cols="12" sm="8">
        <v-select
          v-model.number="data.sui_tls_id"
          :items="tlsSelectItems"
          :label="$t('masque.tlsCertificate')"
          clearable
          hide-details
        />
      </v-col>
    </v-row>
    <v-row v-if="!isServer">
      <v-col cols="12" sm="8">
        <v-text-field v-model="data.server" label="Server" hide-details />
      </v-col>
      <v-col cols="12" sm="4">
        <v-text-field v-model.number="data.server_port" type="number" min="1" max="65535" :label="$t('in.port')" hide-details />
      </v-col>
    </v-row>
    <v-row>
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
    <v-row>
      <v-col cols="12" sm="6">
        <v-text-field v-model="data.tls_server_name" :label="$t('masque.tlsServerName')" hide-details />
      </v-col>
      <v-col cols="12" sm="6">
        <v-text-field v-model="data.http_layer" :label="$t('masque.httpLayer')" hide-details />
      </v-col>
    </v-row>
    <v-row>
      <v-col cols="12">
        <v-text-field v-model="data.template_udp" :label="$t('masque.templateUdp')" hide-details />
      </v-col>
      <v-col cols="12">
        <v-text-field v-model="data.template_ip" :label="$t('masque.templateIp')" hide-details />
      </v-col>
      <v-col cols="12">
        <v-text-field v-model="data.template_tcp" :label="$t('masque.templateTcp')" hide-details />
      </v-col>
    </v-row>
    <v-row v-if="isServer">
      <v-col cols="12">
        <v-textarea v-model="serverAuthJson" :label="$t('masque.serverAuthHint')" rows="4" variant="outlined" hide-details spellcheck="false" />
      </v-col>
    </v-row>
  </v-card>
</template>

<script lang="ts">
import GroupMultiSelect from '@/components/GroupMultiSelect.vue'

export default {
  components: { GroupMultiSelect },
  props: {
    data: { type: Object, required: true },
    userGroups: { type: Array, default: () => [] },
    clients: { type: Array, default: () => [] },
    tlsConfigs: { type: Array, default: () => [] },
  },
  computed: {
    tlsSelectItems() {
      const rows = (this.$props.tlsConfigs ?? []) as any[]
      return [{ title: this.$t('none'), value: 0 }, ...rows.map((t) => ({ title: t.name, value: t.id }))]
    },
    modeItems() {
      return [
        { title: 'server', value: 'server' },
        { title: 'client', value: 'client' },
      ]
    },
    isServer() {
      return String(this.data.mode ?? 'server').toLowerCase() === 'server'
    },
    clientItems() {
      const cl = this.$props.clients ?? []
      return (cl as any[]).map((c) => ({ title: c.name, value: c.id }))
    },
    serverAuthJson: {
      get(): string {
        const sa = this.data.server_auth
        if (!sa || typeof sa !== 'object') return '{\n  "policy": "first_match"\n}'
        try {
          return JSON.stringify(sa, null, 2)
        } catch {
          return '{}'
        }
      },
      set(v: string) {
        try {
          this.data.server_auth = JSON.parse(v && v.trim().length > 0 ? v : '{}')
        } catch {
          /* keep previous */
        }
      },
    },
  },
}
</script>
