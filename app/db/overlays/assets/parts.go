// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package assets

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strconv"

	"ItsBagelBot/pkg/objectstore"
)

// Parts live under tmp/ so any replica can finish an upload; the bucket's lifecycle rule must expire that prefix.
const partRoot = "tmp/"

func partPrefix(token string) string {
	sum := sha256.Sum256([]byte(token))
	return partRoot + hex.EncodeToString(sum[:16]) + "/"
}

func partKey(token string, number int) string {
	return partPrefix(token) + strconv.Itoa(number)
}

type chain struct {
	ctx     context.Context
	store   objectstore.Store
	keys    []string
	current io.ReadCloser
}

func (c *chain) Read(p []byte) (int, error) {
	for {
		if c.current == nil {
			if len(c.keys) == 0 {
				return 0, io.EOF
			}
			body, _, err := c.store.Get(c.ctx, c.keys[0])
			if err != nil {
				return 0, err
			}
			c.current, c.keys = body, c.keys[1:]
		}
		n, err := c.current.Read(p)
		if !errors.Is(err, io.EOF) {
			return n, err
		}
		_ = c.current.Close()
		c.current = nil
		if n > 0 {
			return n, nil
		}
	}
}

func (c *chain) Close() error {
	if c.current == nil {
		return nil
	}
	return c.current.Close()
}

func (s *Service) openParts(ctx context.Context, token string, count int) (io.ReadCloser, int64, error) {
	keys := make([]string, count)
	var total int64
	for i := range keys {
		keys[i] = partKey(token, i+1)
		info, err := s.Objects.Head(ctx, keys[i])
		if errors.Is(err, objectstore.ErrNotFound) {
			return nil, 0, ErrIncomplete
		}
		if err != nil {
			return nil, 0, err
		}
		total += info.Size
	}
	return &chain{ctx: ctx, store: s.Objects, keys: keys}, total, nil
}
