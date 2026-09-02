# Assay web client

The web client for **Assay**, the honest backtester. A TanStack Start
(React 19 + Vite) app that talks to the Go service over ConnectRPC. It never
touches R2 credentials or the bar library directly — every call goes through
the service.

This application lives at `web/` in the Assay repository. In production the
top-level Caddy gateway serves both applications through one origin: browser
requests to `/api/*` reach the Go service and every other route reaches this
web application. That preserves streaming and avoids CORS configuration.

## Screens

The app is a four-step flow, one route each:

1. **Instruments & Data** (`/data`) — `dataApi.listInstruments()` and a streamed
   `dataApi.ensureData({ symbol, timeframe, fromMs, toMs })` that renders
   `stage`, `currentFile`, and `bytesDone / bytesTotal` as the local cache fills.
2. **Strategy Editor** (`/strategy`) — `primitiveApi.listPrimitives()`, a JSON
   strategy document, and `studyApi.validate({ strategyJson })` with errors
   mapped by `path`.
3. **Run** (`/run`) — streams `studyApi.run({ strategyJson, fromMs, toMs })`,
   rendering progress, equity chunks, and trade count, then the terminal
   `RunSummary`. The `summary.runId` is saved for History.
4. **History & Compare** (`/history`) — `runApi.listRuns()`, `runApi.getRun()`,
   and `runApi.compare({ left, right })` showing ordered `changes` and bounded
   `metricDeltas`.

## Getting started

```sh
npm install
npm run dev      # http://localhost:3000
```

Start the Assay server from the repository root alongside it, e.g.:

```sh
assay -library-url https://data.example.com -cache-max-mb 5120
```

By default the dev server proxies `/api` → `http://127.0.0.1:8080`, so the
browser talks same-origin and streaming works without CORS. Point the proxy
elsewhere with `ASSAY_SERVER_URL`, or bypass it entirely by setting
`VITE_ASSAY_BASE_URL` to the server URL (see `.env.example`).

## The generated contract

The ConnectRPC client is generated into `src/gen/assay/v1/`
(`service_pb.ts`, `service_connect.ts`, `primitive_pb.ts`) straight from
`proto/` at the repository root. After a protobuf change, regenerate it with
`npm run generate-rpc` and commit the result.

Two contract details the UI handles explicitly:

- `strategy_json` / `params_json` are `bytes` → `Uint8Array`. The strategy
  document is carried as a JS object and (de)serialized in `src/lib/codec.ts`.
- `from_ms` / `to_ms` are `sint64` → `bigint`. Dates are epoch milliseconds
  throughout and are never stored as floating point.

## Constraints surfaced in the UI

- One instrument per run.
- Crypto supports M15 and above; FX is daily/weekly in the public fallback path.
- Local cache is capped at 5 GiB by default (oldest-file eviction).
- Time windows are `[from, to)` in epoch milliseconds.

## Layout

```
src/
  gen/assay/v1/     vendored ConnectRPC contract
  lib/              rpc clients, codec, constraints, session store, utils
  components/ui/    shadcn/ui primitives
  components/       app nav, page header, constraints banner, equity sparkline
  routes/           __root, index, data, strategy, run, history
```

## Scripts

```sh
npm run dev              # dev server on :3000
npm run build            # production build (client + SSR)
npm run start            # serve the production build on :3000
npm run generate-routes  # regenerate the route tree
npm run lint             # eslint
```
