// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package objstore_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ItsBagelBot/pkg/objstore"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newStore(t *testing.T, h http.HandlerFunc) *objstore.Store {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	s, err := objstore.New(objstore.Config{Endpoint: srv.URL, Bucket: "ledger", AccessKeyID: "id", SecretAccessKey: "secret"})
	require.NoError(t, err)
	return s
}

func TestPutNewIsCreateOnly(t *testing.T) {
	existing := map[string]bool{}
	s := newStore(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "*", r.Header.Get("If-None-Match"))
		require.True(t, strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=id/"))
		if existing[r.URL.Path] {
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		existing[r.URL.Path] = true
	})

	require.NoError(t, s.PutNew(context.Background(), "erasures/1-2", nil))
	assert.ErrorIs(t, s.PutNew(context.Background(), "erasures/1-2", nil), objstore.ErrExists)
	assert.True(t, existing["/ledger/erasures/1-2"])
}

func TestPutNewTreatsLockedObjectAsExisting(t *testing.T) {
	s := newStore(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>ObjectLockedByBucketPolicy</Code><Message>The object is locked by the bucket policy.</Message></Error>`)
	})

	assert.ErrorIs(t, s.PutNew(context.Background(), "erasures/1-2", nil), objstore.ErrExists)
}

func TestPutNewKeepsOtherConflicts(t *testing.T) {
	s := newStore(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		fmt.Fprint(w, `<Error><Code>OperationAborted</Code></Error>`)
	})

	err := s.PutNew(context.Background(), "erasures/1-2", nil)
	assert.NotErrorIs(t, err, objstore.ErrExists)
	assert.ErrorContains(t, err, "409 OperationAborted")
}

func TestListFollowsContinuationTokens(t *testing.T) {
	s := newStore(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/ledger", r.URL.Path)
		require.Equal(t, "erasures/", r.URL.Query().Get("prefix"))
		if r.URL.Query().Get("continuation-token") == "" {
			fmt.Fprint(w, `<ListBucketResult><Contents><Key>erasures/1-1</Key><LastModified>2026-10-01T00:00:00.000Z</LastModified></Contents><IsTruncated>true</IsTruncated><NextContinuationToken>next</NextContinuationToken></ListBucketResult>`)
			return
		}
		fmt.Fprint(w, `<ListBucketResult><Contents><Key>erasures/2-2</Key><LastModified>2026-10-02T00:00:00.000Z</LastModified></Contents><IsTruncated>false</IsTruncated></ListBucketResult>`)
	})

	objects, err := s.List(context.Background(), "erasures/")

	require.NoError(t, err)
	require.Len(t, objects, 2)
	assert.Equal(t, "erasures/2-2", objects[1].Key)
	assert.Equal(t, 2, objects[1].LastModified.Day())
}

func TestErrorStatusIsReturned(t *testing.T) {
	s := newStore(t, func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "denied", http.StatusForbidden) })

	_, err := s.List(context.Background(), "erasures/")

	assert.ErrorContains(t, err, "403")
}
