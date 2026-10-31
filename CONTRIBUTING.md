# Contributing to Assay

Assay is a local-first backtester whose numbers are meant to be defensible.
Contributions are welcome, but changes to detection, fills, analytics or
persistence need tests that show the behaviour is deterministic and does not
introduce look-ahead.

## Before you start

Read, in this order:

1. `CLAUDE.md` — working agreement and non-negotiables
2. `docs/architecture.md` — package boundaries and execution model
3. `docs/conventions.md` — Go rules
4. The batch document for the area you are changing in `docs/batches/`

If a change crosses a layer boundary, open an issue first. The pipeline is
deliberately strict: primitives detect, the simulator fills, the engine wires
them together, and analytics orchestrates runs.

## Development setup

Requirements:

- Go 1.24 or newer
- Node.js 22 and npm
- `protoc` with `protoc-gen-es` and `protoc-gen-connect-es` for regenerating
  the TypeScript stubs
- `buf` for regenerating the Go stubs

```sh
make tools          # golangci-lint and buf
npm --prefix web ci
go mod download
```

Run the server and the UI in separate terminals:

```sh
make build
./bin/assay
npm --prefix web run dev
```

The Vite dev server proxies `/api` to `http://127.0.0.1:8080`.

## Checks

Every pull request must pass:

```sh
make test
make race
make lint
npm --prefix web run lint
npm --prefix web run check
npm --prefix web run build
```

If you change `proto/`, run `make proto` and `npm --prefix web run generate-rpc`
and commit the generated output. Never edit `gen/go` or `web/src/gen` by hand.

## Commit sign-off

This project uses the [Developer Certificate of Origin](https://developercertificate.org/).
Sign commits with `git commit -s`. By signing off you confirm that you have the
right to contribute the change under the project's Apache-2.0 licence.

## Pull requests

- Keep a pull request focused on one change.
- Explain the user-visible effect and the test that proves it.
- Add or update golden fixtures when structured output changes.
- Do not include market data, credentials, local caches, or generated build
  artefacts.
- Financial claims need evidence. Do not describe a backtest as a forecast or
  investment advice.
