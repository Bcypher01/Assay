// Package bls imports official CPI and Employment Situation releases from the
// BLS archive. It deliberately uses release pages, not today's revised time
// series, so each record describes what was published on that day.
package bls

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Bcypher01/Assay/internal/core"
	"github.com/Bcypher01/Assay/internal/event"
	"github.com/Bcypher01/Assay/internal/eventsource"
)

const defaultBaseURL = "https://www.bls.gov"

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

// Archive reads BLS release pages captured from a normal browser. Keeping the
// source documents locally avoids depending on BLS's bot protection at study
// time and preserves the original official release text as evidence.
type Archive struct{ Dir string }

func (Archive) Name() string { return "bls-official-archive-v1" }
func (a Archive) Events(ctx context.Context, scope eventsource.Scope) ([]event.Event, error) {
	entries, err := os.ReadDir(a.Dir)
	if err != nil {
		return nil, fmt.Errorf("bls archive: read %s: %w", a.Dir, err)
	}
	var out []event.Event
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".htm") {
			continue
		}
		var kind event.Kind
		switch {
		case strings.HasPrefix(name, "cpi_"):
			kind = event.KindUSCPI
		case strings.HasPrefix(name, "empsit_"):
			kind = event.KindUSNonfarmPayrolls
		default:
			continue
		}
		// The filename is the official release date. Use it only as a cheap
		// range prefilter; the timestamp used in the event still comes from the
		// release document's embargo line.
		fileDay, err := archiveDay(name)
		if err != nil {
			return nil, err
		}
		if scope.From != 0 && fileDay < scope.From {
			continue
		}
		if scope.To != 0 && fileDay >= scope.To {
			continue
		}
		page, err := os.ReadFile(filepath.Join(a.Dir, name))
		if err != nil {
			return nil, err
		}
		occurred, title, err := releaseTime(string(page))
		if err != nil {
			return nil, fmt.Errorf("bls archive: %s: %w", name, err)
		}
		if (scope.From != 0 && occurred < scope.From) || (scope.To != 0 && occurred >= scope.To) {
			continue
		}
		id := "bls:" + strings.TrimSuffix(name, ".htm")
		url := defaultBaseURL + "/news.release/archives/" + name
		out = append(out, event.Event{ID: id, OccurredAt: occurred, KnowledgeAt: occurred, Precision: event.PrecisionExact, Kind: kind, Title: title, Jurisdiction: "US", Currencies: []string{"USD"}, Impact: event.ImpactHigh, Status: event.StatusReleased, Source: event.SourceRef{Provider: a.Name(), RecordID: id, URL: url}, Evidence: []event.EvidenceRef{{Title: "Captured U.S. Bureau of Labor Statistics release", URL: url}}})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].OccurredAt != out[j].OccurredAt {
			return out[i].OccurredAt < out[j].OccurredAt
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func archiveDay(name string) (core.Time, error) {
	stamp := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(name, "cpi_"), "empsit_"), ".htm")
	if len(stamp) != 8 {
		return 0, fmt.Errorf("bls archive: invalid filename %q", name)
	}
	t, err := time.ParseInLocation("01022006", stamp, time.UTC)
	if err != nil {
		return 0, fmt.Errorf("bls archive: %s: %w", name, err)
	}
	return core.FromStd(t), nil
}

