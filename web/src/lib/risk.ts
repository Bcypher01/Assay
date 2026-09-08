import { assetClassHint } from '@/lib/constraints'

export type RiskUnit = 'pips' | 'percent' | 'ticks' | 'price' | 'atr'

export interface RiskDistance {
  unit: RiskUnit
  value: number
}

export function defaultRiskUnit(symbol: string): RiskUnit {
  return assetClassHint(symbol) === 'crypto' ? 'percent' : 'pips'
}

export function defaultRiskDistance(
  symbol: string,
  kind: 'stop' | 'target',
): RiskDistance {
  const unit = defaultRiskUnit(symbol)
  if (unit === 'percent') {
    return { unit, value: kind === 'stop' ? 0.5 : 1.5 }
  }
  return { unit, value: kind === 'stop' ? 10 : 30 }
}

export function riskDistance(
  value: unknown,
  symbol: string,
  kind: 'stop' | 'target',
): RiskDistance {
  const fallback = defaultRiskDistance(symbol, kind)
  if (typeof value === 'number' && value > 0) {
    return { ...fallback, value }
  }
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return fallback
  }
  const item = value as Record<string, unknown>
  const unit =
    item.unit === 'pips' ||
    item.unit === 'percent' ||
    item.unit === 'ticks' ||
    item.unit === 'price' ||
    item.unit === 'atr'
      ? item.unit
      : fallback.unit
  return {
    unit,
    value:
      typeof item.value === 'number' && item.value > 0
        ? item.value
        : fallback.value,
  }
}

export function riskLabel(unit: RiskUnit, kind: 'stop' | 'target') {
  const base = kind === 'stop' ? 'Stop loss' : 'Take profit'
  switch (unit) {
    case 'percent':
      return `${base} (%)`
    case 'ticks':
      return `${base} (ticks)`
    case 'price':
      return `${base} (price units)`
    case 'atr':
      return `${base} (× ATR(14))`
    default:
      return `${base} (pips)`
  }
}

export function riskStep(unit: RiskUnit) {
  return unit === 'percent' || unit === 'atr' ? '0.1' : '1'
}
