<template>
  <v-card :subtitle="$t('masque.warpMasqueTitle')">
    <v-card-text class="text-body-2 text-medium-emphasis pb-0">
      {{ $t('masque.warpMasquePanelModel') }}
    </v-card-text>
    <v-row class="mt-2">
      <v-col cols="12" sm="6">
        <v-select
          v-model="transportMode"
          :items="transportModeItems"
          :label="$t('masque.transportMode')"
          hide-details
        />
      </v-col>
      <v-col cols="12" sm="6">
        <v-select
          v-model="httpLayer"
          :items="httpLayerItems"
          :label="$t('masque.httpLayer')"
          hide-details
        />
      </v-col>
    </v-row>
  </v-card>
</template>

<script lang="ts">
const emptySelect = { title: '\u00a0', value: '' }

export default {
  props: {
    data: { type: Object, required: true },
  },
  computed: {
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
    transportMode: {
      get(): string {
        return String((this.data as any).transport_mode ?? '')
      },
      set(v: string) {
        const d = this.data as any
        if (!v || !String(v).trim()) {
          delete d.transport_mode
        } else {
          d.transport_mode = v
        }
      },
    },
    httpLayer: {
      get(): string {
        return String((this.data as any).http_layer ?? '')
      },
      set(v: string) {
        const d = this.data as any
        if (!v || !String(v).trim()) {
          delete d.http_layer
        } else {
          d.http_layer = v
        }
      },
    },
  },
}
</script>
