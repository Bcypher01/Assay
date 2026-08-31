// Package dukascopy fetches broker-native bid candles from Dukascopy's public
// historical data feed. The feed needs no API key and exposes LZMA-compressed
// BI5 artifacts.
package dukascopy

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ulikunitz/xz/lzma"

	"github.com/bcypher01/assay/internal/core"
	"github.com/bcypher01/assay/internal/instrument"
	"github.com/bcypher01/assay/internal/venue"
)

const (
	defaultBaseURL = "https://datafeed.dukascopy.com/datafeed"
	workers        = 4
	bi5RecordSize  = 24
)

var defaultHTTPClient = &http.Client{Timeout: 30 * time.Second}

// Client downloads Dukascopy BID candles. Intraday timeframes are aggregated
// from native one-minute artifacts, and completed daily history uses annual
// files. Current-year daily bars also aggregate from M1 because the annual
// artifact is not published incrementally.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
	Registry   *instrument.Registry

	mu            sync.Mutex
	artifactCache map[string]artifactEntry
	retryDelay    func(attempt int, retryAfter string) time.Duration
}

var _ venue.Fetcher = (*Client)(nil)

func (c *Client) Name() string { return "dukascopy-bid" }

func (c *Client) Available(ctx context.Context, sym string) (core.Time, core.Time, []core.TF, error) {
	if err := ctx.Err(); err != nil {
		return 0, 0, nil, err
	}
	if _, err := c.spec(sym); err != nil {
		return 0, 0, nil, err
	}
	return core.FromStd(time.Date(2003, 1, 1, 0, 0, 0, 0, time.UTC)),
		core.FromStd(time.Now().UTC().AddDate(0, 0, 1)), supportedTFs(), nil
}

func (c *Client) Fetch(ctx context.Context, sym string, tf core.TF, from, to core.Time) (*core.Series, error) {
	if to <= from {
		return core.NewSeries(sym, tf, 0), nil
	}
	_, err := c.spec(sym)
	if err != nil {
		return nil, err
	}
	if !supported(tf) {
		return nil, venue.Unsupported(c.Name(), "timeframe "+tf.String())
	}

	rawFrom := tf.AlignDown(from)
	rawTo := tf.Next(to - 1)
	var raw *core.Series
	if tf == core.TFD1 || tf == core.TFW1 {
		// Dukascopy emits a separate Sunday daily session. Include the prior
		// day so a request beginning on Monday can fold that session into the
		// conventional five-day Monday candle.
		rawFrom -= core.Time(core.TFD1.Millis())
		if from.Std().UTC().Year() == time.Now().UTC().Year() {
			// The current year's annual D1 artifact is not consistently
			// published. Go straight to incremental monthly H1 data instead of
			// waiting through failures that will ultimately need this fallback.
			raw, err = c.fetchMinutes(ctx, sym, rawFrom, rawTo)
			if err == nil {
				raw = aggregate(raw, core.TFD1)
			}
		} else {
			raw, err = c.fetchDaily(ctx, sym, rawFrom, rawTo)
		}
		if err == nil && raw.Slice(from, to).Len() == 0 {
			// Annual D1 artifacts can lag or be absent. Daily M1 files are
			// published incrementally and are the verified fallback.
			raw, err = c.fetchMinutes(ctx, sym, rawFrom, rawTo)
			if err == nil {
				raw = aggregate(raw, core.TFD1)
			}
		}
	} else {
		// Every other timeframe is deterministically aggregated from M1.
		raw, err = c.fetchMinutes(ctx, sym, rawFrom, rawTo)
	}
	if err != nil {
		return nil, err
	}
	if raw.TF == core.TFD1 {
		raw = foldSundayIntoMonday(raw)
	}
	if raw.TF == tf {
		return raw.Slice(from, to), nil
	}
	out := aggregate(raw, tf)
	if err := out.Validate(); err != nil {
		return nil, fmt.Errorf("dukascopy: normalised invalid series: %w", err)
	}
	return out.Slice(from, to), nil
}

func (c *Client) fetchDaily(ctx context.Context, sym string, from, to core.Time) (*core.Series, error) {
	startYear := from.Std().UTC().Year()
	endYear := (to - 1).Std().UTC().Year()
	items := make([]artifact, 0, endYear-startYear+1)
	for year := startYear; year <= endYear; year++ {
		base := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
		var ttl time.Duration
		if year == time.Now().UTC().Year() {
			ttl = time.Minute
		}
		items = append(items, artifact{
			url:  fmt.Sprintf("%s/%s/%04d/BID_candles_day_1.bi5", c.base(), marketSymbol(sym), year),
			base: base,
			ttl:  ttl,
		})
	}
	return c.fetchArtifacts(ctx, sym, core.TFD1, items, from, to)
}

