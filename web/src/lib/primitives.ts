import { useEffect, useState } from 'react'

import { primitiveApi } from '@/lib/rpc'

/** A level-valued primitive the guided builder can drop into a rule. */
export interface LevelPrimitive {
  pack: string
  type: string
  label: string
  params: Record<string, number>
}

// Curated default parameters for the level primitives a user can add by hand.
// The server owns which primitives exist and what each outputs; this only says
// how to seed the ones worth offering as a price reference, so an added rule is
// always immediately valid. The params are then edited inline like any node's.
// A primitive the server does not report as level is dropped regardless.
const LEVEL_DEFAULTS: Record<
  string,
  { label: string; params: Record<string, number> }
> = {
  'timeofday/period_high': {
    label: 'Previous period high',
    params: { lookback: 0 },
  },
  'timeofday/period_low': {
    label: 'Previous period low',
    params: { lookback: 0 },
  },
  'timeofday/opening_range_high': {
    label: 'Opening-range high',
    params: { startMinuteUtc: 810, rangeMinutes: 30 },
  },
  'timeofday/opening_range_low': {
    label: 'Opening-range low',
    params: { startMinuteUtc: 810, rangeMinutes: 30 },
  },
  'timeofday/session_high': {
    label: 'Session high',
    params: { startMinuteUtc: 420, endMinuteUtc: 540 },
  },
  'timeofday/session_low': {
    label: 'Session low',
    params: { startMinuteUtc: 420, endMinuteUtc: 540 },
  },
  'timeofday/session_vwap': {
    label: 'Session VWAP',
    params: { startMinuteUtc: 0, endMinuteUtc: 1439 },
  },
  'indicator/sma': { label: 'Simple moving average', params: { period: 20 } },
  'indicator/ema': {
    label: 'Exponential moving average',
    params: { period: 20 },
  },
  'indicator/wma': { label: 'Weighted moving average', params: { period: 20 } },
  'indicator/donchian_high': { label: 'Donchian high', params: { period: 20 } },
  'indicator/donchian_low': { label: 'Donchian low', params: { period: 20 } },
  'indicator/bollinger_upper': {
    label: 'Bollinger upper band',
    params: { period: 20, widthMilli: 2000 },
  },
  'indicator/bollinger_lower': {
    label: 'Bollinger lower band',
    params: { period: 20, widthMilli: 2000 },
  },
}

/**
 * The level primitives a user can compare price against, taken from the live
 * registry and narrowed to the ones we know how to seed. Empty until the server
 * answers, and empty on failure — the caller simply shows no level options
 * rather than a broken control.
 */
export function useLevelPrimitives(): LevelPrimitive[] {
  const [levels, setLevels] = useState<LevelPrimitive[]>([])
  useEffect(() => {
    let cancelled = false
    primitiveApi
      .listPrimitives({})
      .then((res) => {
        if (cancelled) return
        const out: LevelPrimitive[] = []
        for (const p of res.primitives) {
          if (p.outputKind !== 'level') continue
          const seed = LEVEL_DEFAULTS[`${p.pack}/${p.type}`]
          if (!seed) continue
          out.push({
            pack: p.pack,
            type: p.type,
            label: seed.label,
            params: seed.params,
          })
        }
        setLevels(out)
      })
      .catch(() => {
        if (!cancelled) setLevels([])
      })
    return () => {
      cancelled = true
    }
  }, [])
  return levels
}
