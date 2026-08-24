// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/app/sesame/module"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// fakeUrlFetch records Fetch calls and answers each DefID with a canned reply;
// an entry in errs short-circuits, and block sleeps (ctx-aware) before
// answering so a test can exercise cancellation. A DefID with no canned reply
// answers bad_def — the natural "definition missing" default.
type fakeUrlFetch struct {
	mu      sync.Mutex
	reqs    []gossiprpc.Request
	replies map[string]gossiprpc.CustomFetchReply
	errs    map[string]error
	block   map[string]time.Duration
}

func (f *fakeUrlFetch) Fetch(ctx context.Context, req gossiprpc.Request) (gossiprpc.CustomFetchReply, error) {
	f.mu.Lock()
	f.reqs = append(f.reqs, req)
	reply, known := f.replies[req.DefID]
	err := f.errs[req.DefID]
	block := f.block[req.DefID]
	f.mu.Unlock()

	if block > 0 {
		select {
		case <-time.After(block):
		case <-ctx.Done():
			return gossiprpc.CustomFetchReply{}, ctx.Err()
		}
	}
	if err != nil {
		return gossiprpc.CustomFetchReply{}, err
	}
	if !known {
		return gossiprpc.CustomFetchReply{Status: gossiprpc.FetchBadDef}, nil
	}
	return reply, nil
}

func (f *fakeUrlFetch) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.reqs)
}

func (f *fakeUrlFetch) call(t *testing.T, i int) gossiprpc.Request {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	require.Greater(t, len(f.reqs), i)
	return f.reqs[i]
}

// urlFetchPipeline serves one custom command whose response is resp, with the
// given fetch caller wired; mut adjusts the Deps (dedup, cooldown, ...) before
// construction.
func urlFetchPipeline(resp string, ff UrlFetchCaller, mut func(*Deps)) *Pipeline {
	d := Deps{
		Proj:        fakeReader{cmd: projection.Command{Name: "so", Response: resp, IsActive: true, Perm: "everyone"}, cmdFound: true},
		Live:        liveAlways{},
		Cooldown:    NoopCooldown{},
		Pub:         &fakePublisher{},
		Log:         zap.NewNop(),
		CustomFetch: ff,
	}
	if mut != nil {
		mut(&d)
	}
	return NewPipeline(d, NewRegistry(zap.NewNop()), Config{OutgressPremium: premiumSubj, OutgressStandard: standardSubj})
}

// dispatch runs the command stage, returning what was emitted.
func dispatch(t *testing.T, p *Pipeline, c *module.Context) ([]module.Output, error) {
	t.Helper()
	var got []module.Output
	err := p.dispatchCommand(context.Background(), c, nil, func(o *module.Output) { got = append(got, *o) })
	return got, err
}

// --- scan grammar ---

func TestUrlFetchNamesScan(t *testing.T) {
	tests := []struct {
		name string
		tmpl string
		want []string
	}{
		{"none", "plain {user} response", nil},
		{"single", "{urlfetch:temp}", []string{"temp"}},
		{"repeats collapse preserving first appearance", "{urlfetch:b} {urlfetch:a} {urlfetch:b}", []string{"b", "a"}},
		// The sigil itself is matched case-sensitively, byte-for-byte like the
		// counter scan (Index on the lowercase prefix); the PAYLOAD folds
		// through NormalizeCounterName so {urlfetch:Temp.Hum} scans as
		// "temp.hum" — the exact key expandCommand looks up.
		{"case-folded through the counter fold", "{urlfetch:Temp.Hum}", []string{"temp.hum"}},
		{"path payloads are distinct tokens", "{urlfetch:w.temp} {urlfetch:w.hum}", []string{"w.temp", "w.hum"}},
		{"bang stripped like counter names", "{urlfetch:!deaths}", []string{"deaths"}},
		{"empty payload skipped", "{urlfetch:} tail", nil},
		{"unterminated brace ignored", "head {urlfetch:w", nil},
		{"other token families untouched", "{counter:deaths} {choice:A,B} {random}", nil},
		// The scan takes the FIRST closing brace, exactly like closeBrace does
		// at expansion time — so this payload scans as "{nested" AND that is
		// the key expandCommand would look up, keeping scan and expansion in
		// agreement even on malformed input (the token then stays verbatim).
		{"nested braces", "x {urlfetch:{nested}} y", []string{"{nested"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, urlFetchNames(tc.tmpl))
		})
	}
}