func (c *Client) fetchMinutes(ctx context.Context, sym string, from, to core.Time) (*core.Series, error) {
	start := core.TFD1.AlignDown(from).Std().UTC()
	end := core.TFD1.Next(to - 1).Std().UTC()
	items := make([]artifact, 0, int(end.Sub(start)/(24*time.Hour)))
	today := core.TFD1.AlignDown(core.FromStd(time.Now().UTC())).Std()
	for day := start; day.Before(end); day = day.AddDate(0, 0, 1) {
		// Dukascopy's FX/CFD feed has no Saturday session. Most absent
		// Saturday artifacts answer 404, but the upstream hosts occasionally
		// answer 503 instead. Treating that closed-market date as required made
		// one nonexistent file fail an otherwise complete calendar month.
		if day.Weekday() == time.Saturday {
			continue
		}
		var ttl time.Duration
		if day.Equal(today) {
			ttl = 10 * time.Second
		}
		// Dukascopy's artifact path uses a zero-based month.
		items = append(items, artifact{
			url: fmt.Sprintf("%s/%s/%04d/%02d/%02d/BID_candles_min_1.bi5",
				c.base(), marketSymbol(sym), day.Year(), int(day.Month())-1, day.Day()),
			base: day,
			ttl:  ttl,
		})
	}
	return c.fetchArtifacts(ctx, sym, core.TFM1, items, from, to)
}

type artifact struct {
	url  string
	base time.Time
	ttl  time.Duration
}

type artifactEntry struct {
	body    []byte
	expires time.Time
}

func (c *Client) fetchArtifacts(ctx context.Context, sym string, tf core.TF, items []artifact, from, to core.Time) (*core.Series, error) {
	parts := make([]*core.Series, len(items))
	errs := make([]error, len(items))
	jobs := make(chan int)
	var wg sync.WaitGroup
	fetch := func(i int) {
		body, err := c.download(ctx, items[i].url, items[i].ttl)
		if err == nil {
			parts[i], err = decodeBI5(sym, tf, body, items[i].base, from, to)
		}
		errs[i] = err
	}
	workerCount := min(workers, len(items))
	for range workerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				fetch(i)
			}
		}()
	}
	for i := range items {
		select {
		case jobs <- i:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return nil, ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// A transient failure on one day must not cancel successful sibling
	// downloads or surface another worker's context-canceled error. Retry only
	// the failed artifacts, sequentially, so the upstream gets a quiet recovery
	// pass before the month is rejected.
	for i, err := range errs {
		if err == nil {
			continue
		}
		fetch(i)
		if errs[i] != nil {
			return nil, fmt.Errorf("dukascopy: %s: %w", items[i].base.Format("2006-01-02"), errs[i])
		}
	}

	out := core.NewSeries(sym, tf, 0)
	for _, part := range parts {
		if part == nil {
			continue
		}
		for i := range part.Time {
			out.Append(part.Time[i], part.Open[i], part.High[i], part.Low[i], part.Close[i], part.Volume[i])
		}
	}
	if err := out.Validate(); err != nil {
		return nil, fmt.Errorf("dukascopy: invalid BI5 series: %w", err)
	}
	return out, nil
}

func decodeBI5(sym string, tf core.TF, compressed []byte, base time.Time, from, to core.Time) (*core.Series, error) {
	out := core.NewSeries(sym, tf, 0)
	if len(compressed) == 0 {
		return out, nil
	}
	r, err := lzma.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, fmt.Errorf("opening LZMA stream: %w", err)
	}
	buf := make([]byte, bi5RecordSize)
	for {
		_, err := io.ReadFull(r, buf)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading BI5 record: %w", err)
		}
		t := core.FromStd(base.Add(time.Duration(binary.BigEndian.Uint32(buf[0:4])) * time.Second))
		if t < from || t >= to {
			continue
		}
		o := core.Price(binary.BigEndian.Uint32(buf[4:8]))
		cl := core.Price(binary.BigEndian.Uint32(buf[8:12]))
		l := core.Price(binary.BigEndian.Uint32(buf[12:16]))
		h := core.Price(binary.BigEndian.Uint32(buf[16:20]))
		if h < l || o > h || o < l || cl > h || cl < l {
			return nil, fmt.Errorf("malformed BI5 bar at %s", t.Std().UTC().Format(time.RFC3339))
		}
		volume := math.Float32frombits(binary.BigEndian.Uint32(buf[20:24]))
		if volume == 0 && o == h && h == l && l == cl {
			continue // closed-market placeholder, not a traded candle
		}
		v := int64(0)
		if !math.IsNaN(float64(volume)) && !math.IsInf(float64(volume), 0) && volume > 0 {
			v = int64(math.Round(float64(volume)))
		}
		out.Append(t, o, h, l, cl, v)
	}
	return out, nil
}

func aggregate(src *core.Series, tf core.TF) *core.Series {
	out := core.NewSeries(src.Symbol, tf, 0)
	for i, t := range src.Time {
		bucket := tf.AlignDown(t)
		last := out.Len() - 1
		if last < 0 || out.Time[last] != bucket {
			out.Append(bucket, src.Open[i], src.High[i], src.Low[i], src.Close[i], src.Volume[i])
			continue
		}
		out.High[last] = max(out.High[last], src.High[i])
		out.Low[last] = min(out.Low[last], src.Low[i])
		out.Close[last] = src.Close[i]
		out.Volume[last] += src.Volume[i]
	}
	return out
}

