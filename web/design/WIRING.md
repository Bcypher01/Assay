# Wiring the run-log UI to the engine

What changed, why, and the two commands you need to run.

## The problem this fixes

`src/gen/assay/v1/*` was generated from a newer contract than
`backend/proto/assay/v1/service.proto` described. Four methods the UI calls did
not exist on the server:

| Method                          | What broke without it                                                                                     |
| ------------------------------- | --------------------------------------------------------------------------------------------------------- |
| `TemplateService.ListTemplates` | the starting-point gallery rendered empty                                                                 |
| `StudyService.Describe`         | `/strategy` did `Promise.all([validate, describe])`, so the rejection took validation errors down with it |
| `RunService.GetDetails`         | the whole result page failed                                                                              |
| `RunService.GetTrades`          | the trade inspector failed                                                                                |

Both sides are now reconciled: the proto and Go handlers were written to match
the client that already existed, and the client degrades cleanly when it meets
an older server.

## Run these

```sh
cd backend && make proto && make build && make test
npm run build
```

`make proto` regenerates the Go stubs from the extended `service.proto`. The
TypeScript stubs already match it and do not need regenerating. I could not
compile the Go here (no toolchain in the mounted folder), so run
`gofmt -w ./backend` if `make lint` complains about alignment.

## Two behaviour changes worth knowing about

**`direction: "both"` is now rejected.** `engine.addSides` resolves every entry
to one side and its fallback is long, so a document that said "both" was
quietly running long-only while reporting itself as bidirectional. That is the
exact failure this product exists to prevent, so `Strategy.Validate` now
returns a field error explaining it instead. Long and short are separate
studies until the document can carry independent branches (§4.2). The UI no
longer offers "both", and switching direction rewrites `ma_cross` to
`ma_cross_up` / `ma_cross_down`.

**Run assumptions are applied and stored.** `runLocal` hardcoded
`RiskModel{InitialEquity: 100_000, FixedLots: 1}` and ignored everything the
Trading Reality screen collected. `applyAssumptions` now folds the request's
assumptions into the instrument spec and sim config, and `analytics.Study`
gained an `Assumptions` field so a stored result can be read back with the
costs that produced it. A zero override still means "use the instrument's own
cost" — there is no input combination that yields a zero-cost backtest.

## New files

**Backend**

- `internal/ir/describe.go` — generates the plain-English reading from the
  executable document. Engine vocabulary (packs, slots, primitive types) never
  reaches the user.
- `internal/rpc/templates.go` — the six-template catalogue, each one an
  executable document. No RSI or Bollinger template: both need a numeric
  primitive compared against a threshold, and a slot accepts booleans only.
  They arrive with the predicate layer (§4.1).
- `internal/rpc/details.go` — `Describe`, `ListTemplates`, `GetDetails`,
  `GetTrades`, the analytics→proto mapping, and the next-action rubric.

**Frontend**

- `src/lib/strategy.ts` — the document typed as `ir.Strategy` actually defines
  it. The old default document described fields the server has never read.
- `src/lib/describe.ts` — a local reading, shown only while the server request
  is in flight and always labelled as provisional.
- `src/lib/templates.ts` — bundled copies of the catalogue, used when the
  server has no `ListTemplates` yet, with a notice saying so.

## Known gaps

- Stored trades keep entry facts and both bounded net outcomes only. Exit
  times, exit prices and per-trade costs are not persisted, so `GetTrades`
  returns them as zero rather than reconstructing them.
- `near_misses` is empty: the engine does not yet report near-miss counts.
- `walk_forward` is empty until a run populates it; `cost_stress`,
  `monte_carlo` and `sensitivity` are mapped and will fill in as soon as
  `analytics` is driven with a sweep config.
- ESLint and `vite build` could not run in the mounted folder — `node_modules`
  holds macOS-only native bindings. `tsc --noEmit` passes clean.

---

## Event study wired into the UI (27 Aug 2026)

`EventStudyService` (`QueryEvents` + `RunEventStudy`) and `eventStudyApi` already
existed; there was no screen consuming them. Added one.

