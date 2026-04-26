<template>
  <v-card :loading="loading">
    <v-card-title class="d-flex align-center">
      <span>{{ $t('pages.debug') }}</span>
      <v-spacer />
      <v-btn color="primary" variant="tonal" :loading="loading" @click="refreshActiveTab">
        {{ $t('actions.update') }}
      </v-btn>
      <v-btn v-if="activeTab === 'config'" color="primary" variant="outlined" class="ml-2" @click="downloadConfig">
        {{ $t('main.backup.sbConfig') }}
      </v-btn>
    </v-card-title>
    <v-divider />
    <v-card-text>
      <v-alert v-if="errorMessage" type="error" variant="tonal" class="mb-4">
        {{ errorMessage }}
      </v-alert>
      <v-tabs v-model="activeTab" color="primary" class="mb-4">
        <v-tab value="config">Конфиг сервера</v-tab>
        <v-tab value="console">Консоль сервера</v-tab>
      </v-tabs>

      <v-window v-model="activeTab">
        <v-window-item value="config">
          <v-textarea
            v-model="configText"
            auto-grow
            rows="24"
            readonly
            spellcheck="false"
            class="debug-config-textarea"
          />
        </v-window-item>

        <v-window-item value="console">
          <v-row class="mb-2">
            <v-col cols="12" sm="6" md="4">
              <v-select
                hide-details
                :items="runtimeLogLevels"
                v-model="runtimeLogLevel"
                label="Sing-box log level"
                @update:model-value="applyRuntimeLogLevel"
              />
            </v-col>
            <v-col cols="12" sm="6" md="4">
              <v-select
                hide-details
                :items="logLevels"
                v-model="logLevel"
                label="Log level"
                @update:model-value="loadLogs"
              />
            </v-col>
            <v-col cols="12" sm="6" md="4">
              <v-select
                hide-details
                :items="[10, 20, 30, 50, 100, 200]"
                v-model.number="logCount"
                label="Lines"
                @update:model-value="loadLogs"
              />
            </v-col>
            <v-col cols="12" sm="6" md="4">
              <v-switch
                v-model="logsAutoRefresh"
                color="primary"
                hide-details
                label="Auto refresh"
                @update:model-value="updateLogsPolling"
              />
            </v-col>
          </v-row>
          <v-card style="background-color: background" dir="ltr" class="pa-2">
            <div v-for="(line, idx) in logLines" :key="idx">{{ line }}</div>
          </v-card>
        </v-window-item>
      </v-window>
    </v-card-text>
  </v-card>
</template>

<script lang="ts" setup>
import api from '@/plugins/api'
import { i18n } from '@/locales'
import { push } from 'notivue'
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import Data from '@/store/modules/data'

const loading = ref(false)
const activeTab = ref<'config' | 'console'>('config')
const configText = ref('')
const logLines = ref<string[]>([])
const logLevel = ref('info')
const runtimeLogLevel = ref('info')
const logCount = ref(50)
const logsAutoRefresh = ref(true)
const logLevels = [
  { title: 'TRACE', value: 'trace' },
  { title: 'DEBUG', value: 'debug' },
  { title: 'INFO', value: 'info' },
  { title: 'WARNING', value: 'warning' },
  { title: 'ERROR', value: 'err' },
]
const runtimeLogLevels = ['trace', 'debug', 'info', 'warn', 'error', 'fatal', 'panic']
const errorMessage = ref('')
let logsTimer: number | null = null

const normalizeConfigText = (raw: string): string => {
  try {
    return JSON.stringify(JSON.parse(raw), null, 2)
  } catch {
    return raw
  }
}

const startLogsPolling = () => {
  if (logsTimer != null) return
  logsTimer = window.setInterval(() => {
    if (activeTab.value === 'console' && logsAutoRefresh.value) {
      void loadLogs()
    }
  }, 3000)
}

const stopLogsPolling = () => {
  if (logsTimer == null) return
  window.clearInterval(logsTimer)
  logsTimer = null
}

const updateLogsPolling = () => {
  if (activeTab.value === 'console' && logsAutoRefresh.value) {
    startLogsPolling()
  } else {
    stopLogsPolling()
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
    try {
      const cfg = JSON.parse(raw)
      const lvl = String(cfg?.log?.level ?? '').trim().toLowerCase()
      runtimeLogLevel.value = runtimeLogLevels.includes(lvl) ? lvl : 'info'
    } catch {
      runtimeLogLevel.value = 'info'
    }
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

const applyRuntimeLogLevel = async () => {
  const data = Data()
  const cfg = JSON.parse(JSON.stringify(data.config ?? {}))
  if (!cfg.log || typeof cfg.log !== 'object') {
    cfg.log = {}
  }
  cfg.log.level = runtimeLogLevel.value
  const ok = await data.save('config', 'set', cfg)
  if (!ok) {
    return
  }
  await loadConfig()
  if (activeTab.value === 'console') {
    await loadLogs()
  }
}

const loadLogs = async () => {
  loading.value = true
  errorMessage.value = ''
  try {
    const resp = await api.get('api/logs', {
      params: {
        c: logCount.value,
        l: logLevel.value,
      },
    })
    if (Array.isArray(resp.data?.obj)) {
      logLines.value = resp.data.obj
    } else if (Array.isArray(resp.data)) {
      logLines.value = resp.data
    } else {
      logLines.value = []
    }
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

const refreshActiveTab = async () => {
  if (activeTab.value === 'console') {
    await loadLogs()
    return
  }
  await loadConfig()
}

const downloadConfig = () => {
  window.location.href = 'api/singbox-config'
}

onMounted(() => {
  const lvl = String(Data().config?.log?.level ?? '').trim().toLowerCase()
  runtimeLogLevel.value = runtimeLogLevels.includes(lvl) ? lvl : 'info'
  void loadConfig()
})

watch(activeTab, async (tab) => {
  if (tab === 'console') {
    await loadLogs()
  } else if (!configText.value) {
    await loadConfig()
  }
  updateLogsPolling()
})

onBeforeUnmount(() => {
  stopLogsPolling()
})
</script>
