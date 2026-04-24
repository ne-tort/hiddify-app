/**
 * Maps UI state ↔ AmneziaWG-Architect GeneratorInput / AWGConfig for s-ui profile editor.
 */
import {
  genCfg,
  type AWGConfig,
  type BrowserProfile,
  type GeneratorInput,
  type Intensity,
  type MimicProfile,
} from '@/vendor/amneziawg-architect/generator'

export type AwgGeneratorFormState = {
  intensity: Intensity
  profile: MimicProfile
  customHost: string
  mimicAll: boolean
  useTagC: boolean
  useTagT: boolean
  useTagR: boolean
  useTagRC: boolean
  useTagRD: boolean
  useBrowserFp: boolean
  browserProfile: BrowserProfile
  mtu: number
  junkLevel: number
  iterCount: number
  routerMode: boolean
  useExtremeMax: boolean
}

export const defaultAwgGeneratorForm = (): AwgGeneratorFormState => ({
  intensity: 'medium',
  profile: 'quic_initial',
  customHost: '',
  mimicAll: false,
  useTagC: false,
  useTagT: true,
  useTagR: true,
  useTagRC: true,
  useTagRD: true,
  useBrowserFp: false,
  browserProfile: 'chrome',
  mtu: 1500,
  junkLevel: 5,
  iterCount: 0,
  routerMode: false,
  useExtremeMax: false,
})

const AWG_VERSION = '2.0' as const

export function buildGeneratorInput(state: AwgGeneratorFormState): GeneratorInput {
  return {
    version: AWG_VERSION,
    intensity: state.intensity,
    profile: state.profile,
    customHost: state.customHost.trim(),
    mimicAll: state.mimicAll,
    useTagC: state.useTagC,
    useTagT: state.useTagT,
    useTagR: state.useTagR,
    useTagRC: state.useTagRC,
    useTagRD: state.useTagRD,
    useBrowserFp: state.useBrowserFp,
    browserProfile: state.browserProfile,
    mtu: state.mtu,
    junkLevel: state.junkLevel,
    iterCount: state.iterCount,
    routerMode: state.routerMode,
    useExtremeMax: state.useExtremeMax,
  }
}

/** Serializable spec stored in DB (camelCase matches GeneratorInput). */
export function generatorSpecFromState(state: AwgGeneratorFormState): Record<string, unknown> {
  const input = buildGeneratorInput(state)
  return { ...input, version: AWG_VERSION }
}

export function applyStateFromGeneratorSpec(
  raw: unknown,
  base: AwgGeneratorFormState,
): AwgGeneratorFormState {
  if (!raw || typeof raw !== 'object') return base
  const o = raw as Record<string, unknown>
  const pickStr = <T extends string>(k: string, d: T): T => (typeof o[k] === 'string' ? (o[k] as T) : d)
  const pickBool = (k: string, d: boolean): boolean => (typeof o[k] === 'boolean' ? (o[k] as boolean) : d)
  const pickNum = (k: string, d: number): number => (typeof o[k] === 'number' && Number.isFinite(o[k] as number) ? (o[k] as number) : d)
  return {
    intensity: pickStr('intensity', base.intensity),
    profile: pickStr('profile', base.profile),
    customHost: pickStr('customHost', base.customHost),
    mimicAll: pickBool('mimicAll', base.mimicAll),
    useTagC: pickBool('useTagC', base.useTagC),
    useTagT: pickBool('useTagT', base.useTagT),
    useTagR: pickBool('useTagR', base.useTagR),
    useTagRC: pickBool('useTagRC', base.useTagRC),
    useTagRD: pickBool('useTagRD', base.useTagRD),
    useBrowserFp: pickBool('useBrowserFp', base.useBrowserFp),
    browserProfile: pickStr('browserProfile', base.browserProfile),
    mtu: pickNum('mtu', base.mtu),
    junkLevel: pickNum('junkLevel', base.junkLevel),
    iterCount: pickNum('iterCount', base.iterCount),
    routerMode: pickBool('routerMode', base.routerMode),
    useExtremeMax: pickBool('useExtremeMax', base.useExtremeMax),
  }
}

export function generateAwCfg(state: AwgGeneratorFormState): AWGConfig {
  return genCfg(buildGeneratorInput(state))
}

/** Apply AWG 2.0 fields from generator output into flat profile form fields. */
export function applyAwCfgToProfileForm(cfg: AWGConfig): Record<string, unknown> {
  return {
    jc: cfg.jc,
    jmin: cfg.jmin,
    jmax: cfg.jmax,
    s1: cfg.s1,
    s2: cfg.s2,
    s3: cfg.s3,
    s4: cfg.s4,
    h1: cfg.h1,
    h2: cfg.h2,
    h3: cfg.h3,
    h4: cfg.h4,
    i1: cfg.i1,
    i2: cfg.i2,
    i3: cfg.i3,
    i4: cfg.i4,
    i5: cfg.i5,
  }
}

export function clientValidateGeneratorState(state: AwgGeneratorFormState): string | null {
  if (state.mtu < 576 || state.mtu > 9000) return 'mtu_range'
  if (state.junkLevel < 0 || state.junkLevel > 15) return 'junk_level_range'
  return null
}
