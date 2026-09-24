// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine/scope"
	"ItsBagelBot/app/twitch/sesame/module"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/tmpl"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

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

func dispatch(t *testing.T, p *Pipeline, c *module.Context) ([]module.Output, error) {
	t.Helper()
	var got []module.Output
	err := p.dispatchCommand(context.Background(), c, nil, func(o *module.Output) { got = append(got, *o) })
	return got, err
}

type recordFetcher struct{ asked []string }

func (r *recordFetcher) Fetch(_ context.Context, names []string) map[string]string {
	r.asked = append(r.asked, names...)
	out := make(map[string]string, len(names))
	for _, name := range names {
		out[name] = strings.ToUpper(name)
	}
	return out
}

func planFetches(t *testing.T, template string) []string {
	t.Helper()
	rec := &recordFetcher{}
	toks := tmpl.Lex(template)
	chain := scope.Chain{scope.External{Fetcher: rec, Max: maxUrlFetchTokens}}
	chain.Plan(context.Background(), toks, nil)
	return rec.asked
}

func TestUrlFetchScopePlansNames(t *testing.T) {
	tests := []struct {
		name string
		tmpl string
		want []string
	}{
		{"none", "plain {user} response", nil},
		{"single", "{urlfetch:temp}", []string{"temp"}},
		{"repeats collapse preserving first appearance", "{urlfetch:b} {urlfetch:a} {urlfetch:b}", []string{"b", "a"}},
		{"case-folded through the shared fold", "{urlfetch:Temp.Hum}", []string{"temp.hum"}},
		{"path payloads are distinct tokens", "{urlfetch:w.temp} {urlfetch:w.hum}", []string{"w.temp", "w.hum"}},
		{"bang stripped like counter names", "{urlfetch:!deaths}", []string{"deaths"}},
		{"empty payload skipped", "{urlfetch:} tail", nil},
		{"no payload at all skipped", "{urlfetch} tail", nil},
		{"unterminated brace opens no span", "head {urlfetch:w", nil},
		{"other token families untouched", "{counter:deaths} {choice:A,B} {random}", nil},
		{"nested braces", "x {urlfetch:{nested}} y", []string{"{nested"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, planFetches(t, tc.tmpl))
		})
	}
}

func TestUrlFetchScopeCapsFanOut(t *testing.T) {
	var b strings.Builder
	for i := range maxUrlFetchTokens + 3 {
		fmt.Fprintf(&b, "{urlfetch:d%d}", i)
	}
	assert.Len(t, planFetches(t, b.String()), maxUrlFetchTokens)

	rec := &recordFetcher{}
	got := renderScopes(nil, b.String(), scope.External{Fetcher: rec, Max: maxUrlFetchTokens})
	assert.Contains(t, got, "D0")
	assert.Contains(t, got, "{urlfetch:d"+strconv.Itoa(maxUrlFetchTokens)+"}")
}

func TestRenderUrlToken(t *testing.T) {
	fetched := stubFetcher{"temp": "72F", "temp.now": "72"}
	t.Run("resolved payload renders", func(t *testing.T) {
		assert.Equal(t, "72F", renderScopes(nil, "{urlfetch:temp}", urlScope(fetched)))
	})
	t.Run("payload folds like the plan", func(t *testing.T) {
		assert.Equal(t, "72", renderScopes(nil, "{URLFETCH:TEMP.NOW}", urlScope(fetched)))
	})
	t.Run("unresolved stays verbatim", func(t *testing.T) {
		assert.Equal(t, "x {urlfetch:missing} y",
			renderScopes(nil, "x {urlfetch:missing} y", urlScope(fetched)))
	})
	t.Run("unmounted scope leaves the token literal", func(t *testing.T) {
		assert.Equal(t, "{urlfetch:temp}", renderScopes(nil, "{urlfetch:temp}"))
	})
}

type stubFetcher map[string]string

func (s stubFetcher) Fetch(_ context.Context, names []string) map[string]string {
	out := make(map[string]string, len(names))
	for _, name := range names {
		if value, ok := s[name]; ok {
			out[name] = value
		}
	}
	return out
}

func urlScope(f scope.Fetcher) scope.External {
	return scope.External{Fetcher: f, Max: maxUrlFetchTokens}
}

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

type urlFetchCase struct {
	name  string
	reply gossiprpc.CustomFetchReply
	err   error
	want  string
}

func TestUrlFetchFailureTable(t *testing.T) {
	tests := []urlFetchCase{
		{name: "denied", reply: gossiprpc.CustomFetchReply{Status: gossiprpc.FetchDenied}, want: ""},
		{name: "limited", reply: gossiprpc.CustomFetchReply{Status: gossiprpc.FetchLimited}, want: ""},
		{name: "upstream_error", reply: gossiprpc.CustomFetchReply{Status: gossiprpc.FetchUpstreamError}, want: ""},
		{name: "timeout", reply: gossiprpc.CustomFetchReply{Status: gossiprpc.FetchTimeout}, want: ""},
		{name: "transport error", err: errors.New("nats: iotimeout"), want: ""},
		{name: "ok but nothing extracted", reply: gossiprpc.CustomFetchReply{Status: gossiprpc.FetchOK}, want: ""},
		{name: "bad_def stays verbatim", reply: gossiprpc.CustomFetchReply{Status: gossiprpc.FetchBadDef}, want: "{urlfetch:w}"},
		{
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

func TestUrlFetchFirstErrorCancelsBatch(t *testing.T) {
	ff := &fakeUrlFetch{
		replies: map[string]gossiprpc.CustomFetchReply{"fast": {Status: gossiprpc.FetchTimeout}},
		block:   map[string]time.Duration{"slow": 5 * time.Second},
	}
	p := urlFetchPipeline("{urlfetch:slow}-{urlfetch:fast}", ff, nil)

	start := time.Now()
	got, err := dispatch(t, p, chatCtx("!so", ""))
	require.NoError(t, err)
	require.Less(t, time.Since(start), 4*time.Second, "first failure must cancel the in-flight sibling")
	require.Len(t, got, 1)
	assert.Equal(t, "-", got[0].Text)
}

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
	assert.Equal(t, "got ", got[0].Text, "replay renders empty, not the value")
	assert.Equal(t, 1, ff.calls(), "replay must not re-fetch")
}

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
	assert.Equal(t, "got ", got[0].Text)

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

func TestUrlFetchCooldownClaimsBeforeAnyFetch(t *testing.T) {
	cd := &denySecondCooldown{}
	ff := &fakeUrlFetch{replies: map[string]gossiprpc.CustomFetchReply{
		"w": {Status: gossiprpc.FetchOK, Values: []string{"v"}},
	}}
	p := urlFetchPipeline("got {urlfetch:w}", ff, func(d *Deps) {
		d.Cooldown = cd
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

func TestUrlFetchWithoutCallerLeavesTokensVisible(t *testing.T) {
	p := urlFetchPipeline("x {urlfetch:w}", nil, nil)
	got, err := dispatch(t, p, chatCtx("!so", ""))
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "x {urlfetch:w}", got[0].Text)
}

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
