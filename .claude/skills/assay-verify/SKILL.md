---
name: assay-verify
description: The done-gate for any change to the Assay Go code. Run before claiming work is finished, before committing, and before opening a PR - checks formatting, lint, tests, race, determinism, generated-code freshness, and hot-path allocations. Use whenever you have finished editing Go code in this repo, or when asked whether a change is ready.
---

# Verifying a change to Assay

Nothing is done until every step below is clean. Report failures with the
actual output; never describe a step you skipped as passing.

## 1. The mechanical gate

```
make lint    # gofmt (fails the build), go vet, golangci-lint
make test
make race
```

`make lint` runs `.golangci.yml`, which encodes this project's rules rather
than general taste. Two of its linters matter more than the rest:

- **forbidigo** enforces determinism: no `time.Now`, no unseeded `rand` under
  `internal/`. `cmd/`, `internal/pipeline` (a scheduled job) and
  `internal/venue` (asks the venue what exists now) are exempt by path.
  If you need a wall-clock read anywhere else, it is almost certainly a design
  error. If it genuinely is not, annotate the single line:
  `//nolint:forbidigo // <why this cannot affect run output>`.
- **errcheck** ignores best-effort `Close` on read paths, but **every `Close`
  on a write path must be checked** — an unchecked one silently loses data.

## 2. Determinism

Every batch has a determinism test: same input twice, byte-identical output. If
you touched anything under `internal/` that participates in a run, that test
must exist and must pass. A run is a pure function of
`(IR document, data snapshot, seed)`. Re-read the change asking:

- Did I `range` over a map anywhere that affects output? Sort the keys.
- Did I introduce goroutine-order dependence in result assembly? Results go
  into a preallocated indexed slice, never appended from several goroutines.
- Did I compare floats for equality? Level touches are exact integer
  comparisons on `core.Price`.

## 3. Generated code

If you touched `proto/`:

```
make proto              # rewrites gen/go
cd web && npm run generate-rpc   # rewrites web/src/gen
git diff --exit-code -- gen/go   # must be empty after make proto
```

CI fails if `gen/go` is stale. Never hand-edit a generated file.

## 4. Hot path

If the change touches `internal/core`, `internal/sim`, `internal/primitive`,
`internal/engine` or a pack:

```
make bench
```

Compare `-benchmem` against the previous numbers. An added allocation in a
per-bar or per-configuration path needs a stated reason in the commit message.

## 5. Read the change back

- Prices are `core.Price` (int64 ticks). A `float64` holding a price is a bug.
- Timestamps are `core.Time` (ms since epoch, UTC). No `time.Time` in series,
  storage or hot paths.
- Errors wrap with `%w`; sentinels live beside the package that returns them.
- No panics in library code. `cmd/` may fatal on startup.
- Did detection logic leak into the simulator, or fill logic into a primitive?
  If so the boundary is in the wrong place — fix the layering, not the symptom.
