// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotify

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUnrecognizedURLsKeepDistinctTextCacheKeys(t *testing.T) {
	first := classify("https://example.com/first")
	second := classify("https://example.com/second")
	assert.NotEqual(t, first.cacheKey(), second.cacheKey())
	assert.NotEmpty(t, first.text)
}

func TestSpotifyURLSchemeHandlingPreservesIDs(t *testing.T) {
	const id = "3n3Ppam7vgaVa1iaRUc9Lp"
	for _, query := range []string{"//open.spotify.com/track/" + id, "HTTPS://OPEN.SPOTIFY.COM/track/" + id} {
		target := classify(query)
		assert.Equal(t, resolveTrackID, target.kind)
		assert.Equal(t, id, target.id)
	}
	for _, query := range []string{"https:/open.spotify.com/track/" + id, "ftp://open.spotify.com/track/" + id} {
		assert.Equal(t, resolveInvalidLink, classify(query).kind)
	}
}
