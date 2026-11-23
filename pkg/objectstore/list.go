// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package objectstore

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type listResult struct {
	Contents []struct {
		Key          string    `xml:"Key"`
		Size         int64     `xml:"Size"`
		LastModified time.Time `xml:"LastModified"`
	} `xml:"Contents"`
	IsTruncated           bool   `xml:"IsTruncated"`
	NextContinuationToken string `xml:"NextContinuationToken"`
}

func (p listResult) keys() []string {
	keys := make([]string, 0, len(p.Contents))
	for _, c := range p.Contents {
		keys = append(keys, c.Key)
	}
	return keys
}

func (p listResult) more() bool { return p.IsTruncated && p.NextContinuationToken != "" }

func (c *Client) List(ctx context.Context, prefix string) ([]Info, error) {
	var out []Info
	token := ""
	for {
		page, err := c.listPage(ctx, prefix, token)
		if err != nil {
			return nil, err
		}
		for _, o := range page.Contents {
			out = append(out, Info{Key: o.Key, Size: o.Size, LastModified: o.LastModified})
		}
		if !page.more() {
			return out, nil
		}
		token = page.NextContinuationToken
	}
}

func (c *Client) listPage(ctx context.Context, prefix, token string) (listResult, error) {
	query := url.Values{"list-type": {"2"}, "prefix": {prefix}}
	if token != "" {
		query.Set("continuation-token", token)
	}
	resp, err := c.send(ctx, operation{method: http.MethodGet, query: query})
	if err != nil {
		return listResult{}, err
	}
	defer drain(resp)
	if err := check(resp); err != nil {
		return listResult{}, err
	}
	var page listResult
	err = xml.NewDecoder(resp.Body).Decode(&page)
	return page, err
}

func (c *Client) DeletePrefix(ctx context.Context, prefix string) error {
	if prefix == "" {
		return errors.New("objectstore: delete prefix must not be empty")
	}
	token := ""
	for {
		page, err := c.listPage(ctx, prefix, token)
		if err != nil {
			return err
		}
		if err := c.deleteKeys(ctx, page.keys()); err != nil {
			return err
		}
		if !page.more() {
			return nil
		}
		token = page.NextContinuationToken
	}
}

type deleteRequest struct {
	XMLName xml.Name       `xml:"Delete"`
	Quiet   bool           `xml:"Quiet"`
	Objects []deleteTarget `xml:"Object"`
}

type deleteTarget struct {
	Key string `xml:"Key"`
}

type deleteResult struct {
	Errors []struct {
		Key  string `xml:"Key"`
		Code string `xml:"Code"`
	} `xml:"Error"`
}

func (c *Client) deleteKeys(ctx context.Context, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	req := deleteRequest{Quiet: true}
	for _, k := range keys {
		req.Objects = append(req.Objects, deleteTarget{Key: k})
	}
	body, err := xml.Marshal(req)
	if err != nil {
		return err
	}
	sum := md5.Sum(body)
	op := bytesOperation(http.MethodPost, "", body)
	op.query = url.Values{"delete": {""}}
	op.header = http.Header{}
	op.header.Set("Content-Type", "application/xml")
	op.header.Set("Content-MD5", base64.StdEncoding.EncodeToString(sum[:]))
	resp, err := c.send(ctx, op)
	if err != nil {
		return err
	}
	defer drain(resp)
	if err := check(resp); err != nil {
		return err
	}
	var result deleteResult
	_ = xml.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result)
	if len(result.Errors) > 0 {
		return fmt.Errorf("objectstore: delete %s: %s", result.Errors[0].Key, result.Errors[0].Code)
	}
	return nil
}