// --- injection point ---

func TestExpandCommandUrlToken(t *testing.T) {
	t.Run("resolved payload renders", func(t *testing.T) {
		buf := expandCommand(nil, "{urlfetch:temp}", tokens{urls: map[string]string{"temp": "72F"}})
		assert.Equal(t, "72F", string(buf))
	})
	t.Run("payload folds like the scan", func(t *testing.T) {
		buf := expandCommand(nil, "{URLFETCH:TEMP.NOW}", tokens{urls: map[string]string{"temp.now": "72"}})
		assert.Equal(t, "72", string(buf))
	})
	t.Run("unresolved stays verbatim", func(t *testing.T) {
		buf := expandCommand(nil, "x {urlfetch:missing} y", tokens{urls: map[string]string{}})
		assert.Equal(t, "x {urlfetch:missing} y", string(buf))
	})
	// Sanitize/cap coverage lives at the boundary that owns it: resolveUrlToken
	// passes every fetched value through ExternalVar BEFORE it enters the map
	// this expansion reads (see TestUrlFetchFailureTable).
}

// --- end to end through runCustom ---

func TestCustomUrlFetchResolvesOnceAndExpands(t *testing.T) {
	ff := &fakeUrlFetch{replies: map[string]gossiprpc.CustomFetchReply{
		"w.t": {Status: gossiprpc.FetchOK, Values: []string{"72F"}},
	}}
	p := urlFetchPipeline("now {urlfetch:w.t} / later {urlfetch:w.t}", ff, nil)

	c := chatCtx("!so", "")
	c.Env.MsgID = "m1"
	got, err := dispatch(t, p, c)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "now 72F / later 72F", got[0].Text)
	assert.Equal(t, 1, ff.calls(), "same-payload repeats resolve once")

	req := ff.call(t, 0)
	assert.Equal(t, "w.t", req.DefID)
	assert.Equal(t, "123", req.ChannelID)
	assert.False(t, req.IsPremium, "standard lane rides the standard bucket")
	assert.False(t, req.DryRun, "the chat path never dry-runs")
	assert.False(t, req.Fresh, "the chat path prefers gossip's cached bytes")
}

func TestCustomUrlFetchPremiumRidesAlong(t *testing.T) {
	ff := &fakeUrlFetch{replies: map[string]gossiprpc.CustomFetchReply{
		"w": {Status: gossiprpc.FetchOK, Values: []string{"v"}},
	}}
	p := urlFetchPipeline("got {urlfetch:w}", ff, nil)

	c := chatCtx("!so", "")
	c.Regress = module.RegressPremium
	got, err := dispatch(t, p, c)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "got v", got[0].Text)
	assert.True(t, ff.call(t, 0).IsPremium)
}

// urlFetchCase is one failure-table row: the canned reply (or transport error)
// gossip answers with, and the static text that must render.
type urlFetchCase struct {
	name  string
	reply gossiprpc.CustomFetchReply
	err   error
	want  string
}

