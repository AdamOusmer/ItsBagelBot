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

	"ItsBagelBot/app/twitch/sesame/engine/scope"
	"ItsBagelBot/internal/domain/outgress"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/tmpl"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// timerPipeline builds a Pipeline wired only with what a timer's expansion
// can reach: a stream reader for {uptime}/{title}/{game}/{channel.viewers}
// and a fetch caller for {urlfetch}. Either may be nil, leaving that family
// unmounted exactly as an unwired dependency does on the command chain.
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

// timerFireStore builds a ValkeyTimerStore with only what fire touches: a
// recording publisher and the pipeline under test. No real Valkey client —
// fire (unlike tick) never reads one.
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

// decodeChat pulls the rendered chat text back out of one published outgress
// message, the same shape TestProcessChatEmittedToStandardLane decodes.
func decodeChat(t *testing.T, msg outgress.Message) string {
	t.Helper()
	var inner struct {
		Message string `json:"message"`
	}
	require.NoError(t, codec.Unmarshal(msg.Payload, &inner))
	return inner.Message
}

// TestFireExpandsTimerScopedTokensAndLeavesRestLiteral is the family test:
// a channel-scoped token ({uptime}) and a pure one ({random}) expand, while
// the message-family tokens a tick has no chatter to supply ({user}, the
// positional {1}) stay literal — the same "no mounted scope owns this name"
// outcome a typo gets, not an error.
func TestFireExpandsTimerScopedTokensAndLeavesRestLiteral(t *testing.T) {
	stream := &stubStreamInfo{byAddress: map[string]StreamInfoResult{"42/": liveNow()}}
	p := timerPipeline(stream, nil)
	store, pub := timerFireStore(p)

	store.fire(context.Background(), armedTimer{
		ref: timerRef{broadcasterID: 42, id: "t1"},
		def: timerDef{ID: "t1", Message: "{uptime} {random} {user} {1}", Interval: 60, Enabled: true},
	})

	require.Len(t, pub.got, 1)
	text := decodeChat(t, pub.got[0].msg)
	assert.Regexp(t, regexp.MustCompile(`^2 hours \d+ \{user\} \{1\}$`), text)
}

// TestFireNoTokenMessagePostsByteIdentical covers the cheap path: a message
// naming no {token} costs expandTimerText one Lex and is posted unchanged.
func TestFireNoTokenMessagePostsByteIdentical(t *testing.T) {
	p := timerPipeline(nil, nil)
	store, pub := timerFireStore(p)
	const msg = "hello chat, welcome to the stream!"

	store.fire(context.Background(), armedTimer{
		ref: timerRef{broadcasterID: 1, id: "t1"},
		def: timerDef{ID: "t1", Message: msg, Interval: 60, Enabled: true},
	})

	require.Len(t, pub.got, 1)
	assert.Equal(t, msg, decodeChat(t, pub.got[0].msg))
}

// TestFireFailedStreamLookupLeavesUptimeEmptyStillPosts is the degrade case:
// a scope that cannot answer its token (Channel.Plan never errors, by its own
// decision record — a failed read is already the empty answer its span
// renders) leaves that span empty rather than dropping the whole fire, the
// same "one broken dependency degrades its own tokens" contract
// scope.Chain.Plan documents for dispatch's own scopes.
func TestFireFailedStreamLookupLeavesUptimeEmptyStillPosts(t *testing.T) {
	stream := &stubStreamInfo{err: errors.New("boom")}
	p := timerPipeline(stream, nil)
	store, pub := timerFireStore(p)

	store.fire(context.Background(), armedTimer{
		ref: timerRef{broadcasterID: 7, id: "t1"},
		def: timerDef{ID: "t1", Message: "live: {uptime} go", Interval: 60, Enabled: true},
	})

	require.Len(t, pub.got, 1, "a failed lookup must never drop the timer")
	assert.Equal(t, "live:  go", decodeChat(t, pub.got[0].msg))
}