func (c *Client) Name() string { return "bls-official-v1" }
func (c *Client) Events(ctx context.Context, scope eventsource.Scope) ([]event.Event, error) {
	if scope.From == 0 || scope.To == 0 || scope.To <= scope.From {
		return nil, fmt.Errorf("bls: an explicit import range is required")
	}
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = defaultBaseURL
	}
	var out []event.Event
	for _, feed := range []struct {
		path   string
		kind   event.Kind
		prefix string
	}{{"/bls/news-release/cpi.htm", event.KindUSCPI, "cpi_"}, {"/bls/news-release/empsit.htm", event.KindUSNonfarmPayrolls, "empsit_"}} {
		index, err := c.get(ctx, base+feed.path)
		if err != nil {
			return nil, err
		}
		for _, ref := range archiveLinks(index, feed.prefix) {
			page, err := c.get(ctx, absolute(base, ref))
			if err != nil {
				return nil, err
			}
			occurred, title, err := releaseTime(page)
			if err != nil {
				return nil, fmt.Errorf("bls: %s: %w", ref, err)
			}
			if occurred < scope.From || occurred >= scope.To {
				continue
			}
			id := "bls:" + strings.TrimSuffix(strings.TrimPrefix(ref, "/news.release/archives/"), ".htm")
			out = append(out, event.Event{ID: id, OccurredAt: occurred, KnowledgeAt: occurred, Precision: event.PrecisionExact, Kind: feed.kind, Title: title, Jurisdiction: "US", Currencies: []string{"USD"}, Impact: event.ImpactHigh, Status: event.StatusReleased, Source: event.SourceRef{Provider: c.Name(), RecordID: id, URL: absolute(base, ref)}, Evidence: []event.EvidenceRef{{Title: "US Bureau of Labor Statistics release", URL: absolute(base, ref)}}})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].OccurredAt != out[j].OccurredAt {
			return out[i].OccurredAt < out[j].OccurredAt
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (c *Client) get(ctx context.Context, url string) (string, error) {
	h := c.HTTPClient
	if h == nil {
		h = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	// BLS rejects requests with Go's default user agent. Identify the importer
	// and request only its small official archive surface.
	req.Header.Set("User-Agent", "Assay/1.0 (+https://github.com/Bcypher01/Assay)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	res, err := h.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("bls: GET %s: %s", url, res.Status)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

var linkRE = regexp.MustCompile(`(?i)href=["']([^"']*/news\.release/archives/(?:cpi|empsit)_[0-9]{8}\.(?:htm|pdf))["']`)

func archiveLinks(page, prefix string) []string {
	m := linkRE.FindAllStringSubmatch(page, -1)
	seen := map[string]bool{}
	var out []string
	for _, x := range m {
		if !strings.Contains(strings.ToLower(x[1]), prefix) || seen[x[1]] {
			continue
		}
		seen[x[1]] = true
		out = append(out, x[1])
	}
	sort.Strings(out)
	return out
}

var timeRE = regexp.MustCompile(`(?is)(\d{1,2}:\d{2})\s*(a\.m\.|p\.m\.)\s*\((?:ET|EST|EDT)\)\s*(?:(?:Monday|Tuesday|Wednesday|Thursday|Friday),?\s*)?([A-Za-z]+\s+\d{1,2},\s+\d{4})`)
var titleRE = regexp.MustCompile(`(?is)(?:THE\s+EMPLOYMENT\s+SITUATION|CONSUMER\s+PRICE\s+INDEX)\s*(?:--|—|-)?\s*([^<\n]{0,80})`)

func releaseTime(page string) (core.Time, string, error) {
	clean := strings.ReplaceAll(strings.ReplaceAll(page, "&nbsp;", " "), "\r", " ")
	m := timeRE.FindStringSubmatch(clean)
	if len(m) != 4 {
		return 0, "", fmt.Errorf("release timestamp not found")
	}
	clock := m[1]
	if strings.HasPrefix(strings.ToLower(m[2]), "p") && !strings.HasPrefix(clock, "12:") {
		parts := strings.Split(clock, ":")
		clock = fmt.Sprintf("%02d:%s", mustInt(parts[0])+12, parts[1])
	}
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		return 0, "", err
	}
	t, err := time.ParseInLocation("January 2, 2006 15:04", m[3]+" "+clock, loc)
	if err != nil {
		return 0, "", err
	}
	title := "BLS release"
	if x := titleRE.FindStringSubmatch(clean); len(x) == 2 {
		title = strings.TrimSpace(strings.Join(strings.Fields(x[0]), " "))
	}
	return core.FromStd(t), title, nil
}
func mustInt(s string) int { var n int; _, _ = fmt.Sscanf(s, "%d", &n); return n }
func absolute(base, ref string) string {
	if strings.HasPrefix(ref, "http") {
		return ref
	}
	return base + "/" + strings.TrimLeft(ref, "/")
}
