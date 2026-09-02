// Package venue defines the data-source boundary used by the library pipeline.
package venue

import (
	"context"
	"errors"
	"fmt"

	"github.com/bcypher01/assay/internal/core"
)

// ErrUnavailable reports that a venue cannot serve a requested symbol,
// timeframe, or date range.
var ErrUnavailable = errors.New("venue: unavailable")

// Fetcher lists available history and fetches already-normalised bars.
type Fetcher interface {
	Name() string
	Available(ctx context.Context, sym string) (from, to core.Time, tfs []core.TF, err error)
	Fetch(ctx context.Context, sym string, tf core.TF, from, to core.Time) (*core.Series, error)
}

// Unsupported returns an ErrUnavailable with venue context.
func Unsupported(name, reason string) error {
	return fmt.Errorf("%w: %s: %s", ErrUnavailable, name, reason)
}
