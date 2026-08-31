package eventsource

import (
	"context"

	"github.com/bcypher01/assay/internal/core"
	"github.com/bcypher01/assay/internal/event"
)

// Scope limits a provider request to records whose occurrence time is in
// [From, To). Zero bounds mean unbounded.
type Scope struct {
	From core.Time
	To   core.Time
}

// Provider supplies already-normalised canonical events. Raw provider payload
// handling belongs behind this boundary so the store never depends on a vendor.
type Provider interface {
	Name() string
	Events(ctx context.Context, scope Scope) ([]event.Event, error)
}
