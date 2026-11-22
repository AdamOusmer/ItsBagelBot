// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package objectstore_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"

	"ItsBagelBot/pkg/objectstore"
	"ItsBagelBot/pkg/objectstore/objectstoretest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const fakeBucket = "bagel-overlays"

const fakePageSize = 2

type fakeS3 struct {
	mu      sync.Mutex
	objects map[string]string
	puts    []http.Header
}

func newFakeS3(t *testing.T) (*fakeS3, *objectstore.Client) {
	t.Helper()
	fake := &fakeS3{objects: map[string]string{}}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	client, err := objectstore.New(objectstore.Config{Endpoint: srv.URL, Bucket: fakeBucket, AccessKeyID: "id", SecretAccessKey: "secret"})
	require.NoError(t, err)
	return fake, client
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := strings.TrimPrefix(r.URL.Path, "/"+fakeBucket+"/")
	switch r.Method {
	case http.MethodPut:
		f.put(w, r, key)
	case http.MethodGet, http.MethodHead:
		f.get(w, r, key)
	case http.MethodDelete:
		delete(f.objects, key)
		w.WriteHeader(http.StatusNoContent)
	case http.MethodPost:
		f.deleteBatch(w, r)
	}
}

func (f *fakeS3) put(w http.ResponseWriter, r *http.Request, key string) {
	if src := r.Header.Get("X-Amz-Copy-Source"); src != "" {
		body, ok := f.objects[strings.TrimPrefix(src, "/"+fakeBucket+"/")]
		if !ok {
			http.Error(w, "<Error><Code>NoSuchKey</Code></Error>", http.StatusNotFound)
			return
		}
		f.objects[key] = body
		fmt.Fprint(w, "<CopyObjectResult><ETag>x</ETag></CopyObjectResult>")
		return
	}
	body, _ := io.ReadAll(r.Body)
	f.objects[key] = string(body)
	f.puts = append(f.puts, r.Header.Clone())
}

func (f *fakeS3) get(w http.ResponseWriter, r *http.Request, key string) {
	if r.URL.Query().Get("list-type") == "2" {
		f.list(w, r)
		return
	}
	body, ok := f.objects[key]
	if !ok {
		http.Error(w, "<Error><Code>NoSuchKey</Code></Error>", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Content-Length", fmt.Sprint(len(body)))
	if r.Method == http.MethodGet {
		fmt.Fprint(w, body)
	}
}

func (f *fakeS3) list(w http.ResponseWriter, r *http.Request) {
	after := r.URL.Query().Get("continuation-token")
	keys := []string{}
	for k := range f.objects {
		if strings.HasPrefix(k, r.URL.Query().Get("prefix")) && k > after {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	truncated := len(keys) > fakePageSize
	if truncated {
		keys = keys[:fakePageSize]
	}
	var b strings.Builder
	b.WriteString("<ListBucketResult>")
	for _, k := range keys {
		fmt.Fprintf(&b, "<Contents><Key>%s</Key><Size>%d</Size></Contents>", k, len(f.objects[k]))
	}
	fmt.Fprintf(&b, "<IsTruncated>%t</IsTruncated>", truncated)
	if truncated {
		fmt.Fprintf(&b, "<NextContinuationToken>%s</NextContinuationToken>", keys[len(keys)-1])
	}
	b.WriteString("</ListBucketResult>")
	fmt.Fprint(w, b.String())
}

func (f *fakeS3) deleteBatch(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	for _, part := range strings.Split(string(raw), "<Key>")[1:] {
		key, _, _ := strings.Cut(part, "</Key>")
		delete(f.objects, key)
	}
	fmt.Fprint(w, "<DeleteResult></DeleteResult>")
}

func TestClientSatisfiesStoreContract(t *testing.T) {
	objectstoretest.Contract(t, func(t *testing.T) objectstore.Store {
		_, client := newFakeS3(t)
		return client
	})
}

func TestPutStreamsUnsignedBodyWithHeaders(t *testing.T) {
	fake, client := newFakeS3(t)

	err := client.Put(context.Background(), objectstore.Object{
		Key:          "u/1/a b",
		Body:         io.LimitReader(strings.NewReader("hello world"), 5),
		Size:         5,
		ContentType:  "image/png",
		CacheControl: "public, max-age=31536000, immutable",
	})

	require.NoError(t, err)
	require.Len(t, fake.puts, 1)
	assert.Equal(t, "hello", fake.objects["u/1/a b"])
	assert.Equal(t, "UNSIGNED-PAYLOAD", fake.puts[0].Get("X-Amz-Content-Sha256"))
	assert.Contains(t, fake.puts[0].Get("Authorization"), "SignedHeaders=cache-control;content-type;host;x-amz-content-sha256;x-amz-date")
}

func TestPutRejectsUnknownSize(t *testing.T) {
	_, client := newFakeS3(t)

	err := client.Put(context.Background(), objectstore.Object{Key: "k", Body: strings.NewReader("x"), Size: -1})

	assert.Error(t, err)
}
