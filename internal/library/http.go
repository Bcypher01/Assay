package library

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/bcypher01/assay/internal/store"
)

// HTTPStore reads published library objects from a public, read-only HTTP
// origin (typically an R2 custom domain or CDN). It deliberately has no R2
// credentials: research clients must never have authority to publish data.
type HTTPStore struct {
	BaseURL string
	Client  *http.Client

	// Retries is the total number of attempts for a request that fails with a
	// transient error (network failure, 408, 429 or 5xx). Zero uses the package
	// default. A definitive answer — a 404, any other 4xx, or a 2xx — is never
	// retried.
	Retries int
	// Backoff is the delay before the first retry; each further attempt doubles
	// it, capped at maxRetryBackoff. Zero uses the package default.
	Backoff time.Duration
}

var defaultHTTPStoreClient = &http.Client{Timeout: 30 * time.Second}

const (
	defaultHTTPRetries = 3
	defaultHTTPBackoff = 200 * time.Millisecond
	maxRetryBackoff    = 5 * time.Second
)

func (s *HTTPStore) Head(ctx context.Context, key string) (store.Manifest, bool, error) {
	b, ok, err := s.read(ctx, publishedManifestKey(key))
	if err != nil || !ok {
		return store.Manifest{}, ok, err
	}
	var m store.Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return store.Manifest{}, false, fmt.Errorf("library: decode manifest %s: %w", key, err)
	}
	return m, true, nil
}

func publishedManifestKey(key string) string {
	return strings.TrimSuffix(key, ".parquet") + ".manifest.json"
}
func (s *HTTPStore) Get(ctx context.Context, key string) (io.ReadCloser, bool, error) {
	u, err := s.objectURL(key)
	if err != nil {
		return nil, false, err
	}
	c := s.Client
	if c == nil {
		c = defaultHTTPStoreClient
	}
	attempts := s.retries()
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, false, err
		}
		res, err := c.Do(req)
		if err != nil {
			lastErr = err
			if attempt+1 == attempts {
				break
			}
			if err := sleepContext(ctx, s.delay(attempt, "")); err != nil {
				return nil, false, lastErr
			}
			continue
		}
		if res.StatusCode == http.StatusNotFound {
			res.Body.Close()
			return nil, false, nil
		}
		if res.StatusCode >= 200 && res.StatusCode < 300 {
			return res.Body, true, nil
		}
		lastErr = fmt.Errorf("library: GET %s: HTTP %d", key, res.StatusCode)
		retryAfter := res.Header.Get("Retry-After")
		res.Body.Close()
		if !transientStatus(res.StatusCode) || attempt+1 == attempts {
			break
		}
		if err := sleepContext(ctx, s.delay(attempt, retryAfter)); err != nil {
			return nil, false, lastErr
		}
	}
	return nil, false, lastErr
}

// GetRange fetches an object from offset to its end over HTTP. partial reports
// whether the origin honoured the Range header with a 206, so a caller knows
// whether the stream can be appended to bytes it already holds.
func (s *HTTPStore) GetRange(ctx context.Context, key string, offset int64) (io.ReadCloser, bool, bool, error) {
	u, err := s.objectURL(key)
	if err != nil {
		return nil, false, false, err
	}
	c := s.Client
	if c == nil {
		c = defaultHTTPStoreClient
	}
	attempts := s.retries()
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, false, false, err
		}
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
		res, err := c.Do(req)
		if err != nil {
			lastErr = err
			if attempt+1 == attempts {
				break
			}
			if err := sleepContext(ctx, s.delay(attempt, "")); err != nil {
				return nil, false, false, lastErr
			}
			continue
		}
		switch {
		case res.StatusCode == http.StatusPartialContent:
			return res.Body, true, true, nil
		case res.StatusCode == http.StatusOK:
			// The origin ignored the Range header and sent the whole object.
			return res.Body, false, true, nil
		case res.StatusCode == http.StatusNotFound, res.StatusCode == http.StatusRequestedRangeNotSatisfiable:
			res.Body.Close()
			return nil, false, false, nil
		case transientStatus(res.StatusCode):
			lastErr = fmt.Errorf("library: GET %s: HTTP %d", key, res.StatusCode)
			retryAfter := res.Header.Get("Retry-After")
			res.Body.Close()
			if attempt+1 == attempts {
				break
			}
			if err := sleepContext(ctx, s.delay(attempt, retryAfter)); err != nil {
				return nil, false, false, lastErr
			}
		default:
			res.Body.Close()
			return nil, false, false, fmt.Errorf("library: GET %s: HTTP %d", key, res.StatusCode)
		}
	}
	return nil, false, false, lastErr
}

func (s *HTTPStore) retries() int {
	if s.Retries > 0 {
		return s.Retries
	}
	return defaultHTTPRetries
}

func (s *HTTPStore) delay(attempt int, retryAfter string) time.Duration {
	if d := parseRetryAfter(retryAfter); d >= 0 {
		return d
	}
	base := s.Backoff
	if base <= 0 {
		base = defaultHTTPBackoff
	}
	d := base << attempt
	if d <= 0 || d > maxRetryBackoff {
		d = maxRetryBackoff
	}
	return d
}

// transientStatus reports whether an HTTP status is worth retrying. A 404 or
// any other 4xx is a definitive answer about the object, not a transient fault.
func transientStatus(code int) bool {
	return code == http.StatusRequestTimeout ||
		code == http.StatusTooManyRequests ||
		code >= 500
}

// parseRetryAfter reads the delta-seconds form of Retry-After, returning -1
// when the header is absent or unusable.
func parseRetryAfter(v string) time.Duration {
	if v == "" {
		return -1
	}
	secs, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || secs < 0 {
		return -1
	}
	return time.Duration(secs) * time.Second
}

// sleepContext waits for d, returning early if ctx is cancelled.
func sleepContext(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
func (s *HTTPStore) read(ctx context.Context, key string) ([]byte, bool, error) {
	rc, ok, e := s.Get(ctx, key)
	if e != nil || !ok {
		return nil, ok, e
	}
	defer rc.Close()
	b, e := io.ReadAll(rc)
	return b, true, e
}
func (s *HTTPStore) objectURL(key string) (string, error) {
	if strings.Contains(key, "..") || strings.HasPrefix(key, "/") {
		return "", fmt.Errorf("library: unsafe object key %q", key)
	}
	u, e := url.Parse(s.BaseURL)
	if e != nil {
		return "", e
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("library: public object URL must be absolute")
	}
	u.Path = path.Join(u.Path, key)
	return u.String(), nil
}

var _ ObjectStore = (*HTTPStore)(nil)
