<template>
  <v-autocomplete
    :model-value="modelValue"
    @update:model-value="$emit('update:modelValue', $event)"
    :items="choiceItems"
    item-title="title"
    item-value="value"
    multiple
    chips
    closable-chips
    :label="label"
    :clearable="clearable"
    density="compact"
    hide-details
  />
</template>

<script lang="ts" setup>
import { computed } from 'vue'
import { i18n } from '@/locales'

const props = withDefaults(
  defineProps<{
    modelValue: number[]
    userGroups: unknown[]
    label?: string
    /** Exclude group id (e.g. when picking container groups for the same group). */
    excludeId?: number
    clearable?: boolean
  }>(),
  { label: '', clearable: true },
)

defineEmits<{ (e: 'update:modelValue', v: number[]): void }>()

const choiceItems = computed(() => {
  const ex = props.excludeId ?? 0
  const known = (props.userGroups ?? [])
    .filter((g: any) => g.id !== ex)
    .map((g: any) => ({ title: g.name, value: g.id }))
  const knownIDs = new Set<number>(known.map((g: any) => g.value))
  const orphan = (props.modelValue ?? [])
    .filter((id: number) => id !== ex && !knownIDs.has(id))
    .map((id: number) => ({ title: i18n.global.t('group.deletedPlaceholder', { id }), value: id }))
  return [...known, ...orphan]
})
</script>
