package pipeline

import (
	"context"
	"fmt"
	"io"

	"github.com/Bcypher01/Assay/internal/core"
	"github.com/Bcypher01/Assay/internal/store"
	"github.com/Bcypher01/Assay/internal/venue"
)

// Reconcile fetches venue-native bars and compares them with the published
// object for each requested month.
func Reconcile(ctx context.Context, fetcher venue.Fetcher, pub Publisher, job Job) error {
	for _, sym := range job.Symbols {
		tfs := job.TFs
		if job.TFsForSymbol != nil {
			var err error
			tfs, err = job.TFsForSymbol(sym, job.TFs)
			if err != nil {
				return err
			}
		}
		for _, tf := range tfs {
			for _, month := range months(job.From, job.To) {
				key := ObjectKey(sym, tf, month.name)
				rc, ok, err := pub.Get(ctx, key)
				if err != nil {
					return err
				}
				if !ok {
					return fmt.Errorf("pipeline: reconcile missing published object %s", key)
				}
				data, err := io.ReadAll(rc)
				closeErr := rc.Close()
				if err != nil {
					return fmt.Errorf("pipeline: reading %s: %w", key, err)
				}
				if closeErr != nil {
					return fmt.Errorf("pipeline: closing %s: %w", key, closeErr)
				}
				published, err := store.DecodePublished(data, sym, tf)
				if err != nil {
					return fmt.Errorf("pipeline: decode %s: %w", key, err)
				}
				native, err := fetcher.Fetch(ctx, sym, tf, month.from, month.to)
				if err != nil {
					return fmt.Errorf("pipeline: reconciliation fetch %s: %w", key, err)
				}
				if err := equalSeries(published, native); err != nil {
					return fmt.Errorf("pipeline: reconciliation failed for %s: %w", key, err)
				}
			}
		}
	}
	return nil
}

func equalSeries(a, b *core.Series) error {
	if a.Symbol != b.Symbol || a.TF != b.TF {
		return fmt.Errorf("identity mismatch published=%s/%s native=%s/%s", a.Symbol, a.TF, b.Symbol, b.TF)
	}
	if a.Len() != b.Len() {
		return fmt.Errorf("row count mismatch published=%d native=%d", a.Len(), b.Len())
	}
	for i := 0; i < a.Len(); i++ {
		if a.Time[i] != b.Time[i] || a.Open[i] != b.Open[i] || a.High[i] != b.High[i] ||
			a.Low[i] != b.Low[i] || a.Close[i] != b.Close[i] || a.Volume[i] != b.Volume[i] {
			return fmt.Errorf("bar %d mismatch", i)
		}
	}
	return nil
}
