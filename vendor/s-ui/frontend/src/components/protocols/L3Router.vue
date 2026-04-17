<template>
  <v-row>
    <v-col cols="12" sm="6">
      <v-text-field
        v-model="data.overlay_destination"
        label="Overlay destination"
        hint="UDP destination for overlay packets (example: 198.18.0.1:33333)"
        persistent-hint
      />
    </v-col>
    <v-col cols="12" sm="6" class="d-flex align-center">
      <v-switch
        v-model="data.packet_filter"
        color="primary"
        label="Enable packet filter (optional)"
        hint="Disabled by default. ACL filter fields are not required for autosync mode."
        persistent-hint
        hide-details
      />
    </v-col>
    <v-col cols="12">
      <v-textarea
        v-model="peersText"
        label="Peers JSON (autosynced from clients)"
        rows="8"
        auto-grow
        readonly
        :hint="peersHint"
        persistent-hint
      />
      <div v-if="parseError" class="text-error text-caption mt-1">
        {{ parseError }}
      </div>
    </v-col>
  </v-row>
</template>

<script lang="ts">
export default {
  props: ['data'],
  data() {
    return {
      peersText: '[]',
      parseError: '',
      peersHint: 'Peers are managed automatically from clients and displayed as read-only runtime state.',
    }
  },
  watch: {
    'data.peers': {
      immediate: true,
      deep: true,
      handler(newValue: unknown) {
        this.peersText = JSON.stringify(newValue ?? [], null, 2)
      },
    },
  },
}
</script>
