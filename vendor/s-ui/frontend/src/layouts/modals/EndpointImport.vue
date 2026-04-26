<template>
  <v-dialog
    :model-value="visible"
    transition="dialog-top-transition"
    width="820"
    @update:model-value="(v) => { if (!v) $emit('close') }"
  >
    <v-card class="rounded-lg">
      <v-card-title>{{ $t('endpointImport.title') }}</v-card-title>
      <v-divider />
      <v-card-text>
        <v-alert type="info" variant="text" class="mb-2">{{ $t('endpointImport.hint') }}</v-alert>
        <v-textarea
          v-model="rawJson"
          label="JSON"
          variant="outlined"
          rows="12"
          hide-details
          spellcheck="false"
          class="mb-4"
        />
        <v-file-input
          :label="$t('endpointImport.file')"
          accept=".json,application/json"
          variant="outlined"
          hide-details
          prepend-icon="mdi-file-code"
          clearable
          @update:modelValue="onFileUpload($event)"
        />
        <v-alert v-if="error" type="error" variant="tonal" class="mt-3">{{ error }}</v-alert>
        <v-alert v-if="warnings.length > 0" type="warning" variant="tonal" class="mt-3">
          <div v-for="(w, i) in warnings" :key="i">{{ w }}</div>
        </v-alert>
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn variant="text" @click="$emit('close')">{{ $t('actions.close') }}</v-btn>
        <v-btn color="primary" variant="tonal" :disabled="rawJson.trim().length === 0" @click="applyImport">
          {{ $t('actions.apply') }}
        </v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<script lang="ts">
import { applyImportedEndpointToDraft, parseEndpointImport } from '@/utils/endpointImport'

export default {
  props: {
    visible: { type: Boolean, required: true },
    endpoint: { type: Object, required: true },
  },
  emits: ['close', 'applied'],
  data() {
    return {
      rawJson: '',
      error: '',
      warnings: [] as string[],
    }
  },
  methods: {
    reset() {
      this.rawJson = ''
      this.error = ''
      this.warnings = []
    },
    async onFileUpload(files: File | File[] | null) {
      const file = Array.isArray(files) ? files[0] : files
      if (!file) return
      this.rawJson = await file.text()
    },
    applyImport() {
      this.error = ''
      this.warnings = []
      try {
        const parsed = parseEndpointImport(this.rawJson)
        const result = applyImportedEndpointToDraft(this.endpoint, parsed)
        this.warnings = result.warnings
        this.$emit('applied', result.warnings)
      } catch (e: any) {
        this.error = e?.message ?? String(e)
      }
    },
  },
  watch: {
    visible(v: boolean) {
      if (v) this.reset()
    },
  },
}
</script>
