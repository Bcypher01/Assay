---
name: assay-rpc-method
description: Add or change an Assay RPC method or wire format - editing proto/assay/v1, regenerating Go and TypeScript stubs, and wiring the handler through to the engine. Use when asked to expose something to the UI, add an endpoint, change a request or response message, or when the web client and server disagree about a field.
---

# Adding an RPC method

The transport layer is thin on purpose. A handler translates wire messages and
nothing else: no business logic, no computation, no branching on results. If a
handler is deciding something, that decision belongs in `internal/engine`,
`internal/analytics` or `internal/runstore`.

## The path a change travels

```
proto/assay/v1/*.proto
  → make proto                 (rewrites gen/go — never hand-edit)
  → internal/rpc/server.go     (the Func field + handler)
  → internal/rpc/local.go      (production wiring to engine/store/history)
  → cd web && npm run generate-rpc   (rewrites web/src/gen)
  → web/src/lib/rpc.ts and the route that calls it
```

## Proto rules

- The service protos in `proto/assay/v1` are the **public contract**. Everything
  else in the repo is internal and may change; these may not, except additively.
- Add fields, never renumber or reuse a tag. Removing one is a breaking change
  to a client you do not control.
- `go_package` stays `github.com/bcypher01/assay/gen/go/assay/v1;assayv1`.
- Prices and timestamps cross the wire as integers. `from_ms`/`to_ms` are
  `sint64` and arrive in TypeScript as `bigint`; `strategy_json`/`params_json`
  are `bytes` and arrive as `Uint8Array` (see `web/src/lib/codec.ts`).

## Server rules

- `rpc.Server` holds one exported `...Fn` field per operation. The handler
  calls it and maps the result to the wire message. Tests inject fakes through
  those fields; production wiring lives in `NewLocalServer*` in `local.go`.
- Streaming methods deliver **exactly one terminal update**. Non-terminal
  progress goes through the `send`/`report` callback; the terminal summary is
  the return value. Do not send a terminal update through the callback.
- Errors returned to the client keep their `%w` chain. Validation errors from
  the IR carry the offending node ID and field path, because the UI renders
  them next to the control that caused them — do not flatten them to a string.
- Telemetry is opt-in and local-only. A recorder failure never affects a study.

## Client rules

- `web/src/gen` is generated. Regenerate with `npm run generate-rpc` from
  `web/`; do not hand-edit, and do not copy stubs between directories.
- The browser talks to the local server same-origin through the dev proxy
  (`/api` → `127.0.0.1:8080`), which is what keeps streaming working without
  CORS. See `web/README.md` and `web/AGENTS.md` for the UI-side conventions.

## Tests it must ship with

- A transport test that injects a fake `...Fn` and asserts the wire mapping,
  including the error path.
- For a streaming method, a test that asserts exactly one terminal update.
- Golden JSON for any structured response shape.

Then run the `assay-verify` skill — CI fails if `gen/go` is stale.
