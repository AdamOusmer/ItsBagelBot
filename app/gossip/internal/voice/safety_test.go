// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package voice_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/internal/domain/voiceplan"
	"ItsBagelBot/pkg/bus"
	pkgvalkey "ItsBagelBot/pkg/valkey"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeSpeech struct {
	reply gossiprpc.TTSSynthesizeReply
	calls atomic.Int32
}

func (f *fakeSpeech) Synthesize(context.Context, gossiprpc.Request) gossiprpc.TTSSynthesizeReply {
	f.calls.Add(1)
	return f.reply
}

func refusedReply(class gossiprpc.RefusalClass) gossiprpc.TTSSynthesizeReply {
	return gossiprpc.TTSSynthesizeReply{Error: "the speech provider refused this request", Reason: "refused", RefusalClass: class}
}

func viewerJob(source voiceplan.SpeechSource, viewerID, event string) voiceplan.AlertJob {
	job := alertJob("hello chat")
	job.Source, job.ViewerID, job.EventID = source, viewerID, event
	return job
}

func TestRefusalsStrikeOnlyViewerJobs(t *testing.T) {
	for _, tc := range []struct {
		name        string
		class       gossiprpc.RefusalClass
		source      voiceplan.SpeechSource
		viewerID    string
		wantBlocked bool
	}{
		{"policy refusal of a viewer job", gossiprpc.RefusalPolicy, voiceplan.SourceViewer, "viewer", true},
		{"local floor block of a viewer job", gossiprpc.RefusalLocal, voiceplan.SourceViewer, "viewer", true},
		{"unspecified refusal blocks in the channel", gossiprpc.RefusalUnspecified, voiceplan.SourceViewer, "viewer", true},
		{"invalid outcome never strikes", gossiprpc.RefusalInvalid, voiceplan.SourceViewer, "viewer", false},
		{"channel job never strikes", gossiprpc.RefusalPolicy, voiceplan.SourceChannel, "viewer", false},
		{"old job without source never strikes", gossiprpc.RefusalPolicy, "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, audioUpstream)
			speech := &fakeSpeech{reply: refusedReply(tc.class)}
			r.handler.Speech = speech
			viewer := tc.viewerID
			if viewer != "" {
				viewer += uuid.NewString()
			}
			for i := range 5 {
				job := viewerJob(tc.source, viewer, uuid.NewString()[:8]+string(rune('a'+i)))
				assert.ErrorIs(t, r.handler.Handle(context.Background(), message(t, job)), bus.ErrDropJob)
			}
			block, err := r.handler.Safety.ViewerBlock(context.Background(), r.handler.Store, userID, viewer)
			require.NoError(t, err)
			assert.Equal(t, tc.wantBlocked, block.Scope != pkgvalkey.SpeechBlockNone)
			if tc.wantBlocked {
				assert.Less(t, speech.calls.Load(), int32(5))
			}
		})
	}
}

func TestPausedChannelDropsJobBeforeSynthesis(t *testing.T) {
	r := newRig(t, audioUpstream)
	speech := &fakeSpeech{}
	r.handler.Speech = speech
	ctx := context.Background()
	require.NoError(t, r.handler.Safety.SetOperatorPause(ctx, r.handler.Store, true))
	t.Cleanup(func() { _ = r.handler.Safety.SetOperatorPause(ctx, r.handler.Store, false) })

	assert.ErrorIs(t, r.handler.Handle(ctx, message(t, alertJob("hello"))), bus.ErrDropJob)
	assert.Zero(t, speech.calls.Load())
}

func TestEmptyAudioRetriesOnceThenDrops(t *testing.T) {
	for _, tc := range []struct {
		name      string
		delivered uint64
		wantDrop  bool
	}{
		{"first delivery retries", 1, false},
		{"second delivery drops", 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, audioUpstream)
			r.handler.Speech = &fakeSpeech{reply: gossiprpc.TTSSynthesizeReply{Error: "speech provider returned no audio", RefusalClass: gossiprpc.RefusalEmpty}}
			msg := message(t, alertJob("hello"))
			msg.Delivered = tc.delivered
			err := r.handler.Handle(context.Background(), msg)
			if tc.wantDrop {
				assert.ErrorIs(t, err, bus.ErrDropJob)
				return
			}
			var retry bus.JobRetryError
			require.ErrorAs(t, err, &retry)
			assert.Equal(t, 2*time.Second, retry.Delay)
		})
	}
}

func TestHeldJobRetriesUntilNotBefore(t *testing.T) {
	r := newRig(t, audioUpstream)
	speech := &fakeSpeech{}
	r.handler.Speech = speech
	job := alertJob("hello")
	job.NotBefore = time.Now().Add(time.Minute)

	var retry bus.JobRetryError
	require.ErrorAs(t, r.handler.Handle(context.Background(), message(t, job)), &retry)
	assert.Greater(t, retry.Delay, 30*time.Second)
	assert.Zero(t, speech.calls.Load())
}
