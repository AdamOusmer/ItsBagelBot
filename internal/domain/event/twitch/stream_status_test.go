// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitch

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDecodeStreamStatus(t *testing.T) {
	tests := []struct {
		name   string
		raw    string
		want   StreamStatus
		wantOK bool
	}{
		{
			name:   "online carries the Twitch message time as its version",
			raw:    `{"type":"stream.online","event":{"broadcaster_user_id":"42"},"received_at":"2026-10-01T17:16:06.842Z"}`,
			want:   StreamStatus{BroadcasterID: 42, Live: true, Version: 1790874966842},
			wantOK: true,
		},
		{
			name:   "offline in the raw EventSub shape",
			raw:    `{"subscription":{"type":"stream.offline"},"event":{"broadcaster_user_id":"42"},"received_at":"2026-10-01T17:16:07.241Z"}`,
			want:   StreamStatus{BroadcasterID: 42, Version: 1790874967241},
			wantOK: true,
		},
		{
			name:   "a missing receive time leaves the version unset",
			raw:    `{"type":"stream.online","event":{"broadcaster_user_id":"42"}}`,
			want:   StreamStatus{BroadcasterID: 42, Live: true},
			wantOK: true,
		},
		{name: "another event type", raw: `{"type":"channel.follow","event":{"broadcaster_user_id":"42"}}`},
		{name: "a broadcaster id that is not a number", raw: `{"type":"stream.online","event":{"broadcaster_user_id":"x"}}`},
		{name: "not JSON", raw: `{`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := DecodeStreamStatus([]byte(tt.raw))
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}
