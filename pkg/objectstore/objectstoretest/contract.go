// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package objectstoretest

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/pkg/objectstore"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const cacheForever = "public, max-age=31536000, immutable"

func put(t *testing.T, s objectstore.Store, key, body string) {
	t.Helper()
	obj := objectstore.Object{Key: key, Body: strings.NewReader(body), Size: int64(len(body)), ContentType: "image/png", CacheControl: cacheForever}
	require.NoError(t, s.Put(context.Background(), obj))
}

func readAll(t *testing.T, s objectstore.Store, key string) (string, objectstore.Info) {
	t.Helper()
	rc, info, err := s.Get(context.Background(), key)
	require.NoError(t, err)
	t.Cleanup(func() { _ = rc.Close() })
	body, err := io.ReadAll(rc)
	require.NoError(t, err)
	info.ETag = ""
	info.LastModified = time.Time{}
	return string(body), info
}

func Contract(t *testing.T, newStore func(t *testing.T) objectstore.Store) {
	cases := map[string]func(t *testing.T, s objectstore.Store){
		"put then get":                  putThenGet,
		"head reports size":             headReportsSize,
		"missing key is not found":      missingKeyIsNotFound,
		"delete is idempotent":          deleteIsIdempotent,
		"copy keeps the source":         copyKeepsSource,
		"copy of missing key not found": copyMissingIsNotFound,
		"delete prefix spans pages":     deletePrefixSpansPages,
	}
	for name, run := range cases {
		t.Run(name, func(t *testing.T) { run(t, newStore(t)) })
	}
}

func putThenGet(t *testing.T, s objectstore.Store) {
	put(t, s, "u/1/a", "hello")

	body, info := readAll(t, s, "u/1/a")

	assert.Equal(t, "hello", body)
	assert.Equal(t, objectstore.Info{Key: "u/1/a", Size: 5, ContentType: "image/png", CacheControl: cacheForever}, info)
}

func headReportsSize(t *testing.T, s objectstore.Store) {
	put(t, s, "u/1/a", "hello")

	info, err := s.Head(context.Background(), "u/1/a")

	require.NoError(t, err)
	assert.Equal(t, int64(5), info.Size)
}

func missingKeyIsNotFound(t *testing.T, s objectstore.Store) {
	_, _, getErr := s.Get(context.Background(), "u/1/none")
	_, headErr := s.Head(context.Background(), "u/1/none")

	assert.ErrorIs(t, getErr, objectstore.ErrNotFound)
	assert.ErrorIs(t, headErr, objectstore.ErrNotFound)
}

func deleteIsIdempotent(t *testing.T, s objectstore.Store) {
	put(t, s, "u/1/a", "hello")

	require.NoError(t, s.Delete(context.Background(), "u/1/a"))
	require.NoError(t, s.Delete(context.Background(), "u/1/a"))

	_, err := s.Head(context.Background(), "u/1/a")
	assert.ErrorIs(t, err, objectstore.ErrNotFound)
}

func copyKeepsSource(t *testing.T, s objectstore.Store) {
	put(t, s, "u/1/a", "hello")

	require.NoError(t, s.Copy(context.Background(), "u/1/a", "u/1/b"))

	dst, _ := readAll(t, s, "u/1/b")
	src, _ := readAll(t, s, "u/1/a")
	assert.Equal(t, []string{"hello", "hello"}, []string{dst, src})
}

func copyMissingIsNotFound(t *testing.T, s objectstore.Store) {
	err := s.Copy(context.Background(), "u/1/none", "u/1/b")

	assert.ErrorIs(t, err, objectstore.ErrNotFound)
}

func deletePrefixSpansPages(t *testing.T, s objectstore.Store) {
	for _, key := range []string{"u/1/a", "u/1/b", "u/1/c", "u/1/d", "u/1/e", "u/2/a"} {
		put(t, s, key, "x")
	}

	require.NoError(t, s.DeletePrefix(context.Background(), "u/1/"))

	_, errGone := s.Head(context.Background(), "u/1/e")
	_, errKept := s.Head(context.Background(), "u/2/a")
	assert.ErrorIs(t, errGone, objectstore.ErrNotFound)
	assert.NoError(t, errKept)
}
