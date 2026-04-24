<template>
  <v-dialog v-model="visibleModel" max-width="900">
    <v-card>
      <v-card-title>{{ $t('actions.edit') }} {{ kind }} tags</v-card-title>
      <v-divider />
      <v-card-text>
        <v-row>
          <v-col cols="12">
            <div class="d-flex align-center ga-3">
              <v-text-field v-model="newTag" label="New tag" variant="underlined" density="compact" hide-details />
              <v-btn size="small" color="primary" @click="addTag">{{ $t('actions.add') }}</v-btn>
            </div>
          </v-col>
          <v-col cols="12">
            <v-data-table :headers="headers" :items="tags" item-value="id" class="elevation-2 rounded">
              <template v-slot:item.actions="{ item }">
                <v-icon class="me-2" @click="pickTag(item)">mdi-pencil</v-icon>
                <v-icon color="error" @click="deleteTag(item.id)">mdi-delete</v-icon>
              </template>
            </v-data-table>
          </v-col>
        </v-row>
        <v-divider class="my-3" />
        <v-row v-if="selectedTagId > 0">
          <v-col cols="12" md="4">
            <v-select v-model="itemType" :items="itemTypes" label="Item type" variant="underlined" density="compact" />
            <v-text-field v-model="itemValue" label="Value" variant="underlined" density="compact" />
            <v-btn size="small" color="primary" @click="addItem">{{ $t('actions.add') }} item</v-btn>
          </v-col>
          <v-col cols="12" md="8">
            <v-data-table :headers="itemHeaders" :items="tagItems" item-value="id" :loading="itemsLoading" class="elevation-1 rounded">
              <template v-slot:item.actions="{ item }">
                <v-icon color="error" @click="deleteItem(item.id)">mdi-delete</v-icon>
              </template>
            </v-data-table>
          </v-col>
        </v-row>
      </v-card-text>
      <v-divider />
      <v-card-actions>
        <v-spacer />
        <v-btn color="primary" variant="outlined" @click="closeModal">{{ $t('actions.close') }}</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<script lang="ts" setup>
import { computed, ref } from 'vue'
import Data from '@/store/modules/data'
import HttpUtils from '@/plugins/httputil'

const props = defineProps<{ visible: boolean; kind: 'geosite'|'geoip' }>()
const emit = defineEmits(['close'])

const visibleModel = computed({
  get: () => props.visible,
  set: () => emit('close'),
})
const newTag = ref('')
const selectedTagId = ref(0)
const itemType = ref(props.kind === 'geoip' ? 'cidr' : 'domain_suffix')
const itemValue = ref('')
const itemsLoading = ref(false)
const selectedItems = ref<any[]>([])

const headers = [
  { title: 'Tag', key: 'tag_norm' },
  { title: 'Origin', key: 'origin' },
  { title: 'Items', key: 'item_count' },
  { title: 'Actions', key: 'actions', sortable: false },
]
const itemHeaders = [
  { title: 'Type', key: 'item_type' },
  { title: 'Value', key: 'value_raw' },
  { title: 'Actions', key: 'actions', sortable: false },
]
const itemTypes = props.kind === 'geoip'
  ? [{ title: 'cidr', value: 'cidr' }]
  : [
      { title: 'domain_full', value: 'domain_full' },
      { title: 'domain_suffix', value: 'domain_suffix' },
      { title: 'domain_keyword', value: 'domain_keyword' },
      { title: 'domain_regex', value: 'domain_regex' },
    ]

const tags = computed(() =>
  ((Data().geoCatalog?.tags ?? []) as any[]).filter((t:any) => t.dataset_kind === props.kind)
)
const tagItems = computed(() => selectedItems.value)

const closeModal = () => emit('close')
const addTag = async () => {
  if (!newTag.value.trim()) return
  await Data().save('geo_catalog', 'new_tag', { dataset_kind: props.kind, tag: newTag.value })
  newTag.value = ''
}
const deleteTag = async (id:number) => {
  await Data().save('geo_catalog', 'del_tag', id)
  if (selectedTagId.value === id) selectedTagId.value = 0
}
const pickTag = (item:any) => {
  selectedTagId.value = item.id
  void loadSelectedItems()
}
const loadSelectedItems = async () => {
  if (!selectedTagId.value) {
    selectedItems.value = []
    return
  }
  itemsLoading.value = true
  const msg = await HttpUtils.get('api/geo_catalog', { tag_id: selectedTagId.value })
  itemsLoading.value = false
  if (!msg.success) return
  selectedItems.value = msg.obj?.geo_catalog?.items ?? []
}
const addItem = async () => {
  if (!selectedTagId.value || !itemValue.value.trim()) return
  await Data().save('geo_catalog', 'upsert_item', {
    tag_id: selectedTagId.value,
    item_type: itemType.value,
    value: itemValue.value,
  })
  itemValue.value = ''
  await loadSelectedItems()
}
const deleteItem = async (id:number) => {
  await Data().save('geo_catalog', 'del_item', id)
  await loadSelectedItems()
}
</script>
