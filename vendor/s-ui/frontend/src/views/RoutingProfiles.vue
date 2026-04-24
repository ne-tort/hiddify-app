<template>
  <RoutingProfileModal v-model="modal.visible" :visible="modal.visible" :id="modal.id" @close="closeModal" />
  <GeoCatalogEditor :visible="geoModal.geosite" kind="geosite" @close="geoModal.geosite = false" />
  <GeoCatalogEditor :visible="geoModal.geoip" kind="geoip" @close="geoModal.geoip = false" />

  <v-row justify="center" align="center">
    <v-col cols="auto">
      <v-btn color="primary" @click="showModal(0)">{{ $t('actions.add') }}</v-btn>
    </v-col>
    <v-col cols="auto"><v-btn color="secondary" @click="syncGeoCatalog" :loading="syncLoading">Sync Geo DB</v-btn></v-col>
    <v-col cols="auto"><v-btn variant="text" @click="rebuildGeoCatalog">Rebuild</v-btn></v-col>
  </v-row>

  <v-row>
    <v-col cols="12">
      <v-data-table
        :headers="headers"
        :items="profiles"
        item-value="id"
        class="elevation-3 rounded"
        hide-no-data
      >
        <template v-slot:item.enabled="{ item }">
          <v-chip size="small" :color="item.enabled ? 'success' : 'error'">{{ item.enabled ? $t('enable') : $t('disable') }}</v-chip>
        </template>
        <template v-slot:item.actions="{ item }">
          <v-icon class="me-2" @click="showModal(item.id)">mdi-pencil</v-icon>
          <v-icon class="me-2" @click="previewProfile(item.id)">mdi-eye</v-icon>
          <v-icon color="error" @click="deleteProfile(item.id)">mdi-delete</v-icon>
        </template>
      </v-data-table>
    </v-col>
  </v-row>

  <v-row>
    <v-col cols="12" sm="6" md="4" lg="3">
      <v-card rounded="xl" elevation="5" min-width="220" title="Geosite DB">
        <v-card-text>
          <v-row><v-col>Type</v-col><v-col>Domain tags</v-col></v-row>
          <v-row><v-col>Tags</v-col><v-col>{{ geositeStats.tags }}</v-col></v-row>
          <v-row><v-col>Items</v-col><v-col>{{ geositeStats.items }}</v-col></v-row>
        </v-card-text>
        <v-divider></v-divider>
        <v-card-actions style="padding: 0;">
          <v-btn icon="mdi-file-edit" @click="geoModal.geosite = true">
            <v-icon /><v-tooltip activator="parent" location="top" :text="$t('actions.edit')"></v-tooltip>
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-col>

    <v-col cols="12" sm="6" md="4" lg="3">
      <v-card rounded="xl" elevation="5" min-width="220" title="GeoIP DB">
        <v-card-text>
          <v-row><v-col>Type</v-col><v-col>CIDR tags</v-col></v-row>
          <v-row><v-col>Tags</v-col><v-col>{{ geoipStats.tags }}</v-col></v-row>
          <v-row><v-col>Items</v-col><v-col>{{ geoipStats.items }}</v-col></v-row>
        </v-card-text>
        <v-divider></v-divider>
        <v-card-actions style="padding: 0;">
          <v-btn icon="mdi-file-edit" @click="geoModal.geoip = true">
            <v-icon /><v-tooltip activator="parent" location="top" :text="$t('actions.edit')"></v-tooltip>
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-col>
  </v-row>

  <v-row v-if="preview">
    <v-col cols="12">
      <v-card class="rounded elevation-2 mt-3">
        <v-card-title>Preview</v-card-title>
        <v-divider />
        <v-card-text>
          <v-textarea :model-value="preview" rows="12" readonly auto-grow />
        </v-card-text>
      </v-card>
    </v-col>
  </v-row>
</template>

<script lang="ts" setup>
import { computed, ref } from 'vue'
import { i18n } from '@/locales'
import Data from '@/store/modules/data'
import HttpUtils from '@/plugins/httputil'
import RoutingProfileModal from '@/layouts/modals/RoutingProfile.vue'
import GeoCatalogEditor from '@/layouts/modals/GeoCatalogEditor.vue'

const syncLoading = ref(false)
const preview = ref('')

const modal = ref({
  visible: false,
  id: 0,
})
const geoModal = ref({
  geosite: false,
  geoip: false,
})

const headers = [
  { title: i18n.global.t('client.name'), key: 'name' },
  { title: i18n.global.t('client.desc'), key: 'desc' },
  { title: i18n.global.t('enable'), key: 'enabled' },
  { title: i18n.global.t('actions.action'), key: 'actions', sortable: false },
]

const profiles = computed(() => Data().routingProfiles ?? [])
const geoTags = computed(() => (Data().geoCatalog?.tags ?? []) as any[])
const geositeStats = computed(() => {
  const tags = geoTags.value.filter((t:any) => t.dataset_kind === 'geosite')
  const items = tags.reduce((sum:number, t:any) => sum + (t.item_count ?? 0), 0)
  return { tags: tags.length, items }
})
const geoipStats = computed(() => {
  const tags = geoTags.value.filter((t:any) => t.dataset_kind === 'geoip')
  const items = tags.reduce((sum:number, t:any) => sum + (t.item_count ?? 0), 0)
  return { tags: tags.length, items }
})

const showModal = (id: number) => {
  modal.value.id = id
  modal.value.visible = true
}
const closeModal = () => {
  modal.value.visible = false
}

const deleteProfile = async (id: number) => {
  await Data().save('routing_profiles', 'del', id)
}

const syncGeoCatalog = async () => {
  syncLoading.value = true
  await Data().save('geo_catalog', 'sync', {})
  syncLoading.value = false
}

const rebuildGeoCatalog = async () => {
  await Data().save('geo_catalog', 'rebuild', {})
}

const previewProfile = async (id: number) => {
  const happ = await HttpUtils.get('api/routingProfileHapp', { id })
  const sb = await HttpUtils.get('api/routingProfileSingbox', { id })
  if (!happ.success || !sb.success) {
    preview.value = 'Preview error'
    return
  }
  preview.value = JSON.stringify({
    happ: happ.obj,
    singbox: sb.obj,
  }, null, 2)
}
</script>