func foldSundayIntoMonday(src *core.Series) *core.Series {
	out := core.NewSeries(src.Symbol, core.TFD1, src.Len())
	for i, t := range src.Time {
		bucket := t
		if t.Std().UTC().Weekday() == time.Sunday {
			bucket += core.Time(core.TFD1.Millis())
		}
		last := out.Len() - 1
		if last < 0 || out.Time[last] != bucket {
			out.Append(bucket, src.Open[i], src.High[i], src.Low[i], src.Close[i], src.Volume[i])
			continue
		}
		out.High[last] = max(out.High[last], src.High[i])
		out.Low[last] = min(out.Low[last], src.Low[i])
		out.Close[last] = src.Close[i]
		out.Volume[last] += src.Volume[i]
	}
	return out
}

func (c *Client) download(ctx context.Context, rawURL string, ttl time.Duration) ([]byte, error) {
	c.mu.Lock()
	entry, cached := c.artifactCache[rawURL]
	if cached && (entry.expires.IsZero() || time.Now().Before(entry.expires)) {
		c.mu.Unlock()
		return entry.body, nil
	}
	c.mu.Unlock()

	for attempt := 0; attempt < 6; attempt++ {
		requestURL := mirrorURL(rawURL, attempt)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "Assay/1.0")
		req.Header.Set("Accept", "application/octet-stream")
		res, err := c.client().Do(req)
		retryAfter := ""
		if err == nil {
			retryAfter = res.Header.Get("Retry-After")
			body, readErr := io.ReadAll(res.Body)
			res.Body.Close()
			if readErr != nil {
				return nil, readErr
			}
			switch {
			case res.StatusCode == http.StatusOK:
				c.remember(rawURL, body, ttl)
				return body, nil
			case res.StatusCode == http.StatusNotFound:
				if requestURL == rawURL {
					c.remember(rawURL, nil, ttl)
					return nil, nil // ordinary weekends and pre-listing dates
				}
				// The mirror can lag an artifact that exists on the dedicated
				// datafeed host. Its 404 is not authoritative after a transient
				// primary failure; continue and retry the primary.
				err = fmt.Errorf("mirror status %s", res.Status)
			case res.StatusCode < 500 && res.StatusCode != http.StatusTooManyRequests:
				return nil, fmt.Errorf("status %s", res.Status)
			default:
				err = fmt.Errorf("status %s", res.Status)
			}
		}
		if attempt == 5 {
			return nil, fmt.Errorf("download failed after retries: %w", err)
		}
		delay := c.downloadRetryDelay(attempt, retryAfter)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	panic("unreachable")
}

func (c *Client) downloadRetryDelay(attempt int, retryAfter string) time.Duration {
	if c.retryDelay != nil {
		return c.retryDelay(attempt, retryAfter)
	}
	delay := time.Duration(1<<attempt) * time.Second
	if seconds, err := strconv.Atoi(retryAfter); err == nil && seconds > 0 && seconds <= 60 {
		delay = time.Duration(seconds) * time.Second
	}
	return delay
}

func mirrorURL(rawURL string, attempt int) string {
	if attempt%2 == 0 {
		return rawURL
	}
	if strings.Contains(rawURL, "https://datafeed.dukascopy.com/datafeed/") {
		return strings.Replace(rawURL, "https://datafeed.dukascopy.com/datafeed/", "https://www.dukascopy.com/datafeed/", 1)
	}
	if strings.Contains(rawURL, "https://www.dukascopy.com/datafeed/") {
		return strings.Replace(rawURL, "https://www.dukascopy.com/datafeed/", "https://datafeed.dukascopy.com/datafeed/", 1)
	}
	return rawURL
}

func (c *Client) remember(rawURL string, body []byte, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.artifactCache == nil {
		c.artifactCache = make(map[string]artifactEntry)
	}
	entry := artifactEntry{body: body}
	if ttl > 0 {
		entry.expires = time.Now().Add(ttl)
	}
	c.artifactCache[rawURL] = entry
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
		return nil, venue.Unsupported(c.Name(), "symbol "+sym+" is not a Dukascopy instrument")
	}
	return spec, nil
}

func (c *Client) client() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return defaultHTTPClient
}

func (c *Client) base() string {
	if c.BaseURL != "" {
		return strings.TrimRight(c.BaseURL, "/")
	}
	return defaultBaseURL
}

func marketSymbol(sym string) string {
	if _, rest, ok := strings.Cut(sym, ":"); ok {
		sym = rest
	}
	return strings.ToUpper(strings.ReplaceAll(sym, "/", ""))
}

func supported(tf core.TF) bool {
	switch tf {
	case core.TFM1, core.TFM5, core.TFM15, core.TFM30, core.TFH1, core.TFH4, core.TFD1, core.TFW1:
		return true
	default:
		return false
	}
}

func supportedTFs() []core.TF {
	return []core.TF{core.TFM1, core.TFM5, core.TFM15, core.TFM30, core.TFH1, core.TFH4, core.TFD1, core.TFW1}
}
