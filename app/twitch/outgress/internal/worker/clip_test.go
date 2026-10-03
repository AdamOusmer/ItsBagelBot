// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"testing"

	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testClipURL    = "https://clips.twitch.tv/AbCdEf"
	clipCreateBody = `{"data":[{"id":"AbCdEf","edit_url":"https://clips.twitch.tv/create/AbCdEf"}]}`
)

func sentMessage(t *testing.T, r recordedRequest) string {
	t.Helper()
	var body struct {
		Message string `json:"message"`
	}
	require.NoError(t, codec.Unmarshal([]byte(r.Body), &body))
	return body.Message
}

func TestClipCreationAnnouncesTheClipInChat(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    string
	}{
		{"credits the clipper and the title", `{"clipper":"viewer","title":"sick play"}`, "viewer clipped: sick play → " + testClipURL},
		{"credits the clipper alone", `{"clipper":"viewer"}`, "viewer made a clip → " + testClipURL},
		{"names the title alone", `{"title":"sick play"}`, "Clip: sick play → " + testClipURL},
		{"announces an anonymous clip", `{}`, "New clip → " + testClipURL},
		{
			"expands a custom template",
			`{"clipper":"viewer","title":"sick play","reply":"{user} made {clip} titled {target}"}`,
			"viewer made " + testClipURL + " titled sick play",
		},
		{
			"keeps unknown tokens literal",
			`{"clipper":"viewer","title":"sick play","reply":"{clipper}: {title} {clip} {mystery}"}`,
			"viewer: sick play " + testClipURL + " {mystery}",
		},
		{
			"TestClipExpandCaseInsensitive",
			`{"clipper":"viewer","title":"sick play","reply":"{Clipper} clipped {TITLE} → {Clip} {broken"}`,
			"viewer clipped sick play → " + testClipURL + " {broken",
		},
		{
			"TestClipExpandSanitizesLeadingSlashTitle",
			`{"clipper":"viewer","title":" /announce pwned","reply":"{title} clipped by {clipper}"}`,
			"announce pwned clipped by viewer",
		},
		{"resolves the dynamic tokens", `{"clipper":"viewer","reply":"{choice:only}, @{user} {random:7-7} {CHOICE:Hi}"}`, "only, @viewer 7 Hi"},
		{"renders an empty choice as nothing", `{"reply":"[{choice:}]"}`, "[]"},
		{"leaves a bare choice literal", `{"reply":"{choice}"}`, "{choice}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := &scriptedTransport{responses: []scriptedResponse{{status: 202, body: clipCreateBody}}}

			require.NoError(t, testMessage{Type: "clip", Payload: tt.payload}.send(pipelineWorker(t, rt)))

			requests := rt.recorded()
			require.Len(t, requests, 2)
			assert.Equal(t, "POST", requests[0].Method)
			assert.Equal(t, "/helix/clips", requests[0].Path)
			assert.Equal(t, tt.want, sentMessage(t, requests[1]))
		})
	}
}
