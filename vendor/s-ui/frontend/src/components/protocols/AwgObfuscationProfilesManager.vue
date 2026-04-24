<template>
  <v-card variant="outlined" class="mt-2">
    <v-card-title class="text-subtitle-1 d-flex align-center flex-wrap gap-2">
      {{ $t('awg.obfuscationProfiles') }}
      <v-spacer />
      <v-btn size="small" color="primary" variant="tonal" @click="openDialog(0)">{{ $t('actions.add') }}</v-btn>
    </v-card-title>
    <v-divider />
    <v-data-table
      :items="profiles"
      :headers="headers"
      density="compact"
      class="elevation-0"
      hide-default-footer
      :items-per-page="-1"
    >
      <template #item.enabled="{ item }">
        <v-chip size="x-small" :color="item.enabled ? 'success' : 'default'">{{ item.enabled ? $t('enable') : $t('disable') }}</v-chip>
      </template>
      <template #item.clients="{ item }">
        {{ (item.client_ids?.length ?? 0) }}
      </template>
      <template #item.groups="{ item }">
        {{ (item.group_ids?.length ?? 0) }}
      </template>
      <template #item.actions="{ item }">
        <v-btn icon size="x-small" variant="text" @click="openDialog(item.id)"><v-icon>mdi-pencil</v-icon></v-btn>
        <v-btn icon size="x-small" variant="text" color="error" @click="delProfile(item.id)"><v-icon>mdi-delete</v-icon></v-btn>
      </template>
    </v-data-table>

    <v-dialog v-model="dialog" max-width="920" scrollable>
      <v-card>
        <v-card-title>{{ dialogTitle }}</v-card-title>
        <v-divider />
        <v-card-text>
          <v-row dense>
            <v-col cols="12" md="6">
              <v-text-field v-model="form.name" :label="$t('client.name')" density="compact" hide-details />
            </v-col>
            <v-col cols="12" md="6" class="d-flex align-center">
              <v-switch v-model="form.enabled" color="primary" :label="$t('enable')" hide-details density="compact" />
            </v-col>
            <v-col cols="12">
              <v-text-field v-model="form.desc" :label="$t('client.desc')" density="compact" hide-details />
            </v-col>
            <v-col cols="12" md="6">
              <v-select
                v-model="form.client_ids"
                :items="clientItems"
                :label="$t('pages.clients')"
                multiple
                chips
                closable-chips
                density="compact"
                hide-details
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
                density="compact"
                hide-details
              />
            </v-col>
          </v-row>

          <v-expansion-panels class="mt-2" variant="accordion">
            <v-expansion-panel>
              <v-expansion-panel-title>{{ $t('awg.generatorSection') }}</v-expansion-panel-title>
              <v-expansion-panel-text>
                <v-row dense>
                  <v-col cols="12" md="6">
                    <v-select
                      v-model="genForm.intensity"
                      :items="intensityItems"
                      item-title="title"
                      item-value="value"
                      :label="$t('awg.intensity')"
                      density="compact"
                      hide-details
                    />
                  </v-col>
                  <v-col cols="12" md="6">
                    <v-select
                      v-model="genForm.profile"
                      :items="mimicItems"
                      item-title="title"
                      item-value="value"
                      :label="$t('awg.mimicProfile')"
                      density="compact"
                      hide-details
                    />
                  </v-col>
                  <v-col cols="12" v-if="showCustomHost">
                    <v-text-field
                      v-model="genForm.customHost"
                      :label="$t('awg.customHost')"
                      density="compact"
                      hide-details
                      clearable
                    />
                  </v-col>
                  <v-col cols="12" md="6">
                    <v-text-field
                      v-model.number="genForm.mtu"
                      type="number"
                      :label="$t('awg.mtu')"
                      density="compact"
                      hide-details
                    />
                  </v-col>
                  <v-col cols="12" md="6">
                    <v-text-field
                      v-model.number="genForm.junkLevel"
                      type="number"
                      :label="$t('awg.junkLevel')"
                      density="compact"
                      hide-details
                    />
                  </v-col>
                  <v-col cols="12" class="d-flex flex-wrap gap-4 align-center">
                    <v-switch v-model="genForm.mimicAll" :label="$t('awg.mimicAll')" density="compact" hide-details />
                    <v-switch v-model="genForm.routerMode" :label="$t('awg.routerMode')" density="compact" hide-details />
                    <v-switch v-model="genForm.useExtremeMax" :label="$t('awg.extremeMax')" density="compact" hide-details />
                    <v-switch v-model="genForm.useBrowserFp" :label="$t('awg.browserFp')" density="compact" hide-details />
                  </v-col>
                  <v-col cols="12" md="6" v-if="genForm.useBrowserFp">
                    <v-select
                      v-model="genForm.browserProfile"
                      :items="browserItems"
                      item-title="title"
                      item-value="value"
                      :label="$t('awg.browserProfile')"
                      density="compact"
                      hide-details
                    />
                  </v-col>
                  <v-col cols="12"><span class="text-caption text-medium-emphasis">{{ $t('awg.cpsTags') }}</span></v-col>
                  <v-col cols="12" class="d-flex flex-wrap gap-4">
                    <v-switch v-model="genForm.useTagC" label="C" density="compact" hide-details />
                    <v-switch v-model="genForm.useTagT" label="T" density="compact" hide-details />
                    <v-switch v-model="genForm.useTagR" label="R" density="compact" hide-details />
                    <v-switch v-model="genForm.useTagRC" label="RC" density="compact" hide-details />
                    <v-switch v-model="genForm.useTagRD" label="RD" density="compact" hide-details />
                  </v-col>
                  <v-col cols="12" class="d-flex flex-wrap gap-2">
                    <v-btn color="primary" variant="tonal" size="small" @click="runGenerate">{{ $t('awg.generate') }}</v-btn>
                    <v-btn variant="outlined" size="small" @click="boostRegenerate">{{ $t('awg.boostRegenerate') }}</v-btn>
                  </v-col>
                </v-row>
              </v-expansion-panel-text>
            </v-expansion-panel>
          </v-expansion-panels>

          <v-row dense class="mt-2">
            <v-col cols="12"><v-divider class="my-2" /><span class="text-caption text-medium-emphasis">{{ $t('awg.obfuscationParams') }}</span></v-col>
            <v-col cols="4" md="2" v-for="f in intFields" :key="f">
              <v-text-field v-model.number="form[f]" :label="f" type="number" density="compact" hide-details clearable />
            </v-col>
            <v-col cols="12" md="6" v-for="f in strFields" :key="f">
              <v-text-field v-model="form[f]" :label="f" density="compact" hide-details clearable />
            </v-col>
          </v-row>
        </v-card-text>
        <v-divider />
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="dialog = false">{{ $t('actions.close') }}</v-btn>
          <v-btn color="primary" :loading="saving" @click="saveProfile">{{ $t('actions.save') }}</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </v-card>