// TestFireUrlfetchResolvesThroughExternal proves {urlfetch:...} reaches the
// same scope.External a custom command's does, using the package's own
// fakeUrlFetch stub (urlfetch_test.go).
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
	assert.Equal(t, "now 72F", decodeChat(t, pub.got[0].msg))
	assert.Equal(t, 1, ff.calls())
}

// TestFireNilPipelinePostsRawByteIdentical is the pre-wiring degrade: no
// pipeline (main has not called WirePipeline yet, or a unit test that builds
// the struct literal directly, as timers_gate_test.go's fixture still does)
// means timerText/timerOutputs never touch Lex, chatLines or Translate —
// even TOKEN-SHAPED text posts exactly as saved, the same guarantee fire gave
// before this PR existed.
func TestFireNilPipelinePostsRawByteIdentical(t *testing.T) {
	store, pub := timerFireStore(nil)
	const msg = "hello {uptime} {random} raw — no chain wired yet"

	store.fire(context.Background(), armedTimer{
		ref: timerRef{broadcasterID: 3, id: "t1"},
		def: timerDef{ID: "t1", Message: msg, Interval: 60, Enabled: true},
	})

	require.Len(t, pub.got, 1)
	assert.Equal(t, msg, decodeChat(t, pub.got[0].msg))
}

// TestFireMultiLineFansOutAndCapsAtMaxResponseLines proves timerOutputs'
// reuse of chatLines: a blank line drops, and a message naming more than
// validate.MaxResponseLines (5) non-blank lines is truncated the same way a
// command response is — one outgress publish per surviving line, in order.
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
		got[i] = decodeChat(t, c.msg)
	}
	assert.Equal(t, []string{"line1", "line2", "line3", "line4", "line5"}, got)
}

// TestFireBlankExpansionWarnsOnceAndPublishesNothing covers a message that
// expands to nothing chat-visible: no outputs, the fire slot is still spent
// (tick's own recordFire/arm run regardless — fire only decides what to
// PUBLISH), and the warning is the one place that silence becomes visible.
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
	store.fire(context.Background(), at) // a second blank fire must not log twice (D16)

	assert.Empty(t, pub.got, "a blank expansion publishes nothing")
	warnings := logs.FilterMessage("timers: expansion left nothing to post, fire slot spent").All()
	require.Len(t, warnings, 1, "the warning logs once per timer, not once per fire")
}

// failingTimerScope is a scope.Scope whose Plan always errors, standing in
// for the case no real scope in this package can produce (Channel.Plan,
// Modules.Plan and External.Plan all degrade to an empty answer internally
// rather than erroring — see their own decision records) but Chain.Plan's
// onErr contract still has to be exercised against.
type failingTimerScope struct{ name string }

func (f failingTimerScope) Owns(v scope.Var) bool { return v.Name == f.name }
func (failingTimerScope) Plan(context.Context, []scope.Var) (scope.Values, error) {
	return nil, errors.New("boom")
}

// TestLogTimerScopeFailureDegradesToEmptyNotDropped exercises
// logTimerScopeFailure directly as a scope.Chain's onErr: a Plan failure
// degrades that scope's own tokens to empty (scope.Chain's own "one broken
// dependency" contract) and every other span in the same template still
// renders — proving the timer path is wired to the same onErr shape dispatch
// uses (engine/dispatch.go's logScopeFailure), not a dropped fire.
func TestLogTimerScopeFailureDegradesToEmptyNotDropped(t *testing.T) {
	p := &Pipeline{log: zap.NewNop()}
	chain := scope.Chain{failingTimerScope{name: "uptime"}, scope.Pure{}}
	toks := tmpl.Lex("stream: {uptime} dice: {random}")
	ref := timerRef{broadcasterID: 5, id: "t1"}

	values := chain.Plan(context.Background(), toks, p.logTimerScopeFailure(ref))
	out := string(chain.Render(nil, toks, values))

	assert.True(t, strings.HasPrefix(out, "stream:  dice: "), "the failed scope's own span renders empty: %q", out)
	assert.NotContains(t, out, "{uptime}", "a Plan error is not the same outcome as an unmounted scope")
}
