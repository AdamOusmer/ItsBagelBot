// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"testing"

	"ItsBagelBot/internal/domain/voiceplan"
	"ItsBagelBot/internal/moderation"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type manualBlockVoice struct {
	AlertVoicePublisher
	blocked []string
}

func (v manualBlockVoice) BlockedViewers(context.Context, string) ([]string, error) {
	return v.blocked, nil
}

func TestScreenDeniesManuallyBlockedViewer(t *testing.T) {
	queue := SpeechQueue{Voice: manualBlockVoice{blocked: []string{"viewer-1"}}}
	for _, tc := range []struct {
		name    string
		viewer  string
		source  voiceplan.SpeechSource
		allowed bool
	}{
		{"blocked viewer", "viewer-1", voiceplan.SourceViewer, false},
		{"other viewer", "viewer-2", voiceplan.SourceViewer, true},
		{"channel source ignores the list", "viewer-1", voiceplan.SourceChannel, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			subject := SpeechSubject{BroadcasterID: 42, ViewerID: tc.viewer, Source: tc.source}
			_, allowed, err := queue.Screen(context.Background(), subject, moderation.SpeechInput{Message: "hello"}, moderation.SpeechPolicy{})
			require.NoError(t, err)
			assert.Equal(t, tc.allowed, allowed)
		})
	}
}
