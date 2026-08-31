import { createClient } from '@connectrpc/connect'
import { createConnectTransport } from '@connectrpc/connect-web'
import {
  DataService,
  EventStudyService,
  PrimitiveService,
  StudyService,
  RunService,
  TemplateService,
} from '@/gen/assay/v1/service_connect'

/**
 * Base URL for the local Assay server.
 *
 * Default is the dev-server proxy path "/api" (see vite.config.ts), which
 * forwards to http://127.0.0.1:8080 and sidesteps cross-origin issues while the
 * UI runs on :3000. Override with VITE_ASSAY_BASE_URL to talk to the server
 * directly (e.g. "http://127.0.0.1:8080") when it is served same-origin.
 */
const baseUrl = import.meta.env.VITE_ASSAY_BASE_URL ?? '/api'

/** Public static resources served by the same local Assay server as ConnectRPC. */
export const assayHttpBaseUrl = baseUrl

export const transport = createConnectTransport({
  baseUrl,
  // The Connect protocol carries server-streaming responses over fetch; no
  // extra configuration is required for EnsureData / Run.
})

export const dataApi = createClient(DataService, transport)
export const eventStudyApi = createClient(EventStudyService, transport)
export const primitiveApi = createClient(PrimitiveService, transport)
export const studyApi = createClient(StudyService, transport)
export const runApi = createClient(RunService, transport)
export const templateApi = createClient(TemplateService, transport)

/** Normalize a thrown ConnectError (or anything) into a display string. */
export function rpcErrorMessage(err: unknown): string {
  if (err && typeof err === 'object' && 'message' in err) {
    return String(err.message)
  }
  return String(err)
}

/**
 * True when the server does not implement the method that was called.
 *
 * This client is generated from the current contract, and a user's local
 * server can be older than it. That is an ordinary situation, not a fault:
 * the screen should say what is missing and carry on with what it has, rather
 * than presenting an empty panel that looks like a bug in their setup.
 *
 * ConnectError carries a numeric code (12 is UNIMPLEMENTED). The string check
 * covers transports that surface the name instead.
 */
export function isUnimplemented(err: unknown): boolean {
  if (!err || typeof err !== 'object') return false
  const code = (err as { code?: unknown }).code
  if (code === 12 || code === 'unimplemented') return true
  const message = 'message' in err ? String(err.message) : ''
  return /unimplemented|not implemented|unknown (rpc |procedure|method)/i.test(
    message,
  )
}
