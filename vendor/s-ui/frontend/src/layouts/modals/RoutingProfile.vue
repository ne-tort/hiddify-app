<template>
  <v-dialog v-model="visibleModel" max-width="1080">
    <v-card>
      <v-card-title>{{ $t(id === 0 ? 'actions.add' : 'actions.edit') }} {{ $t('objects.routing_profile') }}</v-card-title>
      <v-divider />
      <v-card-text>
        <v-tabs v-model="tab" align-tabs="center">
          <v-tab value="routing">Routing</v-tab>
          <v-tab value="preview">Preview</v-tab>
        </v-tabs>
        <v-window v-model="tab">
          <v-window-item value="routing">
            <v-row class="mt-2">
              <v-col cols="12" md="6">
                <v-text-field v-model="form.name" :label="$t('client.name')" variant="underlined" density="compact" />
              </v-col>
              <v-col cols="12" md="6">
                <v-switch v-model="form.enabled" color="primary" :label="$t('enable')" hide-details />
              </v-col>
              <v-col cols="12">
                <v-text-field v-model="form.desc" :label="$t('client.desc')" variant="underlined" density="compact" />
              </v-col>
              <v-col cols="12">
                <v-row>
                  <v-col cols="12" md="6">
                    <v-select
                      v-model="form.client_ids"
                      :items="clientItems"
                      :label="$t('pages.clients')"
                      multiple
                      chips
                      closable-chips
                      variant="underlined"
                      density="compact"
                    />
                  </v-col>
                  <v-col cols="12" md="6">
                    <v-select
                      v-model="form.group_ids"
                      :items="groupItems"
                      :label="$t('pages.groups')"
                      multiple
                      chips
                      closable-chips
                      variant="underlined"
                      density="compact"
                    />
                  </v-col>
                </v-row>
              </v-col>
              <v-col cols="12" md="4">
                <v-card class="rounded-lg elevation-1">
                  <v-card-title class="text-subtitle-1 py-2">Proxy</v-card-title>
                  <v-card-text class="pt-0">
                    <v-autocomplete
                      v-model="proxyTokens"
                      :items="allGeoItems"
                      item-title="title"
                      item-value="value"
                      label="Tags"
                      multiple
                      chips
                      closable-chips
                      variant="underlined"
                      density="compact"
                      clearable
                    />
                  </v-card-text>
                </v-card>
              </v-col>
              <v-col cols="12" md="4">
                <v-card class="rounded-lg elevation-1">
                  <v-card-title class="text-subtitle-1 py-2">Direct</v-card-title>
                  <v-card-text class="pt-0">
                    <v-autocomplete
                      v-model="directTokens"
                      :items="allGeoItems"
                      item-title="title"
                      item-value="value"
                      label="Tags"
                      multiple
                      chips
                      closable-chips
                      variant="underlined"
                      density="compact"
                      clearable
                    />
                  </v-card-text>
                </v-card>
              </v-col>
              <v-col cols="12" md="4">
                <v-card class="rounded-lg elevation-1">
                  <v-card-title class="text-subtitle-1 py-2">Block</v-card-title>
                  <v-card-text class="pt-0">
                    <v-autocomplete
                      v-model="blockTokens"
                      :items="allGeoItems"
                      item-title="title"
                      item-value="value"
                      label="Tags"
                      multiple
                      chips
                      closable-chips
                      variant="underlined"
                      density="compact"
                      clearable
                    />
                  </v-card-text>
                </v-card>
              </v-col>
            </v-row>
          </v-window-item>
          <v-window-item value="preview">
            <v-row class="mt-2">
              <v-col cols="12">
                <v-textarea :model-value="mergedPreview" rows="14" auto-grow readonly />
              </v-col>
            </v-row>
          </v-window-item>
        </v-window>
      </v-card-text>
      <v-divider />
      <v-card-actions>
        <v-spacer />
        <v-btn color="error" variant="outlined" @click="closeModal">{{ $t('actions.close') }}</v-btn>
        <v-btn color="primary" variant="flat" :loading="loading" @click="saveChanges">{{ $t('actions.save') }}</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<script lang="ts" setup>