**`src/routes/events.tsx` — a standalone `/events` tool, not a pipeline step.**
An event study asks a different question from a strategy backtest ("how did this
market move after an event", not "would a rule have profited"), so it sits
outside the six-step run log rather than inside it. Two halves map onto the two
RPCs: a snapshot + filter panel feeds `QueryEvents` (preview matching events) and
`RunEventStudy` (measure forward moves). Excluded events are shown first, with
their engine reasons rewritten in plain English ("timestamp precision is not
exact" → "the recorded time was not precise enough to line up with a price
bar"), because disclosed exclusions are the point of the tool. Snapshot hashes
use the same truncate-and-copy `Hash` treatment as the result page. Forward
horizons are offered as durations (1 hour / 4 hours / 1 day / 1 week) that map to
timeframe codes the server parses.

**Graceful degradation.** Both calls run through `isUnimplemented`: a server
older than the event-study contract (e.g. the Aug-24 `bin/assay`) yields a
"rebuild with `make build`" notice instead of a broken panel. A
"bars are not prepared" error links back to the Prepare-the-history screen,
since an event study reads the same local bar cache a backtest does.

**Nav.** A "Tools" section was added to the run-log spine (`app-nav.tsx` +
`.runlog-tools` / `.runlog-tool` in `styles.css`) linking to `/events`, kept
visually distinct from the numbered steps.

**Point-in-time honesty.** The filter exposes `as_of_ms` as a "Knowledge as of"
date, so a study can be run as if standing at a past moment with no later
revisions leaking in.

Route registered with `npx tsr generate` (the `tsr` CLI runs in the mounted
folder even though vite does not). `tsc --noEmit` and `prettier` pass clean. Not
yet exercised against a live server: no local event snapshot exists, and the
prebuilt binary predates the contract.

---

## Setup test (`/setup`) — 28 Aug 2026

The Setup Test screen moved out of the Go server and into this app. It was
previously an embedded, dependency-free page in `internal/setupui`
(`index.html` + `styles.css` + `app.js`, served at `/setup/`, with `/`
redirecting to it). That package is deleted; the server now serves no interface
at all and answers `/` with a small JSON index of its entry points.

**What stayed on the server.** The three strict JSON endpoints —
`POST /api/setup/{chart,preview,run}` — are the contract, unchanged. They are
plain JSON rather than ConnectRPC because a drawing has to be evaluated before
a strategy document exists. The Go decoder uses `DisallowUnknownFields`, so an
extra key in a request body is a 400, not a silently ignored property.

**Client.** `src/lib/setup.ts` mirrors `internal/setuptest` field for field,
plus `SetupError` (carrying the HTTP status, so 501 "not built yet" reads
differently from 400 "your drawing is invalid"), `validateDrawing` (the same
checks and order as `Drawing.Validate`, keyed by field), and the tick↔price
display helpers. Every price is an integer tick until the moment it is painted.
Note the base-URL arithmetic: `assayHttpBaseUrl` is `/api` and the Vite proxy
strips exactly one leading `/api`, so `/api` + `/api/setup/chart` arrives as
`/api/setup/chart`; pointing `VITE_ASSAY_BASE_URL` at the server directly gives
the same path with no proxy.

**Chart.** `src/components/setup-chart.tsx` replaces the ~40 lines of canvas in
the old `app.js`. Deliberate choices: drag snaps to the O/H/L/C of the candle
under the cursor (Alt places freely), because a stop "at the low" should be
exactly the low; the anchor candle is _framed_, not filled, so an example never
reads as a result; colours come from new `--chart-*` tokens rather than from
`--success`/`--destructive`, because a rising candle is not an endorsement;
wheel zoom keeps the bar under the cursor fixed; the whole surface is keyboard
operable (arrows move the anchor, `N` picks a line, arrows then nudge it a tick
at a time, Shift ×10) and `Tab` is deliberately _not_ intercepted, so the canvas
is never a focus trap.

**Screen.** `src/routes/setup.tsx` — a standalone tool under "Tools" in the
spine beside Event study, not a numbered run-log step: it is an alternative
authoring path, not a stage of the seven. Copy follows the house rule (bounded
metrics led by the pessimistic reading, "average per trade" not "expectancy",
unresolved bars explained rather than hidden). Saving is gated on
`sampleMatched` — a rule that misses its own example is not a rule the user has
described yet — and a saved run goes through `session.recordRun`, so it counts
as a trial and lands on the normal verdict page.

Route registered with `npx tsr generate`. `tsc --noEmit` and `prettier` clean.
The Go side is `gofmt` clean but was **not compiled**: there is no Go toolchain
in the mounted folder, and the cloud container cannot reach `proxy.golang.org`
to fetch the module cache. Run `make build && make test` locally.

### Revised the same day — full-screen workspace, real candlesticks

The first pass wrapped the screen in the ordinary `.assay-stage` page (cards,
1120px column, page scroll). That threw away what the backend's embedded version
had right: it was a _workspace_, not a page — `height: calc(100vh - 58px)`, a
toolbar strip, a chart taking every remaining pixel, a legend rail, and a fixed
side column.

`/setup` now does the same inside the app shell. `.assay-stage:has(>
.setup-workspace)` drops the page padding and max-width and takes the full
viewport height; the workspace is `minmax(0,1fr) 384px`; the toolbar, legend and
result rails are fixed-height strips and each region scrolls itself, so the
chart never slides out from under the controls that change it. No Cards on this
screen at all — hairlines and the existing tokens, at the density the original
had. `design/setup-test-prototype.html` is a self-contained mock of the layout
(runs without vite, which cannot start in the mounted folder).

Candles are now actual candlesticks. Two things were wrong: the body width was
capped at 9px and the chart opened on the entire fetched window, so ~400 bars
drew as one-pixel sticks — a bar chart wearing a candlestick's name. The body
width now follows the zoom, the chart opens on the most recent
`DEFAULT_VISIBLE = 120` candles, and bodies are hollow on an up close and filled
on a down close, so direction reads from weight and not from colour alone. A
doji, or a zoom too far out for a body to hold an outline, falls back to a solid
mark rather than an empty box. Hollow bodies are painted with `--card` rather
than cleared, because `clearRect` would punch a transparent hole through the
zone shading. `MAX_BARS` was raised to 800: the server truncates to the most
recent N bars rather than downsampling, so every candle on screen is real and
zooming out reaches actual history.

### And the spine goes too

`/setup` opts out of the run-log shell entirely rather than sitting inside it.
`__root.tsx` checks `useMatches()` for `staticData.fullscreen` (declared by
augmenting `StaticDataRouteOption` in `router.tsx`) and returns a bare `Outlet`
for those routes; `src/routes/setup.tsx` sets `staticData: { fullscreen: true }`.
The mechanism is deliberately a route flag rather than a path check in the root,
so the next workspace screen opts in the same way.

The spine is the wayfinder for the seven-step study flow. On a drawing surface
it is a permanent 276px tax on the thing being worked on, and the step it would
highlight is not the one the user is in. Two things it was carrying had to be
re-homed: the way out is now a "‹ Run log" link at the top of the toolbar (a
link to `/`, not browser history — the tool is reachable from anywhere, and
"back" should mean the study flow), and the honest-framing line is back where
the original had it, as "Historical simulation only · no order routing" at the
right end of the toolbar.
