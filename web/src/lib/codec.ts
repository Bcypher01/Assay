/**
 * Encoding helpers for the Assay wire contract.
 *
 * The proto surface uses two representations that need care in the browser:
 *   - `strategy_json` / `params_json` are `bytes` -> `Uint8Array` in the
 *     generated TypeScript. We carry the strategy document as a plain JS object
 *     and (de)serialize it here.
 *   - `from_ms` / `to_ms` are `sint64` -> `bigint`. Dates are epoch milliseconds
 *     throughout; we never use `number` for them, to avoid precision loss and to
 *     match the generated field types exactly.
 */

const encoder = new TextEncoder()
const decoder = new TextDecoder()

/**
 * Serialize a strategy document (or any JSON value) to the `bytes` wire type.
 * Returns an ArrayBuffer-backed view to match the generated field type exactly.
 */
export function toJsonBytes(value: unknown): Uint8Array<ArrayBuffer> {
  return Uint8Array.from(encoder.encode(JSON.stringify(value ?? null)))
}

/** Parse a `bytes` JSON field back into a value. Returns `null` when empty. */
export function fromJsonBytes<T = unknown>(
  bytes: Uint8Array | undefined,
): T | null {
  if (!bytes || bytes.length === 0) return null
  return JSON.parse(decoder.decode(bytes)) as T
}

/** Pretty-print any JSON value with a stable 2-space indent. */
export function prettyJson(value: unknown): string {
  return JSON.stringify(value, null, 2)
}

/**
 * Convert a `<input type="date">` value ("YYYY-MM-DD") to epoch-ms `bigint`,
 * interpreting the day as UTC midnight so the mapping is deterministic and
 * timezone-independent. Returns `null` for empty/invalid input.
 */
export function dateInputToMs(value: string): bigint | null {
  if (!value) return null
  const ms = Date.parse(`${value}T00:00:00Z`)
  return Number.isNaN(ms) ? null : BigInt(ms)
}

/** Convert epoch-ms `bigint` to a UTC "YYYY-MM-DD" string for a date input. */
export function msToDateInput(ms: bigint | number | undefined): string {
  if (ms === undefined) return ''
  const d = new Date(Number(ms))
  if (Number.isNaN(d.getTime())) return ''
  return d.toISOString().slice(0, 10)
}

/** Human-readable epoch-ms timestamp (UTC). */
export function formatMs(ms: bigint | number | undefined): string {
  if (ms === undefined) return '—'
  const d = new Date(Number(ms))
  if (Number.isNaN(d.getTime())) return '—'
  return `${d.toISOString().replace('T', ' ').slice(0, 16)} UTC`
}

/** Format a byte count as B / KiB / MiB / GiB. */
export function formatBytes(bytes: bigint | number | undefined): string {
  const n = Number(bytes ?? 0)
  if (n <= 0) return '0 B'
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB']
  const i = Math.min(units.length - 1, Math.floor(Math.log(n) / Math.log(1024)))
  const v = n / Math.pow(1024, i)
  return `${v.toFixed(i === 0 ? 0 : 1)} ${units[i]}`
}

/** Format an Assay account value, which is always an integer number of USD cents. */
export function formatUsdCents(
  cents: bigint | number | undefined,
  { sign = false }: { sign?: boolean } = {},
): string {
  if (cents === undefined || cents === null || !Number.isFinite(Number(cents)))
    return '—'
  const value = typeof cents === 'bigint' ? Number(cents) : cents
  const prefix = sign && value > 0 ? '+' : ''
  // An explicit locale keeps the server-rendered HTML and the browser's
  // hydration pass in agreement. `undefined` resolves to each runtime's own
  // default (en-US on the SSR process, the visitor's locale in the browser),
  // so a non-US visitor saw US$10,000.00 where the server had written
  // $10,000.00 and React threw away the tree on hydration.
  return `${prefix}${new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
  }).format(value / 100)}`
}

/** Convert a non-negative USD input to cents without accepting fractional cents. */
export function usdInputToCents(value: string): bigint | null {
  if (!/^\d+(?:\.\d{0,2})?$/.test(value)) return null
  const [whole, fraction = ''] = value.split('.')
  return BigInt(whole) * 100n + BigInt((fraction + '00').slice(0, 2))
}

/** Present cents as a decimal USD value suitable for a number input. */
export function centsToUsdInput(cents: bigint): string {
  const whole = cents / 100n
  const fraction = (cents % 100n).toString().padStart(2, '0')
  return fraction === '00' ? whole.toString() : `${whole}.${fraction}`
}

/** Short-form a run hash for display, keeping it copy-friendly on hover. */
export function shortHash(hash: string | undefined, head = 10): string {
  if (!hash) return '—'
  return hash.length > head + 2 ? `${hash.slice(0, head)}…` : hash
}
