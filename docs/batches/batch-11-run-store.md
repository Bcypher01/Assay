# Batch 11 — Run store, history and comparison

**Depends on:** 10
**Blocks:** 12
**Packages:** `internal/runstore`

## Goal

Persist every study so trial counting is real rather than a session counter, and
so a user can see what they have already tried and what each change did. The
content-hashed IR makes this close to mechanical.

## Deliverables

- Local store: study documents, result summaries, trade lists, keyed by IR hash
  plus data snapshot hash
- Trial counter: distinct configurations tested per `(instrument set, period)`,
  surviving restarts — this is what makes the multiple-testing disclosure honest
- History listing: sortable by date, metric, instrument, with the strategy name
  and a short parameter summary
- Comparison: diff two runs, showing which parameters changed and the resulting
  delta per metric
- Near-miss storage from batch 07, so the "why so few trades" view survives the
  session
- Retention: bounded on-disk size with an eviction policy that never evicts a
  study still referenced by trial counting
- Export: a study and its results as a portable file, for sharing or for the
  Python research layer

## Key decisions

**Keyed by content hash, not by a generated ID.** Re-running an identical
configuration must be recognised as the same trial, not counted twice — otherwise
the deflation in batch 10 is wrong in the user's favour, which is the exact
failure mode the product exists to prevent.

**Trial counting is scoped to instrument set and period, not global.** Testing 40
configurations on EURUSD says nothing about a first attempt on gold. Scoping it
wrongly makes the warning either meaningless or insulting.

**Full trade lists persisted, not just summaries.** The trade inspector and the
comparison view both need them, and regenerating means re-running.

**Eviction never breaks the counter.** Summaries and hashes are retained even
when trade lists are evicted. Losing the count would quietly reset a user's
data-mining disclosure to zero.

**Storage is files, not a database.** Parquet for trade lists, JSON for
documents and summaries, an index file for listing. No service, no schema
migrations, consistent with the rest of the project.

## Interfaces

```go
type Store interface {
    Put(ctx context.Context, s *analytics.Study, r *analytics.StudyResult) (RunID, error)
    Get(ctx context.Context, id RunID) (*Run, error)
    List(ctx context.Context, f Filter) ([]RunSummary, error)
    Trials(ctx context.Context, scope Scope) (int, error)
    Compare(ctx context.Context, a, b RunID) (Diff, error)
    Export(ctx context.Context, id RunID, w io.Writer) error
}

type Diff struct {
    ParamChanges []ParamChange              // path, from, to
    MetricDeltas map[string]analytics.Bounded
}
```

## Tests

- Identical configuration stored twice counts as one trial
- A changed parameter counts as a new trial; a reordered but equivalent document
  does not
- Trial count survives a restart
- Eviction removes trade lists but preserves counts and summaries
- Comparison reports exactly the parameters that changed, with correct deltas
- Export round-trips through import without loss

## Exit criteria

- [ ] Trial count is stable across restarts and matches a hand-counted fixture
- [ ] Listing 1,000 stored runs returns in under 100ms
- [ ] Eviction under a size bound never reduces the trial count
- [ ] Diff output is deterministic and ordered

## Out of scope

Sharing between users. Cloud sync. The comparison UI.

## Decision log

_Record the on-disk layout, retention default, and the trial-scope definition._
