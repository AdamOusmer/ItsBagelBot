// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package objstore

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
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

var ErrExists = errors.New("objstore: object already exists")

type Config struct {
	Endpoint        string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	Region          string
	HTTP            *http.Client
}

type Store struct {
	cfg  Config
	base *url.URL
	now  func() time.Time
}

type Object struct {
	Key          string
	LastModified time.Time
}

func New(cfg Config) (*Store, error) {
	base, err := url.Parse(strings.TrimRight(cfg.Endpoint, "/"))
	if err != nil {
		return nil, err
	}
	if base.Scheme == "" || base.Host == "" || cfg.Bucket == "" {
		return nil, errors.New("objstore: endpoint and bucket are required")
	}
	if cfg.Region == "" {
		cfg.Region = "auto"
	}
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: 15 * time.Second}
	}
	return &Store{cfg: cfg, base: base, now: time.Now}, nil
}

func (s *Store) PutNew(ctx context.Context, key string, body []byte) error {
	req, err := s.request(ctx, http.MethodPut, key, nil, body)
	if err != nil {
		return err
	}
	req.Header.Set("If-None-Match", "*")
	resp, err := s.do(req, body)
	if err != nil {
		return err
	}
	defer drain(resp)
	switch {
	case resp.StatusCode == http.StatusPreconditionFailed:
		return ErrExists
	case resp.StatusCode/100 != 2:
		return statusError(resp)
	}
	return nil
}

func (s *Store) List(ctx context.Context, prefix string) ([]Object, error) {
	var out []Object
	token := ""
	for {
		page, err := s.listPage(ctx, prefix, token)
		if err != nil {
			return nil, err
		}
		for _, c := range page.Contents {
			out = append(out, Object{Key: c.Key, LastModified: c.LastModified})
		}
		if !page.IsTruncated || page.NextContinuationToken == "" {
			return out, nil
		}
		token = page.NextContinuationToken
	}
}

type listResult struct {
	Contents []struct {
		Key          string    `xml:"Key"`
		LastModified time.Time `xml:"LastModified"`
	} `xml:"Contents"`
	IsTruncated           bool   `xml:"IsTruncated"`
	NextContinuationToken string `xml:"NextContinuationToken"`
}

func (s *Store) listPage(ctx context.Context, prefix, token string) (listResult, error) {
	query := url.Values{"list-type": {"2"}, "prefix": {prefix}}
	if token != "" {
		query.Set("continuation-token", token)
	}
	req, err := s.request(ctx, http.MethodGet, "", query, nil)
	if err != nil {
		return listResult{}, err
	}
	resp, err := s.do(req, nil)
	if err != nil {
		return listResult{}, err
	}
	defer drain(resp)
	if resp.StatusCode/100 != 2 {
		return listResult{}, statusError(resp)
	}
	var page listResult
	err = xml.NewDecoder(resp.Body).Decode(&page)
	return page, err
}

func (s *Store) request(ctx context.Context, method, key string, query url.Values, body []byte) (*http.Request, error) {
	u := *s.base
	u.Path = "/" + s.cfg.Bucket
	if key != "" {
		u.Path += "/" + key
	}
	u.RawPath = escapePath(u.Path)
	u.RawQuery = canonicalQuery(query)
	return http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
}

func (s *Store) do(req *http.Request, body []byte) (*http.Response, error) {
	s.sign(req, body, s.now().UTC())
	return s.cfg.HTTP.Do(req)
}

func (s *Store) sign(req *http.Request, body []byte, at time.Time) {
	amzDate := at.Format("20060102T150405Z")
	day := amzDate[:8]
	payload := sha256Hex(body)
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", payload)

	signed := "host;x-amz-content-sha256;x-amz-date"
	canonical := strings.Join([]string{
		req.Method,
		req.URL.EscapedPath(),
		req.URL.RawQuery,
		"host:" + req.URL.Host + "\nx-amz-content-sha256:" + payload + "\nx-amz-date:" + amzDate + "\n",
		signed,
		payload,
	}, "\n")
	scope := day + "/" + s.cfg.Region + "/s3/aws4_request"
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + sha256Hex([]byte(canonical))

	key := hmacSHA256([]byte("AWS4"+s.cfg.SecretAccessKey), day)
	for _, part := range []string{s.cfg.Region, "s3", "aws4_request"} {
		key = hmacSHA256(key, part)
	}
	req.Header.Set("Authorization", fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		s.cfg.AccessKeyID, scope, signed, hex.EncodeToString(hmacSHA256(key, toSign))))
}

func canonicalQuery(q url.Values) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, escape(k)+"="+escape(q.Get(k)))
	}
	return strings.Join(parts, "&")
}

func escapePath(p string) string {
	segments := strings.Split(p, "/")
	for i, seg := range segments {
		segments[i] = escape(seg)
	}
	return strings.Join(segments, "/")
}

func escape(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || strings.IndexByte("-_.~", c) >= 0 {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}

func statusError(resp *http.Response) error {
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return fmt.Errorf("objstore: %s %s: %d %s", resp.Request.Method, resp.Request.URL.Path, resp.StatusCode, bytes.TrimSpace(msg))
}

func drain(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	_ = resp.Body.Close()
}
