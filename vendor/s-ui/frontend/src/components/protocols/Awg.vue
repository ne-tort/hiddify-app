<template>
  <div>
    <v-tabs v-model="tab" align-tabs="start" density="compact">
      <v-tab value="conn">{{ $t('awg.tabConnection') }}</v-tab>
      <v-tab value="obs">{{ $t('awg.tabObfuscation') }}</v-tab>
    </v-tabs>
    <v-window v-model="tab">
      <v-window-item value="conn">
        <v-card class="mt-2" variant="flat">
          <v-card-text class="px-0">
            <v-row dense>
              <v-col cols="12" md="6">
                <v-select
                  v-model="data.obfuscation_profile_id"
                  :items="profileItems"
                  item-title="title"
                  item-value="value"
                  clearable
                  :label="$t('awg.linkedProfile')"
                  density="compact"
                  hide-details
                />
              </v-col>
            </v-row>
            <v-expansion-panels variant="accordion" class="mt-2">
              <v-expansion-panel :title="$t('awg.inlineOverrides')">
                <v-expansion-panel-text>
                  <v-row dense>
                    <v-col cols="6" sm="4" md="2" v-for="f in intFields" :key="f">
                      <v-text-field v-model.number="data[f]" :label="f" type="number" density="compact" hide-details clearable />
                    </v-col>
                    <v-col cols="12" sm="6" v-for="f in strFields" :key="'s'+f">
                      <v-text-field v-model="data[f]" :label="f" density="compact" hide-details clearable />
                    </v-col>
                  </v-row>
                </v-expansion-panel-text>
              </v-expansion-panel>
            </v-expansion-panels>
            <Wireguard
              class="mt-2"
              :data="data"
              :user-groups="userGroups"
              :clients="clients"
              :hide-wg-only-options="true"
              :card-subtitle="$t('awg.wireguardSection')"
              @getWgPubKey="$emit('getWgPubKey', $event)"
              @newWgKey="$emit('newWgKey')"
            />
          </v-card-text>
        </v-card>
      </v-window-item>
      <v-window-item value="obs">
        <AwgObfuscationProfilesManager />
      </v-window-item>
    </v-window>
  </div>
</template>

<script lang="ts">
import Wireguard from '@/components/protocols/Wireguard.vue'
import AwgObfuscationProfilesManager from '@/components/protocols/AwgObfuscationProfilesManager.vue'
import Data from '@/store/modules/data'

export default {
  components: { Wireguard, AwgObfuscationProfilesManager },
  props: ['data', 'userGroups', 'clients'],
  emits: ['newWgKey', 'getWgPubKey'],
  data() {
    return {
      tab: 'conn',
      intFields: ['jc', 'jmin', 'jmax', 's1', 's2', 's3', 's4'],
      strFields: ['h1', 'h2', 'h3', 'h4', 'i1', 'i2', 'i3', 'i4', 'i5'],
    }
  },
  mounted() {
    void Data().loadAwgObfuscationProfiles()
  },
  computed: {
    profileItems() {
      const rows = Data().awgObfuscationProfiles ?? []
      return rows
        .filter((p: any) => p?.enabled)
        .map((p: any) => ({ title: `${p.name} (#${p.id})`, value: p.id }))
    },
  },
}
</script>
