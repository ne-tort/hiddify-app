
<template>
  <v-dialog v-model="modal.visible" max-width="640">
    <v-card>
      <v-card-title>{{ $t('actions.' + modal.mode) }} {{ $t('objects.group') }}</v-card-title>
      <v-card-text>
        <v-text-field v-model="modal.name" :label="$t('client.name')" density="compact" />
        <v-text-field v-model="modal.desc" :label="$t('client.desc')" density="compact" />
        <template v-if="modal.mode === 'add'">
          <GroupMultiSelect
            v-model="modal.child_group_ids"
            :user-groups="parentChoicesGroups"
            :exclude-id="modal.id"
            :label="$t('group.nestedGroups')"
          />
          <v-select
            v-model="modal.client_ids"
            :items="clientItems"
            item-title="title"
            item-value="value"
            multiple
            chips
            closable-chips
            :label="$t('group.members')"
            density="compact"
            class="mt-2"
          />
        </template>
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn variant="text" @click="modal.visible = false">{{ $t('actions.close') }}</v-btn>
        <v-btn color="primary" @click="saveModal">{{ $t('actions.save') }}</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>

  <v-dialog v-model="membersModal.visible" max-width="640">
    <v-card>
      <v-card-title>{{ $t('group.members') }}</v-card-title>
      <v-card-text>
        <GroupMultiSelect
          v-model="membersModal.child_group_ids"
          :user-groups="parentChoicesMembersModal"
          :exclude-id="membersModal.group_id"
          :label="$t('group.nestedGroups')"
        />
        <v-select
          v-model="membersModal.client_ids"
          :items="clientItems"
          item-title="title"
          item-value="value"
          multiple
          chips
          closable-chips
          :label="$t('pages.clients')"
          density="compact"
          class="mt-2"
        />
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn variant="text" @click="membersModal.visible = false">{{ $t('actions.close') }}</v-btn>
        <v-btn color="primary" @click="saveMembers">{{ $t('actions.save') }}</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>

  <v-row justify="center" align="center">
    <v-col cols="auto">
      <v-btn color="primary" @click="openNew">{{ $t('actions.add') }}</v-btn>
    </v-col>
  </v-row>
  <v-row>
    <v-col cols="12">
      <v-data-table
        :headers="headers"
        :items="groups"
        hide-no-data
        class="elevation-3 rounded"
      >
        <template #item.groups="{ item }">
          {{ childNames(item) }}
        </template>
        <template #item.desc="{ item }">
          {{ presentDesc(item.desc) }}
        </template>
        <template #item.actions="{ item }">
          <v-btn icon size="small" @click="openEdit(item)"><v-icon>mdi-pencil</v-icon></v-btn>
          <v-btn icon size="small" @click="openMembers(item)"><v-icon>mdi-account-multiple</v-icon></v-btn>
          <v-btn icon size="small" color="error" @click="delGroup(item)"><v-icon>mdi-delete</v-icon></v-btn>
        </template>
      </v-data-table>
    </v-col>
  </v-row>
</template>

<script lang="ts" setup>
import Data from '@/store/modules/data'
import GroupMultiSelect from '@/components/GroupMultiSelect.vue'
import { computed, ref, onMounted } from 'vue'
import { i18n } from '@/locales'

const groups = computed(() => Data().userGroups ?? [])

const groupById = computed(() => {
  const m = new Map<number, any>()
  for (const g of groups.value) {
    m.set(g.id, g)
  }
  return m
})

const headers = [
  { title: 'ID', key: 'id' },
  { title: i18n.global.t('client.name'), key: 'name' },
  { title: i18n.global.t('client.desc'), key: 'desc' },
  { title: i18n.global.t('group.nestedGroups'), key: 'groups' },
  { title: i18n.global.t('group.clientsColumn'), key: 'effective_member_count' },
  { title: '', key: 'actions', sortable: false },
]

const clientItems = computed(() =>
  (Data().clients ?? []).map((c: any) => ({ title: c.name, value: c.id })),
)

const parentChoicesGroups = computed(() => groups.value)

const parentChoicesMembersModal = computed(() =>
  (groups.value as any[]).filter((g: any) => g.id !== membersModal.value.group_id),
)

function parentIds(item: any): number[] {
  return item.member_of_group_ids ?? item.parent_group_ids ?? []
}

function childIds(item: any): number[] {
  return (groups.value as any[])
    .filter((g: any) => parentIds(g).includes(item.id))
    .map((g: any) => g.id)
}

function childNames(item: any): string {
  const ids: number[] = childIds(item)
  if (ids.length === 0) return '—'
  return ids
    .map((id) => groupById.value.get(id)?.name ?? id)
    .join(', ')
}

function presentDesc(desc: string): string {
  const raw = String(desc ?? '')
  const marker = '__l3auto__:'
  if (!raw.startsWith(marker)) {
    return raw
  }
  const tag = raw.slice(marker.length).trim()
  return i18n.global.t('l3router.autoGroupDesc', { name: tag || 'l3router' })
}

const modal = ref({
  visible: false,
  mode: 'add' as 'add' | 'edit',
  id: 0,
  name: '',
  desc: '',
  child_group_ids: [] as number[],
  client_ids: [] as number[],
})

const membersModal = ref({
  visible: false,
  group_id: 0,
  child_group_ids: [] as number[],
  client_ids: [] as number[],
})

onMounted(async () => {
  await Data().loadData()
})

const openNew = () => {
  modal.value = {
    visible: true,
    mode: 'add',
    id: 0,
    name: '',
    desc: '',
    child_group_ids: [],
    client_ids: [],
  }
}

const openEdit = (item: any) => {
  modal.value = {
    visible: true,
    mode: 'edit',
    id: item.id,
    name: item.name,
    desc: item.desc ?? '',
    child_group_ids: [],
    client_ids: [],
  }
}

const saveModal = async () => {
  const m = modal.value
  if (m.mode === 'add') {
    await Data().save('groups', 'new', {
      name: m.name,
      desc: m.desc,
      child_group_ids: m.child_group_ids,
      client_ids: m.client_ids,
    })
  } else {
    await Data().save('groups', 'edit', {
      id: m.id,
      name: m.name,
      desc: m.desc,
    })
  }
  modal.value.visible = false
}

const openMembers = (item: any) => {
  const childGroupIds = (groups.value as any[])
    .filter((g: any) => {
      const pids: number[] = parentIds(g)
      return pids.includes(item.id)
    })
    .map((g: any) => g.id)
  membersModal.value = {
    visible: true,
    group_id: item.id,
    child_group_ids: childGroupIds,
    client_ids: [...(item.member_client_ids ?? [])],
  }
}

const saveMembers = async () => {
  const m = membersModal.value
  await Data().save('groups', 'setMembers', {
    group_id: m.group_id,
    client_ids: m.client_ids,
    child_group_ids: m.child_group_ids,
  })
  membersModal.value.visible = false
}

const delGroup = async (item: any) => {
  if (!confirm(i18n.global.t('confirm'))) return
  await Data().save('groups', 'del', item.id)
}
</script>
