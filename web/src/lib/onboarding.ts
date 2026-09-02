import { parseStrategy } from '@/lib/strategy'
import type { StrategyDoc } from '@/lib/strategy'

export interface OnboardingStudy {
  document: StrategyDoc
  symbol: string
  timeframe: string
  fromMs: bigint
  toMs: bigint
}

type JsonObject = Record<string, unknown>

const asObject = (value: unknown): JsonObject | null =>
  value && typeof value === 'object' && !Array.isArray(value)
    ? (value as JsonObject)
    : null

const asBigInt = (value: unknown): bigint | null => {
  if (typeof value === 'bigint') return value
  if (typeof value === 'number' && Number.isSafeInteger(value))
    return BigInt(value)
  if (typeof value === 'string' && /^-?\d+$/.test(value)) return BigInt(value)
  return null
}

/**
 * Read the server-owned first study. This remains ordinary JSON rather than an
 * RPC because onboarding is a public static resource served beside the local
 * Assay API. Support snake/camel case so the UI works with the documented
 * JSON as well as Go's default JSON encoding.
 */
export async function loadOnboarding(
  baseUrl: string,
): Promise<OnboardingStudy> {
  const url = `${baseUrl.replace(/\/$/, '')}/onboarding.json`
  const response = await fetch(url)
  if (!response.ok)
    throw new Error(`Onboarding request failed (${response.status}).`)
  const body = asObject(await response.json())
  if (!body) throw new Error('Onboarding response was not a JSON object.')

  const window = asObject(body.data) ?? asObject(body.window) ?? body
  const rawDocument =
    body.strategy ?? body.template ?? body.strategy_json ?? body.strategyJson
  const document = parseStrategy(
    typeof rawDocument === 'string' ? rawDocument : JSON.stringify(rawDocument),
  )
  const symbol = String(window.symbol ?? '')
  const timeframe = String(window.timeframe ?? '')
  const fromMs = asBigInt(window.from_ms ?? window.fromMs)
  const toMs = asBigInt(window.to_ms ?? window.toMs)
  if (
    !document ||
    !symbol ||
    !timeframe ||
    fromMs === null ||
    toMs === null ||
    fromMs >= toMs
  )
    throw new Error(
      'Onboarding response did not include a valid strategy and data window.',
    )
  return { document, symbol, timeframe, fromMs, toMs }
}
