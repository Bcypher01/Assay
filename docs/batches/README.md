# Batches

Work is broken into batches. Each has a spec in this folder covering goal,
dependencies, deliverables, key decisions, interfaces, tests and exit criteria.

A batch is done when its exit criteria pass, `make test`, `make race` and
`make lint` are clean, and its tests are written. Not before.

## Dependency graph

```
00 core types
 ├── 01 bar storage ──┬── 02 venue pipeline
 │                    └── 03 library client
 ├── 04 trade simulator ──┐
 └── 05 primitive registry┼── 07 IR + engine ──┬── 08 transport
      ├── 06 base packs   │                    └── 10 analytics ── 11 run store
      └── 09 SMC pack ────┘
                                                    12 packaging (last)
```

## Order

| # | Batch | Why here |
|---|---|---|
| 00 | Core types | Everything depends on fixed-point prices and the series layout. Changing them later touches every file. |
| 01 | Bar storage | Defines the on-disk and on-wire format that both binaries share. |
| 02 | Venue pipeline | Fills the library. Can proceed in parallel with 03 once 01 lands. |
| 03 | Library client | Cold-start path. Proves the 10-second target early. |
| 04 | Trade simulator | Built against synthetic signals, before any detector exists, so correctness-critical logic is proven in isolation. |
| 05 | Primitive registry | The memo cache must exist from the start; retrofitting a cache key onto stateful primitives is unpleasant. |
| 06 | Base packs | Cheap. Proves the registry with real detectors. |
| 07 | IR + engine | Wires 04, 05 and 06 into an end-to-end run. |
| 08 | Transport | Thin once the core is settled. Building it earlier means rewriting it. |
| 09 | SMC pack | The differentiating library, and the one needing outside validation. |
| 10 | Analytics | The reason the product exists. Needs 07 stable underneath it. |
| 11 | Run store | Trial counting and comparison, which analytics makes necessary. |
| 12 | Packaging | Last, because everything it packages must exist. |

## Doc template

New batches follow the same shape: Goal, Depends on / Blocks, Packages,
Deliverables, Key decisions, Interfaces, Tests, Exit criteria, Out of scope,
Decision log.

The decision log is where a dependency added, a convention broken, or a spec
question answered gets recorded. Keep it in the batch doc, not in commit
messages.
