package pipeline

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/bcypher01/assay/internal/store"
)

// R2Config contains the Cloudflare R2 S3-API settings.
type R2Config struct {
	AccountID       string
	AccessKeyID     string
	SecretAccessKey string
	Bucket          string
	Endpoint        string
	Region          string
}

// R2Publisher writes objects to Cloudflare R2 through its S3-compatible API.
type R2Publisher struct {
	cfg    R2Config
	client *http.Client
	now    func() time.Time
}

var defaultR2HTTPClient = &http.Client{Timeout: 30 * time.Second}

// NewR2Publisher returns an R2 publisher. The endpoint should look like
// https://<account-id>.r2.cloudflarestorage.com.
func NewR2Publisher(cfg R2Config, client *http.Client) (*R2Publisher, error) {
	if cfg.Region == "" {
		cfg.Region = "auto"
	}
	if cfg.Endpoint == "" && cfg.AccountID != "" {
		cfg.Endpoint = "https://" + cfg.AccountID + ".r2.cloudflarestorage.com"
	}
	if cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" || cfg.Bucket == "" || cfg.Endpoint == "" {
		return nil, fmt.Errorf("pipeline: incomplete R2 configuration")
	}
	if client == nil {
		client = defaultR2HTTPClient
	}
	return &R2Publisher{cfg: cfg, client: client}, nil
}

var _ Publisher = (*R2Publisher)(nil)

func (p *R2Publisher) Put(ctx context.Context, key string, body io.Reader, meta store.Manifest) error {
	data, err := io.ReadAll(body)
	if err != nil {
		return fmt.Errorf("pipeline: reading object body: %w", err)
	}
	if err := p.do(ctx, http.MethodPut, key, data, "application/octet-stream", nil); err != nil {
		return err
	}
	mf, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("pipeline: encoding manifest: %w", err)
	}
	return p.do(ctx, http.MethodPut, manifestKey(key), mf, "application/json", nil)
}

func (p *R2Publisher) Head(ctx context.Context, key string) (store.Manifest, bool, error) {
	var data []byte
	err := p.do(ctx, http.MethodGet, manifestKey(key), nil, "", &data)
	var errStatus *statusError
	if errors.As(err, &errStatus) && errStatus.code == http.StatusNotFound {
		return store.Manifest{}, false, nil
	}
	if err != nil {
		return store.Manifest{}, false, err
	}
	var mf store.Manifest
	if err := json.Unmarshal(data, &mf); err != nil {
		return store.Manifest{}, false, fmt.Errorf("pipeline: parsing R2 manifest %s: %w", key, err)
	}
	return mf, true, nil
}

func (p *R2Publisher) Get(ctx context.Context, key string) (io.ReadCloser, bool, error) {
	var data []byte
	err := p.do(ctx, http.MethodGet, key, nil, "", &data)
	var errStatus *statusError
	if errors.As(err, &errStatus) && errStatus.code == http.StatusNotFound {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return io.NopCloser(bytes.NewReader(data)), true, nil
}

func (p *R2Publisher) do(ctx context.Context, method, key string, body []byte, contentType string, out *[]byte) error {
	u, err := p.objectURL(key)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	p.sign(req, body)
	res, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("pipeline: R2 %s %s: %w", method, key, err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return &statusError{code: res.StatusCode, msg: strings.TrimSpace(string(msg))}
	}
	if out != nil {
		data, err := io.ReadAll(res.Body)
		if err != nil {
			return fmt.Errorf("pipeline: reading R2 response: %w", err)
		}
		*out = data
	}
	return nil
}

func (p *R2Publisher) objectURL(key string) (*url.URL, error) {
	if strings.Contains(key, "..") || strings.HasPrefix(key, "/") {
		return nil, fmt.Errorf("pipeline: unsafe object key %q", key)
	}
	u, err := url.Parse(strings.TrimRight(p.cfg.Endpoint, "/"))
	if err != nil {
		return nil, fmt.Errorf("pipeline: R2 endpoint: %w", err)
	}
	u.Path = "/" + strings.Trim(p.cfg.Bucket, "/") + "/" + strings.TrimLeft(key, "/")
	return u, nil
}

func (p *R2Publisher) sign(req *http.Request, body []byte) {
	now := time.Now().UTC()
	if p.now != nil {
		now = p.now().UTC()
	}
	amzDate := now.Format("20060102T150405Z")
	shortDate := now.Format("20060102")
	payloadHash := sha256Hex(body)

	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	req.Header.Set("Host", req.URL.Host)

	signedHeaders := signedHeaderNames(req.Header)
	canonical := strings.Join([]string{
		req.Method,
		uriEncodePath(req.URL.Path),
		"",
		canonicalHeaders(req.Header, signedHeaders),
		strings.Join(signedHeaders, ";"),
		payloadHash,
	}, "\n")

	scope := shortDate + "/" + p.cfg.Region + "/s3/aws4_request"
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		sha256Hex([]byte(canonical)),
	}, "\n")

	key := signingKey(p.cfg.SecretAccessKey, shortDate, p.cfg.Region, "s3")
	sig := hex.EncodeToString(hmacSHA256(key, []byte(stringToSign)))
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+p.cfg.AccessKeyID+"/"+scope+
		", SignedHeaders="+strings.Join(signedHeaders, ";")+", Signature="+sig)
}

func signedHeaderNames(h http.Header) []string {
	names := make([]string, 0, len(h))
	for k := range h {
		names = append(names, strings.ToLower(k))
	}
	sort.Strings(names)
	return names
}

func canonicalHeaders(h http.Header, names []string) string {
	var b strings.Builder
	for _, name := range names {
		values := h.Values(name)
		for i := range values {
			values[i] = strings.Join(strings.Fields(values[i]), " ")
		}
		b.WriteString(name)
		b.WriteByte(':')
		b.WriteString(strings.Join(values, ","))
		b.WriteByte('\n')
	}
	return b.String()
}

func uriEncodePath(p string) string {
	if p == "" {
		return "/"
	}
	const hexChars = "0123456789ABCDEF"
	var out strings.Builder
	out.Grow(len(p))
	for i := 0; i < len(p); i++ {
		ch := p[i]
		if ch == '/' ||
			(ch >= 'a' && ch <= 'z') ||
			(ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') ||
			ch == '-' || ch == '_' || ch == '.' || ch == '~' {
			out.WriteByte(ch)
			continue
		}
		out.WriteByte('%')
		out.WriteByte(hexChars[ch>>4])
		out.WriteByte(hexChars[ch&15])
	}
	return out.String()
}

func signingKey(secret, date, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), []byte(date))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	return hmacSHA256(kService, []byte("aws4_request"))
}

func hmacSHA256(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

type statusError struct {
	code int
	msg  string
}

func (e *statusError) Error() string {
	if e.msg == "" {
		return fmt.Sprintf("pipeline: R2 status %d", e.code)
	}
	return fmt.Sprintf("pipeline: R2 status %d: %s", e.code, e.msg)
}
