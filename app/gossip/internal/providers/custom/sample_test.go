// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Tests for the DryRun rehearsal sample: the raw upstream body gossip hands
// back so the dashboard can build a clickable field picker.
//
// Its own file rather than more weight on custom_test.go, which already covers
// caching, rate limits, the SSRF gate, extraction and breakers. One more
// unrelated concern in there buys nothing and costs the reader.

package custom

import (
	"net/http"
	"testing"

	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The sample exists for the dashboard's field picker and must never widen the
// chat lane's blast radius, so both halves are pinned in one test: whatever
// makes DryRun return a body must also leave a non-DryRun reply empty.
func TestFetchDryRunReturnsSampleButChatNeverDoes(t *testing.T) {
	h := newHarness(t)
	const body = `{"forecast":{"temp":71.2}}`
	h.route(t, "/wx", staged{status: http.StatusOK, ct: "application/json", body: body})
	h.addDef("wx", "/wx", gossiprpc.FetchDef{URL: "placeholder", IsActive: true, JSONPath: []string{"forecast", "temp"}})

	dry := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "wx", DryRun: true})
	require.Equal(t, gossiprpc.FetchOK, dry.Status)
	assert.Equal(t, body, dry.Sample, "the field picker builds its tree from the real response")

	chat := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "wx"})
	require.Equal(t, gossiprpc.FetchOK, chat.Status)
	assert.Empty(t, chat.Sample, "upstream text must never reach the chat lane")
}

func TestFetchDryRunReturnsSampleEvenWhenPathIsWrong(t *testing.T) {
	h := newHarness(t)
	const body = `{"forecast":{"temp":71.2}}`
	h.route(t, "/wx", staged{status: http.StatusOK, ct: "application/json", body: body})
	h.addDef("wx", "/wx", gossiprpc.FetchDef{URL: "placeholder", IsActive: true, JSONPath: []string{"nope"}})

	r := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "wx", DryRun: true})
	require.Equal(t, gossiprpc.FetchBadDef, r.Status)
	assert.Equal(t, body, r.Sample, "an author whose path is wrong is exactly who needs the tree")
}

func TestSampleForGuards(t *testing.T) {
	body := []byte(`{"a":1}`)

	assert.Empty(t, sampleFor(&flight{}, body), "the dry-run flag is the only gate")
	assert.Equal(t, `{"a":1}`, sampleFor(&flight{dryRun: true}, body))
	assert.Empty(t, sampleFor(&flight{dryRun: true}, make([]byte, maxSampleBytes+1)),
		"oversized drops rather than truncates: a half body can parse as a shorter valid document whose paths do not match the real response")
	assert.Empty(t, sampleFor(&flight{dryRun: true}, []byte{0xff, 0xfe, 0x00}),
		"non-UTF-8 would not survive JSON marshalling intact")
}
