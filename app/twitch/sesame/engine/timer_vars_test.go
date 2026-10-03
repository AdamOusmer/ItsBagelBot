// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func timerPipeline(stream StreamInfoLookup, fetch UrlFetchCaller) *Pipeline {
	d := Deps{
		Proj:        fakeReader{},
		Live:        liveAlways{},
		Cooldown:    NoopCooldown{},
		Pub:         &fakePublisher{},
		Log:         zap.NewNop(),
		StreamInfo:  stream,
		CustomFetch: fetch,
	}
	return NewPipeline(d, NewRegistry(zap.NewNop()), Config{OutgressPremium: premiumSubj, OutgressStandard: standardSubj})
}

func timerFireStore(p *Pipeline) (*ValkeyTimerStore, *fakePublisher) {
	pub := &fakePublisher{}
	store := &ValkeyTimerStore{
		pub:              pub,
		proj:             fakeReader{},
		outgressStandard: standardSubj,
		log:              zap.NewNop(),
		now:              time.Now,
		pipeline:         p,
	}
	return store, pub
}

func TestFireExpandsTimerScopedTokensAndLeavesRestLiteral(t *testing.T) {
	stream := &stubStreamInfo{byAddress: map[string]StreamInfoResult{"42/": liveNow()}}
	p := timerPipeline(stream, nil)
	store, pub := timerFireStore(p)

	store.fire(context.Background(), armedTimer{
		ref: timerRef{broadcasterID: 42, id: "t1"},
		def: timerDef{ID: "t1", Message: "{uptime} {random} {user} {1}", Interval: 60, Enabled: true},
	})

	require.Len(t, pub.got, 1)
	text := chatMessageText(t, pub.got[0].msg)
	assert.Regexp(t, regexp.MustCompile(`^2 hours \d+ \{user\} \{1\}$`), text)
}

func TestFirePostsByteIdentical(t *testing.T) {
	for _, tc := range []struct {
		name          string
		pipeline      *Pipeline
		broadcasterID uint64
		msg           string
	}{
		{"NoTokenMessage", timerPipeline(nil, nil), 1, "hello chat, welcome to the stream!"},
		{"NilPipelineRaw", nil, 3, "hello {uptime} {random} raw - no chain wired yet"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, pub := timerFireStore(tc.pipeline)

			store.fire(context.Background(), armedTimer{
				ref: timerRef{broadcasterID: tc.broadcasterID, id: "t1"},
				def: timerDef{ID: "t1", Message: tc.msg, Interval: 60, Enabled: true},
			})

			require.Len(t, pub.got, 1)
			assert.Equal(t, tc.msg, chatMessageText(t, pub.got[0].msg))
		})
	}
}

func TestFireFailedStreamLookupLeavesUptimeEmptyStillPosts(t *testing.T) {
	stream := &stubStreamInfo{err: errors.New("boom")}
	p := timerPipeline(stream, nil)
	store, pub := timerFireStore(p)

	store.fire(context.Background(), armedTimer{
		ref: timerRef{broadcasterID: 7, id: "t1"},
		def: timerDef{ID: "t1", Message: "live: {uptime} go", Interval: 60, Enabled: true},
	})

	require.Len(t, pub.got, 1, "a failed lookup must never drop the timer")
	assert.Equal(t, "live:  go", chatMessageText(t, pub.got[0].msg))
}

func TestFireUrlfetchResolvesThroughExternal(t *testing.T) {
	ff := &fakeUrlFetch{replies: map[string]gossiprpc.CustomFetchReply{
		"w.t": {Status: gossiprpc.FetchOK, Values: []string{"72F"}},
	}}
	p := timerPipeline(nil, ff)
	store, pub := timerFireStore(p)

	store.fire(context.Background(), armedTimer{
		ref: timerRef{broadcasterID: 9, id: "t1"},
		def: timerDef{ID: "t1", Message: "now {urlfetch:w.t}", Interval: 60, Enabled: true},
	})

	require.Len(t, pub.got, 1)
	assert.Equal(t, "now 72F", chatMessageText(t, pub.got[0].msg))
	assert.Equal(t, 1, ff.calls())
}

func TestFireMultiLineFansOutAndCapsAtMaxResponseLines(t *testing.T) {
	p := timerPipeline(nil, nil)
	store, pub := timerFireStore(p)
	message := strings.Join([]string{"line1", "line2", "", "line3", "line4", "line5", "line6"}, "\n")

	store.fire(context.Background(), armedTimer{
		ref: timerRef{broadcasterID: 11, id: "t1"},
		def: timerDef{ID: "t1", Message: message, Interval: 60, Enabled: true},
	})

	require.Len(t, pub.got, 5, "the blank line drops and the 6th surviving line hits the cap")
	got := make([]string, len(pub.got))
	for i, c := range pub.got {
		got[i] = chatMessageText(t, c.msg)
	}
	assert.Equal(t, []string{"line1", "line2", "line3", "line4", "line5"}, got)
}

func TestFireBlankExpansionWarnsOnceAndPublishesNothing(t *testing.T) {
	core, logs := observer.New(zapcore.WarnLevel)
	p := timerPipeline(nil, nil)
	pub := &fakePublisher{}
	store := &ValkeyTimerStore{
		pub: pub, proj: fakeReader{}, outgressStandard: standardSubj,
		log: zap.New(core), now: time.Now, pipeline: p,
	}
	at := armedTimer{ref: timerRef{broadcasterID: 13, id: "t1"}, def: timerDef{ID: "t1", Message: "   \n  \n\t", Interval: 60, Enabled: true}}

	store.fire(context.Background(), at)
	store.fire(context.Background(), at)

	assert.Empty(t, pub.got, "a blank expansion publishes nothing")
	warnings := logs.FilterMessage("timers: expansion left nothing to post, fire slot spent").All()
	require.Len(t, warnings, 1, "the warning logs once per timer, not once per fire")
}
