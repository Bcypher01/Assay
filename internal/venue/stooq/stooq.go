// Package stooq fetches free daily and weekly FX candles from Stooq CSV.
package stooq

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Bcypher01/Assay/internal/core"
	"github.com/Bcypher01/Assay/internal/instrument"
	"github.com/Bcypher01/Assay/internal/venue"
)

const defaultBaseURL = "https://stooq.com/q/d/l/"

// Client fetches Stooq daily or weekly CSV candles.
type Client struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
	Registry   *instrument.Registry
}

var _ venue.Fetcher = (*Client)(nil)

func (c *Client) Name() string { return "stooq-fx" }

func (c *Client) Available(ctx context.Context, sym string) (core.Time, core.Time, []core.TF, error) {
	if err := ctx.Err(); err != nil {
		return 0, 0, nil, err
	}
	if _, err := c.spec(sym); err != nil {
		return 0, 0, nil, err
	}
	from := core.FromStd(time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC))
	to := core.FromStd(time.Now().UTC().AddDate(0, 0, 1))
	return from, to, []core.TF{core.TFD1, core.TFW1}, nil
}

func (c *Client) Fetch(ctx context.Context, sym string, tf core.TF, from, to core.Time) (*core.Series, error) {
	if to <= from {
		return core.NewSeries(sym, tf, 0), nil
	}
	spec, err := c.spec(sym)
	if err != nil {
		return nil, err
	}
	interval, ok := interval(tf)
	if !ok {
		return nil, venue.Unsupported(c.Name(), "FX hosted mode supports d1 and w1 only")
	}
	body, err := c.get(ctx, c.url(stooqSymbol(sym), interval, from, to))
	if err != nil {
		return nil, err
	}
	out, err := decodeCSV(sym, tf, spec, body, from, to)
	if err != nil {
		return nil, err
	}
	if err := out.Validate(); err != nil {
		return nil, fmt.Errorf("stooq: normalised invalid series: %w", err)
	}
	return out, nil
}

func (c *Client) spec(sym string) (*instrument.Spec, error) {
	r := c.Registry
	if r == nil {
		var err error
		r, err = instrument.Default()
		if err != nil {
			return nil, err
		}
	}
	spec, err := r.Get(sym)
	if err != nil {
		return nil, err
	}
	if spec.Venue != "dukascopy" {
		return nil, venue.Unsupported(c.Name(), "symbol "+sym+" is not an FX instrument")
	}
	return spec, nil
}

func (c *Client) client() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

func (c *Client) url(sym, interval string, from, to core.Time) string {
	u, _ := url.Parse(c.base())
	q := u.Query()
	q.Set("s", sym)
	q.Set("d1", from.Std().Format("20060102"))
	q.Set("d2", (to - 1).Std().Format("20060102"))
	q.Set("i", interval)
	if c.APIKey != "" {
		q.Set("apikey", c.APIKey)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func (c *Client) base() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return defaultBaseURL
}

func (c *Client) get(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	res, err := c.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("stooq: fetch %s: %w", rawURL, err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("stooq: fetch %s: status %s", rawURL, res.Status)
	}
	first := strings.ToLower(strings.TrimSpace(firstLine(body)))
	if first == "" || strings.Contains(first, "no data") || strings.Contains(first, "error") {
		return nil, fmt.Errorf("%w: stooq returned %q", venue.ErrUnavailable, first)
	}
	if strings.Contains(first, "<!doctype html") || strings.Contains(first, "<html") ||
		strings.Contains(strings.ToLower(string(body)), "requires javascript") {
		return nil, fmt.Errorf("%w: stooq returned browser verification instead of CSV; this endpoint is not usable from the backend fetcher", venue.ErrUnavailable)
	}
	return body, nil
}

func decodeCSV(symbol string, tf core.TF, spec *instrument.Spec, data []byte, from, to core.Time) (*core.Series, error) {
	r := csv.NewReader(strings.NewReader(string(data)))
	r.FieldsPerRecord = -1
	header, err := r.Read()
	if err != nil {
		return nil, err
	}
	if len(header) < 5 || !strings.EqualFold(header[0], "date") {
		return nil, fmt.Errorf("stooq: unexpected header %v", header)
	}
	out := core.NewSeries(symbol, tf, 0)
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(rec) < 5 {
			return nil, fmt.Errorf("stooq: row has %d fields", len(rec))
		}
		t, err := parseDate(rec[0])
		if err != nil {
			return nil, err
		}
		if t < from || t >= to {
			continue
		}
		o, err := spec.PriceFromDecimal(rec[1])
		if err != nil {
			return nil, err
		}
		h, err := spec.PriceFromDecimal(rec[2])
		if err != nil {
			return nil, err
		}
		l, err := spec.PriceFromDecimal(rec[3])
		if err != nil {
			return nil, err
		}
		c, err := spec.PriceFromDecimal(rec[4])
		if err != nil {
			return nil, err
		}
		v := int64(0)
		if len(rec) > 5 {
			v, _ = strconv.ParseInt(strings.TrimSpace(rec[5]), 10, 64)
		}
		if out.Len() > 0 && t <= out.Time[out.Len()-1] {
			return nil, fmt.Errorf("stooq: duplicate or out-of-order bar at %d", t)
		}
		out.Append(t, o, h, l, c, v)
	}
	return out, nil
}

func interval(tf core.TF) (string, bool) {
	switch tf {
	case core.TFD1:
		return "d", true
	case core.TFW1:
		return "w", true
	default:
		return "", false
	}
}

func stooqSymbol(sym string) string {
	_, rest, ok := strings.Cut(sym, ":")
	if ok {
		sym = rest
	}
	return strings.ToLower(sym)
}

func parseDate(s string) (core.Time, error) {
	t, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(s), time.UTC)
	if err != nil {
		return 0, err
	}
	return core.FromStd(t), nil
}

func firstLine(data []byte) string {
	s := string(data)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
