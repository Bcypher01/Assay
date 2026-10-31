import { useEffect, useSyncExternalStore } from 'react'
import {
  emptyStrategy,
  parseStrategy,
  withMarket,
  withSlotTimeframe,
} from '@/lib/strategy'
import type { LadderSlot } from '@/lib/strategy'

/**
 * A tiny cross-screen session store (no dependency). It threads the user's
 * selection — instrument, timeframe, window, strategy document, and the last
 * run id — between the Data, Strategy, Run, and History screens so the flow
 * feels continuous within a session. The working setup is restored locally so
 * an accidental refresh never makes the user start from scratch.
 */

export interface SessionState {
  /** Selected instrument symbol. */
  symbol: string
  /** Selected timeframe code (e.g. "M15"). */
  timeframe: string
  /** Window start, epoch ms. */
  fromMs: bigint | null
  /** Window end, epoch ms (exclusive). */
  toMs: bigint | null
  /** Optional data snapshot pin passed to Run. */
  snapshot: string
  /** Server-confirmed readiness for the exact selected market/time/window. */
  dataStatus: DataStatusState | null
  /** The strategy document, edited as JSON text. */
  strategyText: string
  /** Content hash returned by the last successful Validate. */
  validatedHash: string | null
  /** run_id from the most recent completed Run. */
  lastRunId: string | null
  assumptions: StudyAssumptionsState
  /** The user has reviewed the account and execution assumptions for this run. */
  realityConfirmed: boolean
  /** Which starting point this study came from, for the log entry. */
  templateId: string | null
  /**
   * Runs completed in this session.
   *
   * Trials are counted and shown because a promising result found on the
   * twentieth attempt is a different claim from the same result found on the
   * first. The server counts them across sessions; this is the local view.
   */
  trials: number
  /** Prevents the first-launch template from interrupting an established session. */
  onboardingSeen: boolean
}

export interface StudyAssumptionsState {
  initialEquity: bigint
  riskPerTrade: number
  fixedLots: number
  spreadTicks: bigint
  commissionBps: number
  commissionFlat: bigint
  slippageTicks: bigint
  fillPolicy: 'next_open'
}

export const DEFAULT_ASSUMPTIONS: StudyAssumptionsState = {
  // A realistic starting balance. The bundled templates risk 0.5-1% against
  // 50-100 pip FX stops; on a $100 account that request rounds down to no
  // position at the instrument's minimum lot, so every trade is skipped and
  // the first run ends in "No trades" no matter how good the rule is.
  initialEquity: 1_000_000n,
  riskPerTrade: 0.01,
  fixedLots: 0,
  spreadTicks: 0n,
  commissionBps: 0,
  commissionFlat: 0n,
  slippageTicks: 0n,
  fillPolicy: 'next_open',
}

export type DataReadinessState =
  | 'unknown'
  | 'ready'
  | 'partial'
  | 'missing'
  | 'checking'
  | 'source_unavailable'
  | 'corrupt'
  | 'unsupported'
  | 'error'

export interface DataStatusState {
  key: string
  readiness: DataReadinessState
  snapshot: string
  missingPartitions: number
  cachedFromMs: bigint | null
  cachedToMs: bigint | null
  externalAccessRequired: boolean
  errorCode: string
  message: string
  checkedAt: number
}

/**
 * An empty document in the shape the engine actually validates. The previous
 * default described fields the server has never read; a starting document that
 * cannot pass validation teaches the user that the errors are noise.
 */
export const DEFAULT_STRATEGY = JSON.stringify(emptyStrategy(), null, 2)

let state: SessionState = {
  symbol: '',
  timeframe: '',
  fromMs: null,
  toMs: null,
  snapshot: '',
  dataStatus: null,
  strategyText: DEFAULT_STRATEGY,
  validatedHash: null,
  lastRunId: null,
  assumptions: DEFAULT_ASSUMPTIONS,
  realityConfirmed: false,
  templateId: null,
  trials: 0,
  onboardingSeen: false,
}

const STORAGE_KEY = 'assay.session.v1'
let restored = false

const listeners = new Set<() => void>()

function emit() {
  for (const l of listeners) l()
}

