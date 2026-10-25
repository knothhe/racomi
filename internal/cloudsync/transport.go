// Package cloudsync synchronizes the portable skill library with an encrypted
// S3-compatible store. It requires conditional writes; there is no unsafe
// last-writer-wins fallback.
package cloudsync

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Config struct {
	Endpoint        string `json:"endpoint"`
	Region          string `json:"region"`
	Bucket          string `json:"bucket"`
	Prefix          string `json:"prefix"`
	AccessKeyID     string `json:"accessKeyId"`
	SecretAccessKey string `json:"secretAccessKey"`
	SessionToken    string `json:"sessionToken,omitempty"`
	Passphrase      string `json:"passphrase"`
	PathStyle       bool   `json:"pathStyle"`
}

var bucketPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)
var prefixPattern = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,240}$`)
var hashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func normalized(c Config) Config {
	c.Endpoint = strings.TrimRight(strings.TrimSpace(c.Endpoint), "/")
	c.Bucket, c.Region = strings.TrimSpace(c.Bucket), strings.TrimSpace(c.Region)
	c.Prefix = strings.Trim(strings.TrimSpace(c.Prefix), "/")
	if c.Region == "" {
		c.Region = "auto"
	}
	if c.Prefix == "" {
		c.Prefix = "racomi"
	}
	return c
}

// ValidateConfig performs offline validation without sending credentials.
func ValidateConfig(config Config) error {
	c := normalized(config)
	u, err := url.Parse(c.Endpoint)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return errors.New("S3 endpoint must be an origin URL without a path, query, or user information")
	}
	loopback := strings.EqualFold(u.Hostname(), "localhost")
	if ip := net.ParseIP(u.Hostname()); ip != nil {
		loopback = ip.IsLoopback()
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return errors.New("S3 endpoint must use HTTPS (HTTP is allowed only for local loopback servers)")
	}
	if !bucketPattern.MatchString(c.Bucket) || strings.Contains(c.Bucket, "..") || strings.Contains(c.Bucket, ".-") || strings.Contains(c.Bucket, "-.") {
		return errors.New("invalid S3 bucket name")
	}
	if !prefixPattern.MatchString(c.Prefix) {
		return errors.New("S3 prefix may contain only letters, numbers, dots, hyphens, underscores and slashes")
	}
	for _, part := range strings.Split(c.Prefix, "/") {
		if part == "" || part == "." || part == ".." {
			return errors.New("invalid S3 prefix segment")
		}
	}
	if c.AccessKeyID == "" || c.SecretAccessKey == "" {
		return errors.New("S3 access key ID and secret access key are required")
	}
	for _, value := range []string{c.AccessKeyID, c.SecretAccessKey, c.SessionToken, c.Region} {
		if strings.ContainsAny(value, "\r\n") {
			return errors.New("invalid newline in S3 configuration")
		}
	}
	if strings.ContainsAny(c.Region, " /\\") {
		return errors.New("invalid S3 region")
	}
	if len([]rune(c.Passphrase)) < 12 {
		return errors.New("sync encryption passphrase must contain at least 12 characters")
	}
	if len(c.Passphrase) > 4096 {
		return errors.New("sync encryption passphrase is too long")
	}
	return nil
}

type s3Client struct {
	config Config
	client *http.Client
}
type httpError struct {
	status int
	method string
}

func (e *httpError) Error() string { return fmt.Sprintf("S3 %s failed (HTTP %d)", e.method, e.status) }
func statusIs(err error, statuses ...int) bool {
	var e *httpError
	if !errors.As(err, &e) {
		return false
	}
	for _, s := range statuses {
		if e.status == s {
			return true
		}
	}
	return false
}

// transferTransport keeps enough idle connections for parallel object
// transfers; the default of two per host would redo TLS handshakes. It is
// derived per client so a replaced DefaultTransport still applies.
func transferTransport() http.RoundTripper {
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return http.DefaultTransport
	}
	transport = transport.Clone()
	transport.MaxIdleConnsPerHost = transferConcurrency
	return transport
}

func newClient(config Config) (*s3Client, error) {
	if err := ValidateConfig(config); err != nil {
		return nil, err
	}
	return &s3Client{config: normalized(config), client: &http.Client{Transport: transferTransport(), Timeout: 45 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func awsEscape(value string, slash bool) string {
	const digits = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-_.~", rune(c)) || slash && c == '/' {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte(digits[c>>4])
			b.WriteByte(digits[c&15])
		}
	}
	return b.String()
}
func canonicalQuery(query url.Values) string {
	var parts []string
	for k, values := range query {
		for _, v := range values {
			parts = append(parts, awsEscape(k, false)+"="+awsEscape(v, false))
		}
	}
	// AWS orders encoded keys and values, rather than their raw UTF-8 forms.
	sort.Strings(parts)
	return strings.Join(parts, "&")
}
func sumHex(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func hmacSum(key []byte, value string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(value))
	return h.Sum(nil)
}

func signRequest(req *http.Request, body []byte, c Config, now time.Time) {
	stamp, day := now.UTC().Format("20060102T150405Z"), now.UTC().Format("20060102")
	req.Header.Set("X-Amz-Date", stamp)
	req.Header.Set("X-Amz-Content-Sha256", sumHex(body))
	if c.SessionToken != "" {
		req.Header.Set("X-Amz-Security-Token", c.SessionToken)
	}
	headers := map[string]string{"host": req.URL.Host}
	for k, v := range req.Header {
		headers[strings.ToLower(k)] = strings.Join(strings.Fields(strings.Join(v, ",")), " ")
	}
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)
	var canonicalHeaders strings.Builder
	for _, name := range names {
		canonicalHeaders.WriteString(name + ":" + headers[name] + "\n")
	}
	signedHeaders := strings.Join(names, ";")
	canonical := req.Method + "\n" + awsEscape(req.URL.Path, true) + "\n" + req.URL.RawQuery + "\n" + canonicalHeaders.String() + "\n" + signedHeaders + "\n" + sumHex(body)
	scope := day + "/" + c.Region + "/s3/aws4_request"
	key := hmacSum([]byte("AWS4"+c.SecretAccessKey), day)
	key = hmacSum(key, c.Region)
	key = hmacSum(key, "s3")
	key = hmacSum(key, "aws4_request")
	signature := hex.EncodeToString(hmacSum(key, "AWS4-HMAC-SHA256\n"+stamp+"\n"+scope+"\n"+sumHex([]byte(canonical))))
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+c.AccessKeyID+"/"+scope+", SignedHeaders="+signedHeaders+", Signature="+signature)
}

const maxResponseBytes = 66 << 20

func (s *s3Client) request(ctx context.Context, method, key string, query url.Values, body []byte, headers map[string]string, limit int64) ([]byte, string, error) {
	u, _ := url.Parse(s.config.Endpoint)
	if s.config.PathStyle {
		u.Path = "/" + s.config.Bucket
	} else {
		u.Host = s.config.Bucket + "." + u.Host
	}
	if key != "" {
		u.Path += "/" + key
	} else if u.Path == "" {
		u.Path = "/"
	}
	u.RawPath = awsEscape(u.Path, true)
	u.RawQuery = canonicalQuery(query)
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, "", errors.New("cannot create S3 request")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	signRequest(req, body, s.config, time.Now())
	response, err := s.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, "", ctx.Err()
		}
		return nil, "", errors.New("S3 network request failed; check endpoint, connectivity and TLS")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return nil, "", &httpError{response.StatusCode, method}
	}
	// S3 HEAD reports the size of the stored object even though there is no
	// response body. That metadata must not be treated as a downloaded body.
	if method == http.MethodHead {
		return nil, response.Header.Get("ETag"), nil
	}
	if limit == 0 {
		limit = maxResponseBytes
	}
	if response.ContentLength > limit {
		return nil, "", errors.New("S3 response exceeds size limit")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, "", errors.New("S3 response was interrupted")
	}
	if int64(len(data)) > limit {
		return nil, "", errors.New("S3 response exceeds size limit")
	}
	return data, response.Header.Get("ETag"), nil
}
func (s *s3Client) get(ctx context.Context, key string, limit int64) ([]byte, string, error) {
	return s.request(ctx, http.MethodGet, key, nil, nil, nil, limit)
}
func (s *s3Client) put(ctx context.Context, key string, data []byte, etag string, create bool) (string, error) {
	headers := map[string]string{"Content-Type": "application/octet-stream"}
	if create {
		headers["If-None-Match"] = "*"
	} else if etag != "" {
		headers["If-Match"] = etag
	} else {
		return "", errors.New("refusing S3 write without a conditional precondition")
	}
	_, next, err := s.request(ctx, http.MethodPut, key, nil, data, headers, 4096)
	return next, err
}

type listPage struct {
	Contents []struct {
		Key string `xml:"Key"`
	} `xml:"Contents"`
	IsTruncated bool   `xml:"IsTruncated"`
	Next        string `xml:"NextContinuationToken"`
}

func (s *s3Client) list(ctx context.Context, prefix string) ([]string, error) {
	var result []string
	token := ""
	for page := 0; page < 100; page++ {
		q := url.Values{"list-type": {"2"}, "prefix": {prefix}, "max-keys": {"1000"}}
		if token != "" {
			q.Set("continuation-token", token)
		}
		data, _, err := s.request(ctx, http.MethodGet, "", q, nil, nil, 4<<20)
		if err != nil {
			return nil, err
		}
		var listing listPage
		if err = xml.Unmarshal(data, &listing); err != nil {
			return nil, errors.New("invalid S3 object listing")
		}
		for _, entry := range listing.Contents {
			if !strings.HasPrefix(entry.Key, prefix) {
				return nil, errors.New("S3 listing returned an unexpected key")
			}
			result = append(result, entry.Key)
		}
		if len(result) > 10000 {
			return nil, errors.New("backup history exceeds 10,000 revisions")
		}
		if !listing.IsTruncated {
			return result, nil
		}
		if listing.Next == "" || listing.Next == token {
			return nil, errors.New("invalid S3 listing continuation")
		}
		token = listing.Next
	}
	return nil, errors.New("S3 listing has too many pages")
}
