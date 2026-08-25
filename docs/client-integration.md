# Client integration

The browser calls the local Assay server through ConnectRPC; it never receives
R2 credentials or reads the bar library directly. Run `assay` with a public,
read-only R2 custom-domain/CDN URL:

```sh
assay -library-url https://data.example.com -cache-dir cache -cache-max-mb 5120 -history-dir history
```

`DataStatus` takes a symbol, timeframe, and `[from_ms, to_ms)` window and
returns the cache-only readiness for that exact request, including missing
partitions and the snapshot hash when local data is ready. Call it before
enabling Strategy or Run workflow steps.

`EnsureData` takes the same request and streams resolution, download, and
verification progress as the server fills the local month-partitioned Parquet
cache. The cache uses a 5 GiB default oldest-file eviction budget; set
`-cache-max-mb 0` only when an unbounded cache is intentional. `Run` does not
download data. It performs a cache-only preflight and returns `DATA_MISSING` or
`DATA_PARTIAL` in `RunSummary.error_code` when the exact local dataset is not
ready.

Run responses contain `run_id`. Use it with `RunService.GetRun`; use two IDs
with `RunService.Compare`, whose response contains ordered parameter changes and
bounded metric deltas. The TypeScript client is generated under
`gen/typescript/src`; run `npm run generate` after a protobuf change.

Interactive API documentation is available from the local server at `/docs`.
The corresponding OpenAPI 3.1 document is `/openapi.json`. Streaming endpoints
are documented there but should be called through the generated ConnectRPC
client, which handles the streaming protocol.