// TestUrlFetchFailureTable pins the failure-semantics mapping: every non-ok
// outcome renders static authored text (never upstream content) and still
// returns a nil handler error, so nothing lands on the retry lane.
func TestUrlFetchFailureTable(t *testing.T) {
	tests := []urlFetchCase{
		{name: "denied", reply: gossiprpc.CustomFetchReply{Status: gossiprpc.FetchDenied}, want: urlFetchUnavailableText},
		{name: "limited", reply: gossiprpc.CustomFetchReply{Status: gossiprpc.FetchLimited}, want: urlFetchUnavailableText},
		{name: "upstream_error", reply: gossiprpc.CustomFetchReply{Status: gossiprpc.FetchUpstreamError}, want: urlFetchErrorText},
		{name: "timeout", reply: gossiprpc.CustomFetchReply{Status: gossiprpc.FetchTimeout}, want: urlFetchTimeoutText},
		{name: "transport error", err: errors.New("nats: iotimeout"), want: urlFetchTimeoutText},
		{name: "ok but nothing extracted", reply: gossiprpc.CustomFetchReply{Status: gossiprpc.FetchOK}, want: urlFetchErrorText},
		{name: "bad_def stays verbatim", reply: gossiprpc.CustomFetchReply{Status: gossiprpc.FetchBadDef}, want: "{urlfetch:w}"},
		{
			// Leading slash run trimmed at the variable boundary: a hostile
			// upstream cannot mint a "/ban ..." verb through the per-line
			// split. (Control-character stripping lands with the emit-side
			// guard; this row pins only what ExternalVar guarantees here.)
			name:  "hostile value sanitized",
			reply: gossiprpc.CustomFetchReply{Status: gossiprpc.FetchOK, Values: []string{"//ban @everyone"}},
			want:  "ban @everyone",
		},
		{
			name:  "value capped rune-safe at the variable boundary",
			reply: gossiprpc.CustomFetchReply{Status: gossiprpc.FetchOK, Values: []string{strings.Repeat("é", 80)}},
			want:  strings.Repeat("é", MaxExternalVarBytes/2),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ff := &fakeUrlFetch{replies: map[string]gossiprpc.CustomFetchReply{"w": tc.reply}}
			if tc.err != nil {
				ff.errs = map[string]error{"w": tc.err}
			}
			p := urlFetchPipeline("out: {urlfetch:w}", ff, nil)

			got, err := dispatch(t, p, chatCtx("!so", ""))
			require.NoError(t, err, "fallback text keeps the handler off the retry lane")
			require.Len(t, got, 1)
			assert.Equal(t, "out: "+tc.want, got[0].Text)
		})
	}
}

// TestUrlFetchFirstErrorCancelsBatch proves errgroup-style cancellation: a
// typed timeout on one token cancels the in-flight sibling mid-request, whose
// transport failure then renders the same timeout-family text — the whole
// fan-out finishes in well under the slow sibling's block instead of waiting
// it out.
func TestUrlFetchFirstErrorCancelsBatch(t *testing.T) {
	ff := &fakeUrlFetch{
		replies: map[string]gossiprpc.CustomFetchReply{"fast": {Status: gossiprpc.FetchTimeout}},
		block:   map[string]time.Duration{"slow": 5 * time.Second},
	}
	p := urlFetchPipeline("{urlfetch:slow} {urlfetch:fast}", ff, nil)

	start := time.Now()
	got, err := dispatch(t, p, chatCtx("!so", ""))
	require.NoError(t, err)
	require.Less(t, time.Since(start), 4*time.Second, "first failure must cancel the in-flight sibling")
	require.Len(t, got, 1)
	assert.Equal(t, "[source timed out] [source timed out]", got[0].Text)
}

// TestUrlFetchReplayDoesNotRefetch pins redelivery safety: the same event
// identity claiming twice renders fallback text on the replay without a second
// network call, so a quorum-loss redelivery never burns fetch quota twice.
func TestUrlFetchReplayDoesNotRefetch(t *testing.T) {
	store := newRecordingStore()
	ff := &fakeUrlFetch{replies: map[string]gossiprpc.CustomFetchReply{
		"w": {Status: gossiprpc.FetchOK, Values: []string{"v"}},
	}}
	p := urlFetchPipeline("got {urlfetch:w}", ff, func(d *Deps) {
		d.Dedup = NewEventDedup(store, "sesame:seen:", time.Minute, zap.NewNop())
	})

	c := chatCtx("!so", "")
	c.Env.MsgID = "evt-1"

	got, err := dispatch(t, p, c)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "got v", got[0].Text)

	got, err = dispatch(t, p, c)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "got "+urlFetchUnavailableText, got[0].Text, "replay renders fallback, not the value")
	assert.Equal(t, 1, ff.calls(), "replay must not re-fetch")
}