</template>

<script lang="ts">
import Data from '@/store/modules/data'
import { push } from 'notivue'
import { PROFILE_LABELS } from '@/vendor/amneziawg-architect/generator'
import {
  applyAwCfgToProfileForm,
  applyStateFromGeneratorSpec,
  clientValidateGeneratorState,
  defaultAwgGeneratorForm,
  generateAwCfg,
  generatorSpecFromState,
  type AwgGeneratorFormState,
} from '@/lib/awgGeneratorForm'

const emptyForm = (): Record<string, any> => ({
  id: 0,
  name: '',
  desc: '',
  enabled: true,
  client_ids: [] as number[],
  group_ids: [] as number[],
  jc: undefined,
  jmin: undefined,
  jmax: undefined,
  s1: undefined,
  s2: undefined,
  s3: undefined,
  s4: undefined,
  h1: '',
  h2: '',
  h3: '',
  h4: '',
  i1: '',
  i2: '',
  i3: '',
  i4: '',
  i5: '',
})

export default {
  data() {
    return {
      dialog: false,
      saving: false,
      hydratingGenForm: false,
      form: emptyForm() as Record<string, any>,
      genForm: defaultAwgGeneratorForm() as AwgGeneratorFormState,
      intFields: ['jc', 'jmin', 'jmax', 's1', 's2', 's3', 's4'],
      strFields: ['h1', 'h2', 'h3', 'h4', 'i1', 'i2', 'i3', 'i4', 'i5'],
    }
  },
  computed: {
    profiles() {
      return Data().awgObfuscationProfiles ?? []
    },
    headers() {
      return [
        { title: this.$t('client.name'), key: 'name' },
        { title: this.$t('enable'), key: 'enabled', width: 100 },
        { title: this.$t('pages.clients'), key: 'clients', width: 90 },
        { title: this.$t('pages.groups'), key: 'groups', width: 90 },
        { title: '', key: 'actions', sortable: false, width: 100 },
      ]
    },
    clientItems() {
      return (Data().clients ?? []).map((c: any) => ({ title: c.name, value: c.id }))
    },
    groupItems() {
      return (Data().userGroups ?? []).map((g: any) => ({ title: g.name, value: g.id }))
    },
    dialogTitle() {
      return this.form.id ? this.$t('actions.edit') + ' ' + this.$t('objects.awg_obfuscation_profile') : this.$t('actions.add') + ' ' + this.$t('objects.awg_obfuscation_profile')
    },
    mimicItems() {
      return Object.entries(PROFILE_LABELS).map(([value, title]) => ({ value, title }))
    },
    intensityItems() {
      return [
        { value: 'low', title: 'low' },
        { value: 'medium', title: 'medium' },
        { value: 'high', title: 'high' },
      ]
    },
    browserItems() {
      return [
        { value: 'chrome', title: 'Chrome' },
        { value: 'edge', title: 'Edge' },
        { value: 'firefox', title: 'Firefox' },
        { value: 'safari', title: 'Safari' },
        { value: 'yandex_desktop', title: 'Yandex (desktop)' },
        { value: 'yandex_mobile', title: 'Yandex (mobile)' },
      ]
    },
    showCustomHost() {
      return this.genForm.profile !== 'wireguard_noise'
    },
  },
  methods: {
    restoreGenFormFromRow(row: any) {
      const base = defaultAwgGeneratorForm()
      const raw = row?.generator_spec
      if (raw == null || raw === '') {
        this.genForm = { ...base }
        return
      }
      try {
        const obj = typeof raw === 'string' ? JSON.parse(raw) : raw
        this.genForm = applyStateFromGeneratorSpec(obj, base)
      } catch {
        this.genForm = { ...base }
      }
    },
    openDialog(id: number) {
      if (id === 0) {
        this.form = emptyForm()
        this.hydratingGenForm = true
        this.genForm = defaultAwgGeneratorForm()
        this.hydratingGenForm = false
        this.runGenerate(false)
      } else {
        const row = this.profiles.find((p: any) => p.id === id)
        if (!row) return
        this.form = {
          ...emptyForm(),
          ...row,
          client_ids: [...(row.client_ids ?? [])],
          group_ids: [...(row.group_ids ?? [])],
        }
        this.hydratingGenForm = true
        this.restoreGenFormFromRow(row)
        this.hydratingGenForm = false
      }
      this.dialog = true
    },
    runGenerate(notify = true, notifyError = true) {
      const errKey = clientValidateGeneratorState(this.genForm)
      if (errKey) {
        if (notifyError) {
          push.error({ message: this.$t(`awg.genErr.${errKey}`) as string })
        }
        return
      }
      const cfg = generateAwCfg(this.genForm)
      Object.assign(this.form, applyAwCfgToProfileForm(cfg))
      if (notify) {
        push.success({ message: this.$t('awg.generateDone') as string })
      }
    },
    boostRegenerate() {
      this.genForm.iterCount = Math.min(20, (this.genForm.iterCount ?? 0) + 1)
      this.runGenerate()
    },
    async saveProfile() {
      if (!String(this.form.name ?? '').trim()) {
        push.error({ message: this.$t('awg.nameRequired') as string })
        return
      }
      this.saving = true
      const payload = { ...this.form }
      payload.generator_spec = generatorSpecFromState(this.genForm)
      const act = this.form.id ? 'edit' : 'new'
      const ok = await Data().save('awg_obfuscation_profiles', act, payload)
      this.saving = false
      if (ok) {
        this.dialog = false
        await Data().loadData()
      }
    },
    async delProfile(id: number) {
      if (!id) return
      if (!confirm(String(this.$t('confirm')))) return
      const ok = await Data().save('awg_obfuscation_profiles', 'del', id)
      if (ok) await Data().loadData()
    },
  },
  watch: {
    genForm: {
      deep: true,
      handler() {
        if (this.hydratingGenForm || !this.dialog) return
        this.runGenerate(false, false)
      },
    },
  },
}
</script>