function savedState(): Partial<SessionState> | null {
  if (typeof window === 'undefined') return null
  try {
    const raw: unknown = JSON.parse(
      window.localStorage.getItem(STORAGE_KEY) ?? '',
    )
    if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return null
    const saved = raw as Record<string, unknown>
    return {
      symbol: typeof saved.symbol === 'string' ? saved.symbol : '',
      timeframe: typeof saved.timeframe === 'string' ? saved.timeframe : '',
      fromMs: typeof saved.fromMs === 'string' ? BigInt(saved.fromMs) : null,
      toMs: typeof saved.toMs === 'string' ? BigInt(saved.toMs) : null,
      snapshot: typeof saved.snapshot === 'string' ? saved.snapshot : '',
      // Readiness is derived from the current server/cache, so always recheck
      // it after restore instead of trusting a stale browser snapshot.
      dataStatus: null,
      strategyText:
        typeof saved.strategyText === 'string'
          ? saved.strategyText
          : DEFAULT_STRATEGY,
      // The hash is tied to the exact saved document. Any later strategy edit
      // clears it in set(), so a refresh must not make a completed rules step
      // look unfinished or force the user backward through the flow.
      validatedHash:
        typeof saved.validatedHash === 'string' ? saved.validatedHash : null,
      lastRunId: typeof saved.lastRunId === 'string' ? saved.lastRunId : null,
      templateId:
        typeof saved.templateId === 'string' ? saved.templateId : null,
      trials: typeof saved.trials === 'number' ? saved.trials : 0,
      onboardingSeen: saved.onboardingSeen === true,
      // Assumptions are restored as one unit with their review state. Editing
      // any assumption clears confirmation again in set().
      realityConfirmed: saved.realityConfirmed === true,
      assumptions: {
        ...DEFAULT_ASSUMPTIONS,
        ...(saved.assumptions && typeof saved.assumptions === 'object'
          ? {
              initialEquity: BigInt(
                String(
                  (saved.assumptions as Record<string, unknown>)
                    .initialEquity ?? DEFAULT_ASSUMPTIONS.initialEquity,
                ),
              ),
              riskPerTrade: Number(
                (saved.assumptions as Record<string, unknown>).riskPerTrade ??
                  DEFAULT_ASSUMPTIONS.riskPerTrade,
              ),
              fixedLots: Number(
                (saved.assumptions as Record<string, unknown>).fixedLots ??
                  DEFAULT_ASSUMPTIONS.fixedLots,
              ),
              spreadTicks: BigInt(
                String(
                  (saved.assumptions as Record<string, unknown>).spreadTicks ??
                    DEFAULT_ASSUMPTIONS.spreadTicks,
                ),
              ),
              commissionBps: Number(
                (saved.assumptions as Record<string, unknown>).commissionBps ??
                  DEFAULT_ASSUMPTIONS.commissionBps,
              ),
              commissionFlat: BigInt(
                String(
                  (saved.assumptions as Record<string, unknown>)
                    .commissionFlat ?? DEFAULT_ASSUMPTIONS.commissionFlat,
                ),
              ),
              slippageTicks: BigInt(
                String(
                  (saved.assumptions as Record<string, unknown>)
                    .slippageTicks ?? DEFAULT_ASSUMPTIONS.slippageTicks,
                ),
              ),
              fillPolicy: 'next_open' as const,
            }
          : {}),
      },
    }
  } catch {
    return null
  }
}

function persist() {
  if (typeof window === 'undefined' || !restored) return
  const { fromMs, toMs, assumptions, dataStatus: _dataStatus, ...saved } = state
  try {
    window.localStorage.setItem(
      STORAGE_KEY,
      JSON.stringify({
        ...saved,
        assumptions: {
          ...assumptions,
          initialEquity: assumptions.initialEquity.toString(),
          spreadTicks: assumptions.spreadTicks.toString(),
          commissionFlat: assumptions.commissionFlat.toString(),
          slippageTicks: assumptions.slippageTicks.toString(),
        },
        fromMs: fromMs?.toString() ?? null,
        toMs: toMs?.toString() ?? null,
      }),
    )
  } catch {
    // Persistence is a convenience. A private browser mode or full storage
    // quota must never prevent a study from being run.
  }
}

function restore() {
  if (restored) return
  restored = true
  const saved = savedState()
  if (saved) state = { ...state, ...saved }
  emit()
}

