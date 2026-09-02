// Package curated provides the checked-in, reviewed initial event catalogue.
package curated

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/bcypher01/assay/internal/event"
	"github.com/bcypher01/assay/internal/eventsource"
)

//go:embed events.json
var catalogue []byte

// Provider is the reviewed, dependency-free Phase 1 source.
type Provider struct{}

func (Provider) Name() string { return "curated-v1" }

func (Provider) Events(ctx context.Context, scope eventsource.Scope) ([]event.Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var all []event.Event
	if err := json.Unmarshal(catalogue, &all); err != nil {
		return nil, fmt.Errorf("curated events: decode catalogue: %w", err)
	}
	for _, e := range all {
		if err := e.Validate(); err != nil {
			return nil, fmt.Errorf("curated events: %w", err)
		}
	}
	out := event.Snapshot{Events: all}.Query(event.Filter{From: scope.From, To: scope.To})
	return out, nil
}
