// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package objectstore

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"ItsBagelBot/pkg/monitor"
)

var _ Store = (*Client)(nil)

type Client struct {
	bucket string
	base   *url.URL
	signer Signer
	http   *http.Client
	now    func() time.Time
}

func New(cfg Config) (*Client, error) {
	base, err := url.Parse(strings.TrimRight(cfg.Endpoint, "/"))
	if err != nil {
		return nil, err
	}
	if base.Scheme == "" || base.Host == "" || cfg.Bucket == "" {
		return nil, errors.New("objectstore: endpoint and bucket are required")
	}
	if cfg.Region == "" {
		cfg.Region = "auto"
	}
	if cfg.HTTP == nil {
		cfg.HTTP = defaultHTTP()
	}
	signer := Signer{AccessKeyID: cfg.AccessKeyID, SecretAccessKey: cfg.SecretAccessKey, Region: cfg.Region}
	return &Client{bucket: cfg.Bucket, base: base, signer: signer, http: cfg.HTTP, now: time.Now}, nil
}

// No overall client timeout: uploads of tens of MiB are bounded by the caller's context.
func defaultHTTP() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 30 * time.Second
	return &http.Client{Transport: monitor.ExternalTransport(transport)}
}

type operation struct {
	method  string
	key     string
	query   url.Values
	header  http.Header
	body    io.Reader
	size    int64
	payload string
}

func bytesOperation(method, key string, body []byte) operation {
	return operation{method: method, key: key, body: bytes.NewReader(body), size: int64(len(body)), payload: sha256Hex(body)}
}

func (c *Client) send(ctx context.Context, op operation) (*http.Response, error) {
	u := *c.base
	u.Path = "/" + c.bucket
	if op.key != "" {
		u.Path += "/" + op.key
	}
	u.RawPath = escapePath(u.Path)
	u.RawQuery = canonicalQuery(op.query)
	req, err := http.NewRequestWithContext(ctx, op.method, u.String(), op.body)
	if err != nil {
		return nil, err
	}
	req.ContentLength = op.size
	if op.body != nil && op.size == 0 {
		req.Body = http.NoBody
	}
	for name, v := range op.header {
		req.Header[name] = v
	}
	payload := op.payload
	if payload == "" {
		payload = sha256Hex(nil)
	}
	c.signer.Sign(req, payload, c.now())
	return c.http.Do(req)
}

func (c *Client) run(ctx context.Context, op operation) error {
	resp, err := c.send(ctx, op)
	if err != nil {
		return err
	}
	defer drain(resp)
	return check(resp)
}

func (c *Client) Put(ctx context.Context, obj Object) error {
	if obj.Size < 0 {
		return errors.New("objectstore: object size is required")
	}
	header := http.Header{}
	if obj.ContentType != "" {
		header.Set("Content-Type", obj.ContentType)
	}
	if obj.CacheControl != "" {
		header.Set("Cache-Control", obj.CacheControl)
	}
	return c.run(ctx, operation{method: http.MethodPut, key: obj.Key, header: header, body: obj.Body, size: obj.Size, payload: unsignedPayload})
}

func (c *Client) PutNew(ctx context.Context, key string, body []byte) error {
	op := bytesOperation(http.MethodPut, key, body)
	op.header = http.Header{"If-None-Match": {"*"}}
	resp, err := c.send(ctx, op)
	if err != nil {
		return err
	}
	defer drain(resp)
	switch {
	case resp.StatusCode == http.StatusPreconditionFailed:
		return ErrExists
	case resp.StatusCode == http.StatusConflict:
		return conflictError(resp)
	}
	return check(resp)
}

// R2 checks a bucket lock before If-None-Match, and only an existing object
// can be locked.
func conflictError(resp *http.Response) error {
	var body struct {
		Code string `xml:"Code"`
	}
	if xml.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&body) == nil && body.Code == "ObjectLockedByBucketPolicy" {
		return ErrExists
	}
	return fmt.Errorf("objectstore: %s %s: %d %s", resp.Request.Method, resp.Request.URL.Path, resp.StatusCode, body.Code)
}

func (c *Client) Get(ctx context.Context, key string) (io.ReadCloser, Info, error) {
	resp, err := c.send(ctx, operation{method: http.MethodGet, key: key})
	if err != nil {
		return nil, Info{}, err
	}
	if err := check(resp); err != nil {
		drain(resp)
		return nil, Info{}, err
	}
	return resp.Body, infoOf(key, resp), nil
}

func (c *Client) Head(ctx context.Context, key string) (Info, error) {
	resp, err := c.send(ctx, operation{method: http.MethodHead, key: key})
	if err != nil {
		return Info{}, err
	}
	defer drain(resp)
	if err := check(resp); err != nil {
		return Info{}, err
	}
	return infoOf(key, resp), nil
}

func (c *Client) Delete(ctx context.Context, key string) error {
	err := c.run(ctx, operation{method: http.MethodDelete, key: key})
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

func (c *Client) Copy(ctx context.Context, src, dst string) error {
	header := http.Header{}
	header.Set("x-amz-copy-source", "/"+c.bucket+"/"+escapePath(src))
	resp, err := c.send(ctx, operation{method: http.MethodPut, key: dst, header: header})
	if err != nil {
		return err
	}
	defer drain(resp)
	if err := check(resp); err != nil {
		return err
	}
	return copyResultError(resp)
}

// S3 can answer 200 and still report the failure in the body.
func copyResultError(resp *http.Response) error {
	var result struct {
		XMLName xml.Name
		Code    string `xml:"Code"`
	}
	_ = xml.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&result)
	if result.XMLName.Local == "Error" {
		return fmt.Errorf("objectstore: copy: %s", result.Code)
	}
	return nil
}

func infoOf(key string, resp *http.Response) Info {
	modified, _ := http.ParseTime(resp.Header.Get("Last-Modified"))
	return Info{
		Key:          key,
		Size:         resp.ContentLength,
		ContentType:  resp.Header.Get("Content-Type"),
		CacheControl: resp.Header.Get("Cache-Control"),
		ETag:         strings.Trim(resp.Header.Get("ETag"), `"`),
		LastModified: modified,
	}
}

func check(resp *http.Response) error {
	switch {
	case resp.StatusCode/100 == 2:
		return nil
	case resp.StatusCode == http.StatusNotFound:
		return fmt.Errorf("%w: %s", ErrNotFound, resp.Request.URL.Path)
	}
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return fmt.Errorf("objectstore: %s %s: %d %s", resp.Request.Method, resp.Request.URL.Path, resp.StatusCode, bytes.TrimSpace(msg))
}

func drain(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	_ = resp.Body.Close()
}
