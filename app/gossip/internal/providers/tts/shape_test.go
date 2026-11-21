// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tts_test

import (
	"strings"
	"testing"

	"ItsBagelBot/app/gossip/internal/providers/tts"

	"github.com/stretchr/testify/assert"
)

func TestShape(t *testing.T) {
	for _, tc := range []struct{ name, text, want string }{
		{"duplicate long pauses", "hi <long pause><long pause><long pause> bye", "hi <long pause> bye"},
		{"duplicates through whitespace and punctuation", "hi <laugh> ,! <LAUGH> bye", "hi <laugh> ,! bye"},
		{"same tag after speech", "hi <laugh> there <laugh>", "hi <laugh> there <laugh>"},
		{"long pause limit", "a <long pause> b <long pause> c <short pause> d", "a <long pause> b c <short pause> d"},
		{"pause limit includes long pause", "a <short pause> b <long pause> c <short pause> d <short pause> e", "a <short pause> b <long pause> c <short pause> d e"},
		{"total tag limit includes pauses", "a <short pause><laugh><cheer><sigh><breath><whispers> b", "a <short pause><laugh><cheer><sigh> b"},
		{"edge pauses leave the budget available", "<long pause> hi <long pause> bye!<short pause>", "hi <long pause> bye!"},
		{"mixed case and spacing", "hi <Short Pause> there <  WhISpERS  > friend", "hi <short pause> there <whispers> friend"},
		{"punctuation spam", "hi" + strings.Repeat(".", 200) + "bye", "hi...bye"},
		{"only dots", strings.Repeat(".", 200), ""},
		{"dash and mixed runs", "hi----bye ……_,~ now... ok", "hi--bye ... now... ok"},
		{"whitespace", "\r\n hi\t\n there\u2003 friend ", "hi there friend"},
		{"only duplicate laughs", "<laugh><laugh><laugh>", ""},
		{"tags and punctuation only", "<cheer> !! <sigh><breath><whispers> <long pause>", ""},
		{"emoji and punctuation only", "🙂 ...", ""},
		{"unknown tag becomes spoken text", "hi <rm -rf> bye", "hi rm -rf bye"},
		{"unknown tag alone becomes spoken text", "<rm -rf>", "rm -rf"},
		{"stray brackets cannot form a tag", "hi <<>laugh> bye", "hi laugh bye"},
		{"Unicode digit counts as speech", "<short pause> ٤ <sigh>", "٤ <sigh>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tts.Shape(tc.text))
		})
	}
}
