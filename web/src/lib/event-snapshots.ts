import { assayHttpBaseUrl } from '@/lib/rpc'

export interface EventSnapshotMeta {
  hash: string
  source: string
  eventCount: number
  fromMs: bigint | null
  toMs: bigint | null
  taxonomyVersion: string
  schemaVersion: string
  createdAtMs: bigint | null
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

const asTimestampMs = (value: unknown): bigint | null => {
  const numeric = asBigInt(value)
  if (numeric !== null) return numeric
  if (typeof value !== 'string') return null
  const parsed = Date.parse(value)
  return Number.isNaN(parsed) ? null : BigInt(parsed)
}

function readSnapshot(value: unknown): EventSnapshotMeta | null {
  const raw = asObject(value)
  if (!raw) return null
  const coverage = asObject(raw.coverage)
  const hash = String(raw.hash ?? raw.contentHash ?? '')
  if (!hash) return null
  return {
    hash,
    source: String(raw.source ?? ''),
    eventCount: Number(raw.eventCount ?? raw.rows ?? 0),
    fromMs: asTimestampMs(
      raw.fromMs ??
        raw.first ??
        coverage?.fromMs ??
        coverage?.from ??
        coverage?.first,
    ),
    toMs: asTimestampMs(
      raw.toMs ?? raw.last ?? coverage?.toMs ?? coverage?.to ?? coverage?.last,
    ),
    taxonomyVersion: String(raw.taxonomyVersion ?? ''),
    schemaVersion: String(raw.schemaVersion ?? ''),
    createdAtMs: asTimestampMs(raw.createdAtMs ?? raw.createdAt),
  }
}

/** Read the local immutable-snapshot catalogue served by Assay. */
export async function listEventSnapshots(): Promise<EventSnapshotMeta[]> {
  const response = await fetch(
    `${assayHttpBaseUrl.replace(/\/$/, '')}/event-snapshots`,
  )
  if (!response.ok) {
    throw new Error(
      `Event snapshot catalogue request failed (${response.status}).`,
    )
  }
  const json: unknown = await response.json()
  const body = asObject(json)
  const raw = Array.isArray(json)
    ? json
    : body && Array.isArray(body.snapshots)
      ? body.snapshots
      : null
  if (!raw) throw new Error('Event snapshot catalogue had an unexpected shape.')
  return raw
    .map(readSnapshot)
    .filter((snapshot): snapshot is EventSnapshotMeta => snapshot !== null)
}
