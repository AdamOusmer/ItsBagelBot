// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type listFetcher struct{ batches [][]string }

func (f *listFetcher) Fetch(_ context.Context, names []string) map[string]string {
	f.batches = append(f.batches, names)
	out := make(map[string]string, len(names))
	for _, name := range names {
		out[name] = "<" + name + ">"
	}
	return out
}

func TestExternalFansOutOnceAndCaps(t *testing.T) {
	f := &listFetcher{}
	chain := Chain{External{Fetcher: f, Max: 2}}
	assert.Equal(t, "<a> <b> {urlfetch:c} <a>",
		render(t, "{urlfetch:A} {urlfetch:b} {urlfetch:c} {urlfetch:a}", chain, nil))
	require.Len(t, f.batches, 1, "one fan-out per run")
	assert.Equal(t, []string{"a", "b"}, f.batches[0], "distinct, folded, capped")
}

func TestExternalNeverFetchesForANamelessSpan(t *testing.T) {
	f := &listFetcher{}
	chain := Chain{External{Fetcher: f, Max: 8}}
	assert.Equal(t, "{urlfetch} {urlfetch:}", render(t, "{urlfetch} {urlfetch:}", chain, nil))
	assert.Empty(t, f.batches)
}
