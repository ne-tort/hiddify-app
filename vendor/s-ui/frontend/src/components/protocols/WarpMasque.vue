<template>
  <v-card :subtitle="$t('masque.warpMasqueTitle')">
    <v-alert type="info" variant="tonal" density="compact" class="mb-2">
      {{ $t('masque.warpMasqueAutoRegister') }}
    </v-alert>
    <v-row>
      <v-col cols="12" sm="6">
        <v-text-field v-model="data.server" label="Server" hide-details />
      </v-col>
      <v-col cols="12" sm="6">
        <v-text-field v-model.number="data.server_port" type="number" min="1" max="65535" :label="$t('in.port')" hide-details />
      </v-col>
    </v-row>
    <v-row>
      <v-col cols="12" sm="6">
        <v-text-field v-model="data.transport_mode" :label="$t('masque.transportMode')" hide-details />
      </v-col>
      <v-col cols="12" sm="6">
        <v-text-field v-model="data.tls_server_name" :label="$t('masque.tlsServerName')" hide-details />
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
      <v-col cols="12">
        <v-text-field v-model="profileCompatibility" :label="$t('masque.profileCompatibility')" hide-details />
      </v-col>
    </v-row>
    <v-row>
      <v-col cols="12">
        <v-textarea v-model="extSecretsJson" :label="$t('masque.extHint')" rows="5" variant="outlined" hide-details spellcheck="false" />
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
  },
  computed: {
    clientItems() {
      const cl = this.$props.clients ?? []
      return (cl as any[]).map((c) => ({ title: c.name, value: c.id }))
    },
    profileCompatibility: {
      get(): string {
        const p = this.data.profile
        if (!p || typeof p !== 'object') return 'consumer'
        return String((p as any).compatibility ?? 'consumer')
      },
      set(v: string) {
        if (!this.data.profile || typeof this.data.profile !== 'object') {
          this.data.profile = {}
        }
        ;(this.data.profile as any).compatibility = v
      },
    },
    extSecretsJson: {
      get(): string {
        const ex = this.data.ext
        if (!ex || typeof ex !== 'object') return '{\n}'
        try {
          return JSON.stringify(ex, null, 2)
        } catch {
          return '{}'
        }
      },
      set(v: string) {
        try {
          this.data.ext = JSON.parse(v && v.trim().length > 0 ? v : '{}')
        } catch {
          /* keep */
        }
      },
    },
  },
}
</script>