import { computed, reactive, ref, watch } from 'vue'
import Data from '@/store/modules/data'

const props = defineProps<{
  visible: boolean
  id: number
}>()
const emit = defineEmits(['close'])

const loading = ref(false)
const tab = ref('routing')
const visibleModel = computed({
  get: () => props.visible,
  set: () => emit('close'),
})

const geositeItems = computed(() => {
  const tags = (Data().geoCatalog?.tags ?? []) as any[]
  return tags
    .filter((t:any) => t.dataset_kind === 'geosite')
    .map((t:any) => ({ title: `geosite:${t.tag_norm}`, value: `geosite:${t.tag_norm}` }))
})
const geoipItems = computed(() => {
  const tags = (Data().geoCatalog?.tags ?? []) as any[]
  return tags
    .filter((t:any) => t.dataset_kind === 'geoip')
    .map((t:any) => ({ title: `geoip:${t.tag_norm}`, value: `geoip:${t.tag_norm}` }))
})
const allGeoItems = computed(() => [...geositeItems.value, ...geoipItems.value])

const emptyForm = () => ({
  id: 0,
  name: '',
  desc: '',
  enabled: true,
  route_order: [] as string[],
  direct_sites: [] as string[],
  direct_ip: [] as string[],
  proxy_sites: [] as string[],
  proxy_ip: [] as string[],
  block_sites: [] as string[],
  block_ip: [] as string[],
  dns_policy: {},
  compatibility: {},
  geo_catalog_version: '',
  client_ids: [] as number[],
  group_ids: [] as number[],
})

const form = reactive(emptyForm())

const splitTokens = (tokens: string[]) => ({
  sites: tokens.filter((x) => x.startsWith('geosite:')),
  ip: tokens.filter((x) => x.startsWith('geoip:')),
})

const directTokens = computed({
  get: () => [...form.direct_sites, ...form.direct_ip],
  set: (tokens: string[]) => {
    const split = splitTokens(tokens)
    form.direct_sites = split.sites
    form.direct_ip = split.ip
  },
})
const proxyTokens = computed({
  get: () => [...form.proxy_sites, ...form.proxy_ip],
  set: (tokens: string[]) => {
    const split = splitTokens(tokens)
    form.proxy_sites = split.sites
    form.proxy_ip = split.ip
  },
})
const blockTokens = computed({
  get: () => [...form.block_sites, ...form.block_ip],
  set: (tokens: string[]) => {
    const split = splitTokens(tokens)
    form.block_sites = split.sites
    form.block_ip = split.ip
  },
})

const applyData = (id: number) => {
  Object.assign(form, emptyForm())
  if (id === 0) return
  const row = (Data().routingProfiles ?? []).find((x:any) => x.id === id)
  if (!row) return
  Object.assign(form, row)
}

watch(() => props.visible, (v) => {
  if (v) {
    tab.value = 'routing'
    applyData(props.id)
  }
})

const closeModal = () => emit('close')

const saveChanges = async () => {
  loading.value = true
  form.route_order = []
  if (form.block_sites.length > 0 || form.block_ip.length > 0) form.route_order.push('block')
  if (form.direct_sites.length > 0 || form.direct_ip.length > 0) form.route_order.push('direct')
  if (form.proxy_sites.length > 0 || form.proxy_ip.length > 0) form.route_order.push('proxy')
  const ok = await Data().save('routing_profiles', props.id === 0 ? 'new' : 'edit', form)
  loading.value = false
  if (ok) closeModal()
}

const clientItems = computed(() =>
  (Data().clients ?? []).map((c:any) => ({ title: c.name, value: c.id }))
)
const groupItems = computed(() =>
  (Data().userGroups ?? []).map((g:any) => ({ title: g.name, value: g.id }))
)
const mergedPreview = computed(() => JSON.stringify({
  route_order: form.route_order.length > 0 ? form.route_order : undefined,
  block: { sites: form.block_sites, ip: form.block_ip },
  direct: { sites: form.direct_sites, ip: form.direct_ip },
  proxy: { sites: form.proxy_sites, ip: form.proxy_ip },
  bindings: { clients: form.client_ids, groups: form.group_ids },
}, null, 2))
</script>
