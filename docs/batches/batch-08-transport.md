# Batch 08 — ConnectRPC transport

**Depends on:** 07
**Blocks:** 12
**Packages:** `proto/assay/v1`, `internal/rpc`, `gen/go`

## Goal

Expose the engine as a typed service with streaming, so the UI is a generated
client and the future Python SDK and CLI cost nothing. Built after the core
because building it earlier means rewriting it as the core's shape changes.

## Deliverables

Protos for four services:

- `DataService` — list instruments, describe availability and quality tier,
  ensure data with **streaming progress**, escalate windows
- `PrimitiveService` — list registered primitives with parameter schemas,
  evaluate one primitive and stream back overlays for a viewport
- `StudyService` — validate a strategy, run it, **stream partial results** so the
  equity curve draws as it computes
- `RunService` — history, fetch a run, compare two runs

Plus:

- ConnectRPC handlers wiring to the engine, with context cancellation honoured
- Viewport downsampling: series requests return points sized to the viewport, not
  raw bars
- Bulk transfer path for anything that would exceed message limits
- Generated Go and TypeScript clients checked into `gen/`

## Key decisions

**ConnectRPC, not grpc-web.** One Go server speaks gRPC, gRPC-Web and plain HTTP
with no Envoy sidecar. The client is a browser; this removes the proxy entirely.

**Streaming is the reason this batch exists.** Download progress, sweep progress
and incremental study results are all server-streaming. A 90-second run behind a
unary call is a spinner; behind a stream it is a chart filling in.

**Never send raw bar arrays.** 2.6M bars as protobuf messages exceeds default
size limits and wastes the transfer regardless. Downsample to the viewport
server-side; use a binary blob path for genuine bulk.

**Handlers are thin.** No computation, no branching on results, no business
logic. If a handler needs to decide something, that decision belongs in the
engine or analytics layer.

**Cancellation is real.** A user closing a tab must stop the sweep. Context
cancellation is checked inside long loops, not only between stages.

## Interfaces

```protobuf
service StudyService {
  rpc Validate(ValidateRequest) returns (ValidateResponse);
  rpc Run(RunRequest) returns (stream RunUpdate);
}

message RunUpdate {
  oneof payload {
    Progress   progress = 1;   // percent, stage, ETA
    EquityChunk equity  = 2;   // incremental curve points
    TradeChunk  trades  = 3;
    RunSummary  summary = 4;   // terminal, includes bounds and unresolved count
  }
}
```

## Tests

- Round-trip: every service method against an in-process server
- Streaming: a long run emits progress before completion and a terminal summary
  exactly once
- Cancellation: cancelling mid-run stops work within a bounded time and leaks no
  goroutine — asserted with `goleak`
- Downsampling: a viewport request over 5 years of M15 returns a bounded point
  count and preserves visual extremes
- Generated TypeScript client compiles against the checked-in protos

## Exit criteria

- [ ] Overlay repaint over 5 years of M15 completes under 1 second end to end
- [ ] A cancelled study stops within 200ms and leaks no goroutines
- [ ] No response message exceeds the default size limit under any fixture
- [ ] `buf lint` and `buf breaking` clean against the previous proto revision

## Out of scope

The UI itself. Authentication — the server binds to localhost and has no accounts
in v0. Hosted deployment.

## Decision log

_Record the buf configuration, proto package versioning policy, and the downsampling algorithm._

**Implemented decisions.** `buf.yaml` uses the `buf.build/assay/assay` module
and `buf.gen.yaml` generates source-relative Go output; the generated Connect
service is checked in beside the protobuf Go model. New wire fields are added
only to `assay.v1`; incompatible changes require a new versioned package.
Viewport responses use chronological min/max bucket sampling: retain the first
and last bar, then retain the low and high extrema from each interior bucket.
This bounds payload size while preserving visible spikes.
