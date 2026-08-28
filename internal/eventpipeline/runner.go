package eventpipeline

import (
	"context"
	"fmt"

	"github.com/Bcypher01/Assay/internal/event"
	"github.com/Bcypher01/Assay/internal/eventsource"
)

// Job identifies one reproducible import request.
type Job struct {
	Scope           eventsource.Scope
	BuildVersion    string
	TaxonomyVersion string
}

// Runner moves normalised provider records into an immutable snapshot.
type Runner struct {
	Provider eventsource.Provider
	Store    *event.Store
}

func (r Runner) Run(ctx context.Context, job Job) (event.Manifest, error) {
	if r.Provider == nil {
		return event.Manifest{}, fmt.Errorf("eventpipeline: missing provider")
	}
	if r.Store == nil {
		return event.Manifest{}, fmt.Errorf("eventpipeline: missing store")
	}
	if job.BuildVersion == "" {
		return event.Manifest{}, fmt.Errorf("eventpipeline: missing build version")
	}
	if job.Scope.To != 0 && job.Scope.From != 0 && job.Scope.To <= job.Scope.From {
		return event.Manifest{}, fmt.Errorf("eventpipeline: invalid scope")
	}
	events, err := r.Provider.Events(ctx, job.Scope)
	if err != nil {
		return event.Manifest{}, fmt.Errorf("eventpipeline: fetch %s: %w", r.Provider.Name(), err)
	}
	return r.Store.Write(ctx, events, event.Manifest{Source: r.Provider.Name(), BuildVersion: job.BuildVersion, TaxonomyVersion: job.TaxonomyVersion})
}
