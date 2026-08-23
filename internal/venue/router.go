package venue

import (
	"context"
	"fmt"

	"github.com/Bcypher01/Assay/internal/core"
	"github.com/Bcypher01/Assay/internal/instrument"
)

// Router dispatches fetches by the instrument registry's Venue field.
type Router struct {
	Registry *instrument.Registry
	Fetchers map[string]Fetcher
}

var _ Fetcher = (*Router)(nil)

func (r *Router) Name() string { return "venue-router" }

// Source returns the concrete fetcher name that will serve sym.
func (r *Router) Source(sym string) (string, error) {
	f, err := r.fetcher(sym)
	if err != nil {
		return "", err
	}
	return f.Name(), nil
}

func (r *Router) Available(ctx context.Context, sym string) (core.Time, core.Time, []core.TF, error) {
	f, err := r.fetcher(sym)
	if err != nil {
		return 0, 0, nil, err
	}
	return f.Available(ctx, sym)
}

func (r *Router) Fetch(ctx context.Context, sym string, tf core.TF, from, to core.Time) (*core.Series, error) {
	f, err := r.fetcher(sym)
	if err != nil {
		return nil, err
	}
	return f.Fetch(ctx, sym, tf, from, to)
}

func (r *Router) fetcher(sym string) (Fetcher, error) {
	reg := r.Registry
	if reg == nil {
		var err error
		reg, err = instrument.Default()
		if err != nil {
			return nil, err
		}
	}
	spec, err := reg.Get(sym)
	if err != nil {
		return nil, err
	}
	f, ok := r.Fetchers[spec.Venue]
	if !ok || f == nil {
		return nil, Unsupported("router", fmt.Sprintf("no fetcher for venue %q", spec.Venue))
	}
	return f, nil
}
