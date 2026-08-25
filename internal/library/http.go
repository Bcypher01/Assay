package library

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/Bcypher01/Assay/internal/store"
)

// HTTPStore reads published library objects from a public, read-only HTTP
// origin (typically an R2 custom domain or CDN). It deliberately has no R2
// credentials: research clients must never have authority to publish data.
type HTTPStore struct {
	BaseURL string
	Client  *http.Client
}

func (s *HTTPStore) Head(ctx context.Context, key string) (store.Manifest, bool, error) {
	b, ok, err := s.read(ctx, key+".manifest.json")
	if err != nil || !ok {
		return store.Manifest{}, ok, err
	}
	var m store.Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return store.Manifest{}, false, fmt.Errorf("library: decode manifest %s: %w", key, err)
	}
	return m, true, nil
}
func (s *HTTPStore) Get(ctx context.Context, key string) (io.ReadCloser, bool, error) {
	u, err := s.objectURL(key)
	if err != nil {
		return nil, false, err
	}
	c := s.Client
	if c == nil {
		c = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, false, err
	}
	res, err := c.Do(req)
	if err != nil {
		return nil, false, err
	}
	if res.StatusCode == http.StatusNotFound {
		res.Body.Close()
		return nil, false, nil
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		res.Body.Close()
		return nil, false, fmt.Errorf("library: GET %s: HTTP %d", key, res.StatusCode)
	}
	return res.Body, true, nil
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
