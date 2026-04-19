<template>

  <v-row>

    <v-col cols="12" sm="6">

      <v-text-field

        v-model="data.private_subnet"

        :label="$t('l3router.privateSubnet')"

        :rules="privateSubnetRules"

      />

    </v-col>

    <v-col cols="12" sm="6">

      <v-text-field

        v-model="data.overlay_destination"

        :label="$t('l3router.overlayDestination')"

      />

    </v-col>

    <v-col cols="12" sm="6">

      <GroupMultiSelect

        v-model="data.member_group_ids"

        :user-groups="userGroups"

        :label="$t('l3router.groups')"

      />

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

    <v-col cols="12">

      <v-data-table
        :headers="peerHeaders"
        :items="data.peers"
        :sort-by="defaultPeerSort"
        :multi-sort="false"
        density="comfortable"
        class="elevation-2 rounded"
      >

        <template #item.row_id="{ index }">{{ index + 1 }}</template>

        <template #item.client_name="{ item }">

          {{ peerRow(item).client_name || ('#' + (peerRow(item).client_id ?? '')) }}

        </template>

        <template #item.group_id="{ item }">

          <span>{{ peerGroupLabel(peerRow(item)) }}</span>

        </template>

        <template #item.peer_id="{ item }">

          <span class="text-body-2">{{ peerRow(item).peer_id ?? '' }}</span>

        </template>

        <template #item.allowed_ips="{ item }">

          <v-text-field

            :model-value="firstAllowedIP(peerRow(item))"

            @update:model-value="setAllowedIP(peerRow(item), $event)"

            @blur="onAllowedIpBlur(peerRow(item))"

            density="compact"

            hide-details

          />

        </template>

        <template #item.filter_source_ips="{ item }" v-if="data.packet_filter">

          <v-text-field

            :model-value="joinArray(peerRow(item).filter_source_ips)"

            @update:model-value="setFilter(peerRow(item), 'filter_source_ips', $event)"

            @blur="onFilterBlur(peerRow(item), 'source')"

            density="compact"

            hide-details

          />

        </template>

        <template #item.filter_destination_ips="{ item }" v-if="data.packet_filter">

          <v-text-field

            :model-value="joinArray(peerRow(item).filter_destination_ips)"

            @update:model-value="setFilter(peerRow(item), 'filter_destination_ips', $event)"

            @blur="onFilterBlur(peerRow(item), 'dest')"

            density="compact"

            hide-details

          />

        </template>

      </v-data-table>

      <div class="text-caption text-error mt-2" v-if="validateError">{{ validateError }}</div>

    </v-col>

  </v-row>

</template>



<script lang="ts">

import { isValidPrivateSubnetField, privateSubnetRuleMessage, isValidIPv4CIDR } from '@/utils/l3Subnet'

import GroupMultiSelect from '@/components/GroupMultiSelect.vue'

import Data from '@/store/modules/data'

import { push } from 'notivue'

import { i18n } from '@/locales'



export default {

  components: { GroupMultiSelect },

  props: {

    data: { type: Object, required: true },

    userGroups: { type: Array, default: () => [] },

    clients: { type: Array, default: () => [] },

    isNew: { type: Boolean, default: false },

  },

  data() {

    return {

      validateError: '',

      privateSubnetRules: [

        (v: string) => isValidPrivateSubnetField(v) || privateSubnetRuleMessage(),

      ],

    }

  },

  computed: {

    clientItems() {

      const cl = this.$props.clients ?? []

      return cl.map((c: any) => ({ title: c.name, value: c.id }))

    },

    peerHeaders() {

      const base: any[] = [

        { title: 'ID', key: 'row_id' },

        { title: this.$t('l3router.peerClient'), key: 'client_name' },

        { title: this.$t('l3router.peerGroup'), key: 'group_id' },

        { title: this.$t('l3router.peerId'), key: 'peer_id' },

        { title: this.$t('l3router.peerIP'), key: 'allowed_ips' },

      ]

      if ((this.data as any).packet_filter) {

        base.push({ title: this.$t('l3router.filterSource'), key: 'filter_source_ips' })

        base.push({ title: this.$t('l3router.filterDestination'), key: 'filter_destination_ips' })

      }

      return base

    },

    /** Default ordering: backend peer_order (monotonic add order). Not a visible column. */
    defaultPeerSort(): { key: string; order: 'asc' | 'desc' }[] {
      return [{ key: 'peer_order', order: 'asc' }]
    },

  },

  methods: {

    /** Vuetify v-data-table may pass InternalItem with .raw or the row object. */

    peerRow(item: unknown): any {

      const o = item as any

      return o?.raw != null ? o.raw : o

    },

    firstAllowedIP(item: any): string {

      const arr = Array.isArray(item.allowed_ips) ? item.allowed_ips : []

      return arr[0] ?? ''

    },

    setAllowedIP(item: any, value: string) {

      item.allowed_ips = String(value ?? '').trim() ? [String(value).trim()] : []

      this.validatePeers()

    },

    joinArray(value: any): string {

      return Array.isArray(value) ? value.join(',') : ''

    },

    setFilter(item: any, key: string, value: string) {

      item[key] = String(value ?? '').trim()

        ? String(value).split(',').map((s) => s.trim()).filter((s) => s.length > 0)

        : []

    },

    peerGroupLabel(item: any): string {

      const gid = Number(item?.group_id ?? 0)

      if (!gid) return this.$t('l3router.individual') as string

      const g = (this.$props.userGroups ?? []).find((x: any) => Number(x.id) === gid) as any

      return (g?.name as string) ?? `#${gid}`

    },

    validatePeers() {

      this.validateError = ''

      const seen = new Set<string>()

      for (const peer of ((this.data as any).peers ?? [])) {

        const ip = this.firstAllowedIP(peer)

        if (!ip) {

          continue

        }

        if (seen.has(ip)) {

          this.validateError = this.$t('l3router.duplicateIP') as string

          break

        }

        seen.add(ip)

      }

    },

    async onAllowedIpBlur(item: any) {

      const ep = this.data as any

      if (this.isNew || !ep.id || !item.client_id) return

      const ip = this.firstAllowedIP(item)

      if (!ip) return

      if (!isValidIPv4CIDR(ip)) {

        push.error({ message: i18n.global.t('l3router.invalidPeerIP') as string })

        return

      }

      await Data().save('l3router_peer', 'update', {

        endpoint_id: ep.id,

        client_id: item.client_id,

        allowed_ips: [ip],

      })

    },

    async onFilterBlur(item: any, which: 'source' | 'dest') {

      const ep = this.data as any

      if (this.isNew || !ep.id || !item.client_id) return

      const payload: Record<string, unknown> = {

        endpoint_id: ep.id,

        client_id: item.client_id,

      }

      if (which === 'source') {

        payload.filter_source_ips = Array.isArray(item.filter_source_ips) ? item.filter_source_ips : []

      } else {

        payload.filter_destination_ips = Array.isArray(item.filter_destination_ips) ? item.filter_destination_ips : []

      }

      await Data().save('l3router_peer', 'update', payload)

    },

  },

  watch: {

    'data.peers': {

      immediate: true,

      deep: true,

      handler(newValue: unknown) {

        const d = this.data as any

        if (!Array.isArray(d.member_group_ids)) d.member_group_ids = []

        if (!Array.isArray(d.member_client_ids)) d.member_client_ids = []

        d.peers = Array.isArray(newValue) ? newValue : []

        this.validatePeers()

      },

    },

  },

}

</script>


