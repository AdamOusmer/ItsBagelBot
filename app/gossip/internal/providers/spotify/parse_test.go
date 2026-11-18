// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotify

import (
	"net/http"
	"sync/atomic"
	"testing"

	"ItsBagelBot/app/gossip/internal/providertest"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/stretchr/testify/assert"
)

func TestUnrecognizedURLsKeepDistinctTextCacheEntries(t *testing.T) {
	var searches atomic.Int32
	p := newPlayerProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		searches.Add(1)
		writeSearchTracks(t, w)
	}))
	for _, query := range []string{"https://example.com/first", "https://example.com/second", "https://example.com/first"} {
		reply := providertest.Call[gossiprpc.SpotifySearchReply](t, p, "search", gossiprpc.Request{ChannelID: "2", Query: query})
		assert.Empty(t, reply.Error)
	}
	assert.EqualValues(t, 2, searches.Load())
}
