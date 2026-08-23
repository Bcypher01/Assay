// Package binance fetches public Binance kline dumps from data.binance.vision.
package binance

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/Bcypher01/Assay/internal/core"
	"github.com/Bcypher01/Assay/internal/instrument"
	"github.com/Bcypher01/Assay/internal/venue"
)

const defaultBaseURL = "https://data.binance.vision"

// Client fetches anonymous spot kline dumps.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
	Registry   *instrument.Registry
}

var _ venue.Fetcher = (*Client)(nil)

func (c *Client) Name() string { return "binance-public-data" }

// Available reports the coarse public-data range Binance publishes for klines.
// Exact symbol probing happens during Fetch so this method remains offline
// friendly and cheap.
func (c *Client) Available(ctx context.Context, sym string) (core.Time, core.Time, []core.TF, error) {
	if err := ctx.Err(); err != nil {
		return 0, 0, nil, err
	}
	if _, err := c.spec(sym); err != nil {
		return 0, 0, nil, err
	}
	from := core.FromStd(time.Date(2017, 8, 1, 0, 0, 0, 0, time.UTC))
	to := core.FromStd(time.Now().UTC().AddDate(0, 0, 1))
	return from, to, []core.TF{core.TFM1, core.TFM5, core.TFM15, core.TFM30, core.TFH1, core.TFH4, core.TFD1}, nil
}

// Fetch downloads monthly kline ZIPs that overlap [from,to), falling back to
// daily ZIPs for months whose archive is not available yet. It normalises
// decimal prices into instrument ticks and rejects malformed ordering.
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
		return nil, venue.Unsupported(c.Name(), "timeframe "+tf.String())
	}

	out := core.NewSeries(sym, tf, 0)
	for _, month := range monthNames(from, to) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		market := marketSymbol(sym)
		body, err := c.get(ctx, c.monthlyURL(market, interval, month))
		if err == nil {
			if err := appendZipCSV(out, spec, body, from, to); err != nil {
				return nil, fmt.Errorf("binance: %s %s: %w", sym, month, err)
			}
			continue
		}
		if !errors.Is(err, venue.ErrUnavailable) {
			return nil, err
		}
		for _, day := range dayNames(month, from, to) {
			body, err := c.get(ctx, c.dailyURL(market, interval, day))
			if errors.Is(err, venue.ErrUnavailable) {
				continue
			}
			if err != nil {
				return nil, err
			}
			if err := appendZipCSV(out, spec, body, from, to); err != nil {
				return nil, fmt.Errorf("binance: %s %s: %w", sym, day, err)
			}
		}
	}
	if err := out.Validate(); err != nil {
		return nil, fmt.Errorf("binance: normalised invalid series: %w", err)
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
	if spec.Venue != "binance" {
		return nil, venue.Unsupported(c.Name(), "symbol "+sym+" is not a Binance instrument")
	}
	return spec, nil
}

func (c *Client) client() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

func (c *Client) base() string {
	if c.BaseURL != "" {
		return strings.TrimRight(c.BaseURL, "/")
	}
	return defaultBaseURL
}

func (c *Client) monthlyURL(sym, interval, month string) string {
	u, _ := url.Parse(c.base())
	u.Path = path.Join(u.Path, "data/spot/monthly/klines", sym, interval, sym+"-"+interval+"-"+month+".zip")
	return u.String()
}

func (c *Client) dailyURL(sym, interval, day string) string {
	u, _ := url.Parse(c.base())
	u.Path = path.Join(u.Path, "data/spot/daily/klines", sym, interval, sym+"-"+interval+"-"+day+".zip")
	return u.String()
}

func marketSymbol(sym string) string {
	if _, rest, ok := strings.Cut(sym, ":"); ok {
		return rest
	}
	return sym
}

func (c *Client) get(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	res, err := c.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("binance: fetch %s: %w", rawURL, err)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%w: %s", venue.ErrUnavailable, rawURL)
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("binance: fetch %s: status %s", rawURL, res.Status)
	}
	return io.ReadAll(res.Body)
}

func appendZipCSV(dst *core.Series, spec *instrument.Spec, data []byte, from, to core.Time) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("opening zip: %w", err)
	}
	if len(zr.File) != 1 {
		return fmt.Errorf("zip contains %d files, want 1", len(zr.File))
	}
	rc, err := zr.File[0].Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	r := csv.NewReader(rc)
	r.FieldsPerRecord = -1
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if len(rec) < 6 {
			return fmt.Errorf("kline row has %d fields", len(rec))
		}
		t, err := parseMillis(rec[0])
		if err != nil {
			if strings.EqualFold(rec[0], "open_time") {
				continue
			}
			return err
		}
		if t < from || t >= to {
			continue
		}
		o, err := spec.PriceFromDecimal(rec[1])
		if err != nil {
			return err
		}
		h, err := spec.PriceFromDecimal(rec[2])
		if err != nil {
			return err
		}
		l, err := spec.PriceFromDecimal(rec[3])
		if err != nil {
			return err
		}
		c, err := spec.PriceFromDecimal(rec[4])
		if err != nil {
			return err
		}
		v, err := parseVolume(rec[5])
		if err != nil {
			return err
		}
		if dst.Len() > 0 && t <= dst.Time[dst.Len()-1] {
			return fmt.Errorf("duplicate or out-of-order bar at %d", t)
		}
		dst.Append(t, o, h, l, c, v)
	}
	return nil
}

func parseMillis(s string) (core.Time, error) {
	v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("open time %q: %w", s, err)
	}
	return core.Time(v), nil
}

func parseVolume(s string) (int64, error) {
	t := strings.TrimSpace(s)
	if t == "" {
		return 0, nil
	}
	parts := strings.SplitN(t, ".", 2)
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("volume %q: %w", s, err)
	}
	return whole, nil
}

func interval(tf core.TF) (string, bool) {
	switch tf {
	case core.TFM1:
		return "1m", true
	case core.TFM5:
		return "5m", true
	case core.TFM15:
		return "15m", true
	case core.TFM30:
		return "30m", true
	case core.TFH1:
		return "1h", true
	case core.TFH4:
		return "4h", true
	case core.TFD1:
		return "1d", true
	case core.TFW1:
		return "1w", true
	default:
		return "", false
	}
}

func monthNames(from, to core.Time) []string {
	start := firstOfMonth(from.Std())
	end := firstOfMonth((to - 1).Std())
	var out []string
	for cur := start; !cur.After(end); cur = cur.AddDate(0, 1, 0) {
		out = append(out, cur.Format("2006-01"))
	}
	return out
}

func dayNames(month string, from, to core.Time) []string {
	start, err := time.Parse("2006-01", month)
	if err != nil {
		return nil
	}
	end := start.AddDate(0, 1, 0)
	if core.FromStd(start) < from {
		start = from.Std()
	}
	if core.FromStd(end) > to {
		end = to.Std()
	}
	start = time.Date(start.UTC().Year(), start.UTC().Month(), start.UTC().Day(), 0, 0, 0, 0, time.UTC)
	var out []string
	for cur := start; cur.Before(end); cur = cur.AddDate(0, 0, 1) {
		out = append(out, cur.Format("2006-01-02"))
	}
	return out
}

func firstOfMonth(t time.Time) time.Time {
	y, m, _ := t.UTC().Date()
	return time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
}