export const session = {
  get(): SessionState {
    return state
  },
  set(patch: Partial<SessionState>) {
    const next = { ...state, ...patch }
    const dataChanged =
      (patch.symbol !== undefined && patch.symbol !== state.symbol) ||
      (patch.timeframe !== undefined && patch.timeframe !== state.timeframe) ||
      (patch.fromMs !== undefined && patch.fromMs !== state.fromMs) ||
      (patch.toMs !== undefined && patch.toMs !== state.toMs)
    const strategyChanged =
      patch.strategyText !== undefined &&
      patch.strategyText !== state.strategyText
    const assumptionsChanged =
      patch.assumptions !== undefined && patch.assumptions !== state.assumptions
    state = {
      ...next,
      dataStatus: dataChanged ? null : next.dataStatus,
      snapshot: dataChanged ? '' : next.snapshot,
      validatedHash:
        strategyChanged && patch.validatedHash === undefined
          ? null
          : next.validatedHash,
      realityConfirmed:
        assumptionsChanged && patch.realityConfirmed === undefined
          ? false
          : next.realityConfirmed,
    }
    persist()
    emit()
  },
  /** Keep the strategy document pointed at the selected market and timeframe. */
  setMarket(symbol: string, timeframe: string) {
    const document = parseStrategy(state.strategyText)
    const strategyText = document
      ? JSON.stringify(withMarket(document, symbol, timeframe), null, 2)
      : // An unparseable document is one the user is mid-edit on. Overwriting
        // it to keep two fields in step would lose their work.
        state.strategyText
    state = {
      ...state,
      symbol,
      timeframe,
      snapshot: '',
      dataStatus: null,
      strategyText,
      validatedHash: null,
    }
    persist()
    emit()
  },
  /**
   * Move one rung of the analysis ladder. The entry rung is the resolution a
   * run actually loads, so changing it re-points the data selection and clears
   * the readiness check the way a market change does; bias and poi only edit
   * the document. An unparseable document is left untouched — the user is
   * mid-edit and overwriting it would lose their work.
   */
  setSlotTimeframe(slot: LadderSlot, tf: string) {
    const document = parseStrategy(state.strategyText)
    if (!document) return
    const strategyText = JSON.stringify(
      withSlotTimeframe(document, slot, tf),
      null,
      2,
    )
    state = {
      ...state,
      strategyText,
      validatedHash: null,
      ...(slot === 'entry'
        ? { timeframe: tf, snapshot: '', dataStatus: null }
        : {}),
    }
    persist()
    emit()
  },
  /** Record a completed run. Every run is a trial, including a repeat. */
  recordRun(runId: string) {
    state = { ...state, lastRunId: runId, trials: state.trials + 1 }
    persist()
    emit()
  },
  setDataStatus(dataStatus: DataStatusState) {
    if (dataStatus.key !== dataKey(state)) return
    state = {
      ...state,
      dataStatus,
      snapshot: dataStatus.readiness === 'ready' ? dataStatus.snapshot : '',
    }
    persist()
    emit()
  },
  reset() {
    state = {
      symbol: '',
      timeframe: '',
      fromMs: null,
      toMs: null,
      snapshot: '',
      dataStatus: null,
      strategyText: DEFAULT_STRATEGY,
      validatedHash: null,
      lastRunId: null,
      assumptions: DEFAULT_ASSUMPTIONS,
      realityConfirmed: false,
      templateId: null,
      trials: 0,
      onboardingSeen: false,
    }
    if (typeof window !== 'undefined')
      window.localStorage.removeItem(STORAGE_KEY)
    emit()
  },
  subscribe(listener: () => void) {
    listeners.add(listener)
    return () => listeners.delete(listener)
  },
}

export function dataKey(
  value: Pick<SessionState, 'symbol' | 'timeframe' | 'fromMs' | 'toMs'>,
): string {
  return [
    value.symbol,
    value.timeframe,
    value.fromMs?.toString() ?? '',
    value.toMs?.toString() ?? '',
  ].join('|')
}

export function hasReadyData(value: SessionState): boolean {
  return (
    value.dataStatus?.key === dataKey(value) &&
    value.dataStatus.readiness === 'ready'
  )
}

/** Subscribe a component to the whole session state. */
export function useSession(): SessionState {
  useEffect(() => {
    restore()
  }, [])
  return useSyncExternalStore(session.subscribe, session.get, session.get)
}
