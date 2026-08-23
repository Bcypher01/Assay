// Package dukascopy documents the FX venue boundary.
//
// Dukascopy is intentionally not used by the publishing pipeline: the public
// feed is suitable for client-side fallback, while republishing derived FX bars
// remains outside the current batch's allowed publish path.
package dukascopy

import (
	"context"

	"github.com/Bcypher01/Assay/internal/core"
	"github.com/Bcypher01/Assay/internal/venue"
)

// Client is a disabled pipeline fetcher for Dukascopy public data.
type Client struct{}

var _ venue.Fetcher = (*Client)(nil)

func (c *Client) Name() string { return "dukascopy" }

func (c *Client) Available(ctx context.Context, sym string) (core.Time, core.Time, []core.TF, error) {
	if err := ctx.Err(); err != nil {
		return 0, 0, nil, err
	}
	return 0, 0, nil, venue.Unsupported(c.Name(), "FX data is client-side fallback only until redistribution rights are cleared")
}

func (c *Client) Fetch(ctx context.Context, sym string, tf core.TF, from, to core.Time) (*core.Series, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, venue.Unsupported(c.Name(), "FX data is client-side fallback only until redistribution rights are cleared")
}
