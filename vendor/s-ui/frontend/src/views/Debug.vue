<template>
  <v-card :loading="loading">
    <v-card-title class="d-flex align-center">
      <span>{{ $t('pages.debug') }}</span>
      <v-spacer />
      <v-btn
        color="primary"
        variant="tonal"
        :loading="loading"
        @click="loadConfig"
      >
        {{ $t('actions.update') }}
      </v-btn>
      <v-btn
        color="primary"
        variant="outlined"
        class="ml-2"
        @click="downloadConfig"
      >
        {{ $t('main.backup.sbConfig') }}
      </v-btn>
    </v-card-title>
    <v-divider />
    <v-card-text>
      <v-alert v-if="errorMessage" type="error" variant="tonal" class="mb-4">
        {{ errorMessage }}
      </v-alert>
      <v-textarea
        v-model="configText"
        auto-grow
        rows="24"
        readonly
        spellcheck="false"
        class="debug-config-textarea"
      />
    </v-card-text>
  </v-card>
</template>

<script lang="ts" setup>
import api from '@/plugins/api'
import { i18n } from '@/locales'
import { push } from 'notivue'
import { onMounted, ref } from 'vue'

const loading = ref(false)
const configText = ref('')
const errorMessage = ref('')

const normalizeConfigText = (raw: string): string => {
  try {
    return JSON.stringify(JSON.parse(raw), null, 2)
  } catch {
    return raw
  }
}

const loadConfig = async () => {
  loading.value = true
  errorMessage.value = ''
  try {
    const resp = await api.get('api/singbox-config', {
      responseType: 'text',
      transformResponse: [(data: string) => data],
    })
    const raw = typeof resp.data === 'string' ? resp.data : JSON.stringify(resp.data, null, 2)
    configText.value = normalizeConfigText(raw)
  } catch (e: any) {
    errorMessage.value = e?.response?.data || e?.message || String(e)
    push.error({
      title: i18n.global.t('failed'),
      message: errorMessage.value,
    })
  } finally {
    loading.value = false
  }
}

const downloadConfig = () => {
  window.location.href = 'api/singbox-config'
}

onMounted(() => {
  loadConfig()
})
</script>
