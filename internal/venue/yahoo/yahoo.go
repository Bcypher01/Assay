// Package yahoo fetches free daily and weekly FX candles from Yahoo Finance's
// chart endpoint.
package yahoo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Bcypher01/Assay/internal/core"
	"github.com/Bcypher01/Assay/internal/instrument"
	"github.com/Bcypher01/Assay/internal/venue"
)

const defaultBaseURL = "https://query1.finance.yahoo.com/v8/finance/chart/"

// Client fetches Yahoo Finance daily or weekly candles.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
	Registry   *instrument.Registry
}

var _ venue.Fetcher = (*Client)(nil)

func (c *Client) Name() string { return "yahoo-fx" }

func (c *Client) Available(ctx context.Context, sym string) (core.Time, core.Time, []core.TF, error) {
	if err := ctx.Err(); err != nil {
		return 0, 0, nil, err
	}
	if _, err := c.spec(sym); err != nil {
		return 0, 0, nil, err
	}
	from := core.FromStd(time.Date(1971, 1, 1, 0, 0, 0, 0, time.UTC))
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
	body, err := c.get(ctx, c.url(yahooSymbol(sym), interval, from, to))
	if err != nil {
		return nil, err
	}
	out, err := decodeChart(sym, tf, spec, body, from, to)
	if err != nil {
		return nil, err
	}
	if err := out.Validate(); err != nil {
		return nil, fmt.Errorf("yahoo: normalised invalid series: %w", err)
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
	if spec.Class != instrument.ClassFX && spec.Class != instrument.ClassMetal {
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
	base := c.base()
	u, _ := url.Parse(base + url.PathEscape(sym))
	q := u.Query()
	q.Set("period1", strconv.FormatInt(from.Std().Unix(), 10))
	q.Set("period2", strconv.FormatInt(to.Std().Unix(), 10))
	q.Set("interval", interval)
	q.Set("events", "history")
	u.RawQuery = q.Encode()
	return u.String()
}

func (c *Client) base() string {
	if c.BaseURL != "" {
		return strings.TrimRight(c.BaseURL, "/") + "/"
	}
	return defaultBaseURL
}

func (c *Client) get(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 Assay/1.0")
	res, err := c.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("yahoo: fetch %s: %w", rawURL, err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("yahoo: fetch %s: status %s: %s", rawURL, res.Status, strings.TrimSpace(string(body)))
	}
	return body, nil
}

type chartResponse struct {
	Chart struct {
		Result []struct {
			Timestamp  []int64 `json:"timestamp"`
			Indicators struct {
				Quote []struct {
					Open   []*float64 `json:"open"`
					High   []*float64 `json:"high"`
					Low    []*float64 `json:"low"`
					Close  []*float64 `json:"close"`
					Volume []*int64   `json:"volume"`
				} `json:"quote"`
			} `json:"indicators"`
		} `json:"result"`
		Error any `json:"error"`
	} `json:"chart"`
}

func decodeChart(symbol string, tf core.TF, spec *instrument.Spec, data []byte, from, to core.Time) (*core.Series, error) {
	var cr chartResponse
	if err := json.Unmarshal(data, &cr); err != nil {
		return nil, fmt.Errorf("yahoo: parsing chart JSON: %w", err)
	}
	if cr.Chart.Error != nil {
		return nil, fmt.Errorf("%w: yahoo chart error: %v", venue.ErrUnavailable, cr.Chart.Error)
	}
	if len(cr.Chart.Result) == 0 || len(cr.Chart.Result[0].Indicators.Quote) == 0 {
		return nil, fmt.Errorf("%w: yahoo returned no chart data", venue.ErrUnavailable)
	}
	res := cr.Chart.Result[0]
	q := res.Indicators.Quote[0]
	out := core.NewSeries(symbol, tf, len(res.Timestamp))
	for i, unix := range res.Timestamp {
		if i >= len(q.Open) || i >= len(q.High) || i >= len(q.Low) || i >= len(q.Close) {
			return nil, fmt.Errorf("yahoo: quote columns shorter than timestamps")
		}
		if q.Open[i] == nil || q.High[i] == nil || q.Low[i] == nil || q.Close[i] == nil {
			continue
		}
		t := tf.AlignDown(core.FromStd(time.Unix(unix, 0).UTC()))
		if t < from || t >= to {
			continue
		}
		o, err := price(spec, *q.Open[i])
		if err != nil {
			return nil, err
		}
		h, err := price(spec, *q.High[i])
		if err != nil {
			return nil, err
		}
		l, err := price(spec, *q.Low[i])
		if err != nil {
			return nil, err
		}
		c, err := price(spec, *q.Close[i])
		if err != nil {
			return nil, err
		}
		// Yahoo FX occasionally reports high/low that do not bracket the
		// open/close. Widening the range preserves the reported open/close and
		// is conservative for stop/target ambiguity.
		if h < o {
			h = o
		}
		if h < c {
			h = c
		}
		if l > o {
			l = o
		}
		if l > c {
			l = c
		}
		v := int64(0)
		if i < len(q.Volume) && q.Volume[i] != nil {
			v = *q.Volume[i]
		}
		if out.Len() > 0 {
			last := out.Len() - 1
			if t < out.Time[last] {
				return nil, fmt.Errorf("yahoo: out-of-order bar at %d", t)
			}
			if t == out.Time[last] {
				// Yahoo can return more than one observation that aligns to the
				// same UTC week, particularly around month boundaries and DST.
				// Fold them into one candle instead of rejecting otherwise valid
				// history. The first open and latest close preserve chronology.
				out.High[last] = core.Max(out.High[last], h)
				out.Low[last] = core.Min(out.Low[last], l)
				out.Close[last] = c
				out.Volume[last] += v
				continue
			}
		}
		out.Append(t, o, h, l, c, v)
	}
	return out, nil
}

func price(spec *instrument.Spec, v float64) (core.Price, error) {
	digits := spec.PriceDigits()
	scale := math.Pow10(digits)
	return spec.PriceFromDecimal(strconv.FormatFloat(math.Round(v*scale)/scale, 'f', digits, 64))
}

func interval(tf core.TF) (string, bool) {
	switch tf {
	case core.TFD1:
		return "1d", true
	case core.TFW1:
		return "1wk", true
	default:
		return "", false
	}
}

func yahooSymbol(sym string) string {
	_, rest, ok := strings.Cut(sym, ":")
	if ok {
		sym = rest
	}
	return strings.ToUpper(sym) + "=X"
}