// TestUrlFetchFailedFetchReleasesClaimForRedelivery mirrors claimedCounterValue:
// a fetch that produced no fresh value releases its claim, so a later delivery
// of the same event retries instead of being stuck on fallback forever.
func TestUrlFetchFailedFetchReleasesClaimForRedelivery(t *testing.T) {
	store := newRecordingStore()
	ff := &fakeUrlFetch{errs: map[string]error{"w": errors.New("connection reset")}}
	p := urlFetchPipeline("got {urlfetch:w}", ff, func(d *Deps) {
		d.Dedup = NewEventDedup(store, "sesame:seen:", time.Minute, zap.NewNop())
	})

	c := chatCtx("!so", "")
	c.Env.MsgID = "evt-1"

	got, err := dispatch(t, p, c)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "got "+urlFetchTimeoutText, got[0].Text)

	ff.mu.Lock()
	ff.errs = nil
	ff.replies = map[string]gossiprpc.CustomFetchReply{"w": {Status: gossiprpc.FetchOK, Values: []string{"fresh"}}}
	ff.mu.Unlock()

	got, err = dispatch(t, p, c)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "got fresh", got[0].Text, "released claim lets the retry fetch")
	assert.Equal(t, 2, ff.calls())
}

// denySecondCooldown allows exactly one claim, then gates everything else.
type denySecondCooldown struct {
	mu      sync.Mutex
	allowed int
}

func (c *denySecondCooldown) Allow(context.Context, string, time.Duration) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.allowed++
	return c.allowed == 1, nil
}

// TestUrlFetchCooldownClaimsBeforeAnyFetch pins the gate order: the cooldown
// window is consumed by the gate BEFORE the fan-out runs, so neither a
// succeeding nor a failing definition can be hot-looped faster than its
// cooldown — the second invocation is refused having made zero calls.
func TestUrlFetchCooldownClaimsBeforeAnyFetch(t *testing.T) {
	cd := &denySecondCooldown{}
	ff := &fakeUrlFetch{replies: map[string]gossiprpc.CustomFetchReply{
		"w": {Status: gossiprpc.FetchOK, Values: []string{"v"}},
	}}
	p := urlFetchPipeline("got {urlfetch:w}", ff, func(d *Deps) {
		d.Cooldown = cd
		// The command row itself carries the cooldown window; the gate reads
		// it from the projection, so the fixture row must set one.
		d.Proj = fakeReader{cmd: projection.Command{
			Name: "so", Response: "got {urlfetch:w}", IsActive: true, Perm: "everyone", Cooldown: 30,
		}, cmdFound: true}
	})

	first := chatCtx("!so", "")
	first.Env.MsgID = "a"
	got, err := dispatch(t, p, first)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, 1, ff.calls())

	second := chatCtx("!so", "")
	second.Env.MsgID = "b"
	got, err = dispatch(t, p, second)
	require.NoError(t, err)
	assert.Empty(t, got, "cooldown refusal emits nothing")
	assert.Equal(t, 1, ff.calls(), "the gate blocks before any fetch")
}

// TestUrlFetchWithoutCallerLeavesTokensVisible: no caller wired is the kill
// switch — tokens render verbatim like every other unknown token.
func TestUrlFetchWithoutCallerLeavesTokensVisible(t *testing.T) {
	p := urlFetchPipeline("x {urlfetch:w}", nil, nil)
	got, err := dispatch(t, p, chatCtx("!so", ""))
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "x {urlfetch:w}", got[0].Text)
}

// TestUrlFetchScanCapBackstop proves the emit-side bound: a row carrying more
// distinct payloads than validation allows fans out only the capped prefix;
// the rest stay verbatim.
func TestUrlFetchScanCapBackstop(t *testing.T) {
	replies := make(map[string]gossiprpc.CustomFetchReply, maxUrlFetchTokens)
	parts := make([]string, 0, maxUrlFetchTokens+2)
	want := make([]string, 0, maxUrlFetchTokens+2)
	for i := 0; i < maxUrlFetchTokens+2; i++ {
		name := "n" + string(rune('0'+i))
		parts = append(parts, "{urlfetch:"+name+"}")
		if i < maxUrlFetchTokens {
			replies[name] = gossiprpc.CustomFetchReply{Status: gossiprpc.FetchOK, Values: []string{"v" + name}}
			want = append(want, "v"+name)
		} else {
			want = append(want, "{urlfetch:"+name+"}")
		}
	}
	ff := &fakeUrlFetch{replies: replies}
	p := urlFetchPipeline(strings.Join(parts, " "), ff, nil)

	got, err := dispatch(t, p, chatCtx("!so", ""))
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, strings.Join(want, " "), got[0].Text)
	assert.Equal(t, maxUrlFetchTokens, ff.calls())
}
