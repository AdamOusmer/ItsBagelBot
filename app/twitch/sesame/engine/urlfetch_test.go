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

	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/internal/projection"

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

func (f *fakeUrlFetch) request(t *testing.T, i int) gossiprpc.Request {
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

func fetchOK(values ...string) gossiprpc.CustomFetchReply {
	return gossiprpc.CustomFetchReply{Status: gossiprpc.FetchOK, Values: values}
}

func TestCustomUrlFetchRequestShape(t *testing.T) {
	cases := []struct {
		name        string
		lane        string
		wantPremium bool
	}{
		{"the standard lane rides the standard bucket", "standard", false},
		{"the premium lane rides along", "premium", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ff := &fakeUrlFetch{replies: map[string]gossiprpc.CustomFetchReply{"w.t": fetchOK("72F")}}
			p := urlFetchPipeline("now {urlfetch:w.t} / later {urlfetch:w.t}", ff, nil)
			env := chatEnv("!so", "")
			env.MsgID, env.Lane = "m1", tc.lane

			got, err := runChat(t, p, env)

			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Equal(t, "now 72F / later 72F", got[0].Text)
			assert.Equal(t, 1, ff.calls(), "same-payload repeats resolve once")
			req := ff.request(t, 0)
			assert.Equal(t, "w.t", req.DefID)
			assert.Equal(t, "123", req.ChannelID)
			assert.Equal(t, tc.wantPremium, req.IsPremium)
			assert.False(t, req.DryRun, "the chat path never dry-runs")
			assert.False(t, req.Fresh, "the chat path prefers gossip's cached bytes")
		})
	}
}

func TestUrlFetchRendersFailuresAndHostileValues(t *testing.T) {
	cases := []struct {
		name     string
		response string
		reply    gossiprpc.CustomFetchReply
		err      error
		unwired  bool
		want     string
	}{
		{name: "denied", reply: gossiprpc.CustomFetchReply{Status: gossiprpc.FetchDenied}, want: "out: "},
		{name: "limited", reply: gossiprpc.CustomFetchReply{Status: gossiprpc.FetchLimited}, want: "out: "},
		{name: "upstream_error", reply: gossiprpc.CustomFetchReply{Status: gossiprpc.FetchUpstreamError}, want: "out: "},
		{name: "timeout", reply: gossiprpc.CustomFetchReply{Status: gossiprpc.FetchTimeout}, want: "out: "},
		{name: "transport error", err: errors.New("nats: iotimeout"), want: "out: "},
		{name: "ok but nothing extracted", reply: fetchOK(), want: "out: "},
		{
			name: "an empty value renders the span's fallback", response: "out: {urlfetch:w|down}",
			reply: fetchOK(), want: "out: down",
		},
		{name: "bad_def stays verbatim", reply: gossiprpc.CustomFetchReply{Status: gossiprpc.FetchBadDef}, want: "out: {urlfetch:w}"},
		{name: "no caller wired leaves the tokens visible", unwired: true, want: "out: {urlfetch:w}"},
		{name: "hostile value sanitized", reply: fetchOK("//ban @everyone"), want: "out: ban @everyone"},
		{
			name:  "value capped rune-safe at the variable boundary",
			reply: fetchOK(strings.Repeat("é", 80)),
			want:  "out: " + strings.Repeat("é", MaxExternalVarBytes/2),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ff := &fakeUrlFetch{replies: map[string]gossiprpc.CustomFetchReply{"w": tc.reply}}
			if tc.err != nil {
				ff.errs = map[string]error{"w": tc.err}
			}
			var caller UrlFetchCaller = ff
			if tc.unwired {
				caller = nil
			}
			response := tc.response
			if response == "" {
				response = "out: {urlfetch:w}"
			}

			got, err := runChat(t, urlFetchPipeline(response, caller, nil), chatEnv("!so", ""))

			require.NoError(t, err, "fallback text keeps the handler off the retry lane")
			require.Len(t, got, 1)
			assert.Equal(t, tc.want, got[0].Text)
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
	got, err := runChat(t, p, chatEnv("!so", ""))
	require.NoError(t, err)
	require.Less(t, time.Since(start), 4*time.Second, "first failure must cancel the in-flight sibling")
	require.Len(t, got, 1)
	assert.Equal(t, "-", got[0].Text)
}

func TestUrlFetchReplayDoesNotRefetch(t *testing.T) {
	store := newRecordingStore()
	ff := &fakeUrlFetch{replies: map[string]gossiprpc.CustomFetchReply{"w": fetchOK("v")}}
	p := urlFetchPipeline("got {urlfetch:w}", ff, func(d *Deps) {
		d.Dedup = NewEventDedup(store, "sesame:seen:", time.Minute, zap.NewNop())
	})

	env := chatEnv("!so", "")
	env.MsgID = "evt-1"

	got, err := runChat(t, p, env)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "got v", got[0].Text)

	got, err = runChat(t, p, env)
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

	env := chatEnv("!so", "")
	env.MsgID = "evt-1"

	got, err := runChat(t, p, env)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "got ", got[0].Text)

	ff.mu.Lock()
	ff.errs = nil
	ff.replies = map[string]gossiprpc.CustomFetchReply{"w": fetchOK("fresh")}
	ff.mu.Unlock()

	got, err = runChat(t, p, env)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "got fresh", got[0].Text, "released claim lets the retry fetch")
	assert.Equal(t, 2, ff.calls())
}

type denySecondCooldown struct {
	NoopCooldown
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
	ff := &fakeUrlFetch{replies: map[string]gossiprpc.CustomFetchReply{"w": fetchOK("v")}}
	p := urlFetchPipeline("got {urlfetch:w}", ff, func(d *Deps) {
		d.Cooldown = cd
		d.Proj = fakeReader{cmd: projection.Command{
			Name: "so", Response: "got {urlfetch:w}", IsActive: true, Perm: "everyone", Cooldown: 30,
		}, cmdFound: true}
	})

	first := chatEnv("!so", "")
	first.MsgID = "a"
	got, err := runChat(t, p, first)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, 1, ff.calls())

	second := chatEnv("!so", "")
	second.MsgID = "b"
	got, err = runChat(t, p, second)
	require.NoError(t, err)
	assert.Empty(t, got, "cooldown refusal emits nothing")
	assert.Equal(t, 1, ff.calls(), "the gate blocks before any fetch")
}

func TestUrlFetchScanCapBackstop(t *testing.T) {
	replies := make(map[string]gossiprpc.CustomFetchReply, maxUrlFetchTokens)
	parts := make([]string, 0, maxUrlFetchTokens+2)
	want := make([]string, 0, maxUrlFetchTokens+2)
	for i := 0; i < maxUrlFetchTokens+2; i++ {
		name := "n" + string(rune('0'+i))
		parts = append(parts, "{urlfetch:"+name+"}")
		if i < maxUrlFetchTokens {
			replies[name] = fetchOK("v" + name)
			want = append(want, "v"+name)
		} else {
			want = append(want, "{urlfetch:"+name+"}")
		}
	}
	ff := &fakeUrlFetch{replies: replies}
	p := urlFetchPipeline(strings.Join(parts, " "), ff, nil)

	got, err := runChat(t, p, chatEnv("!so", ""))
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, strings.Join(want, " "), got[0].Text)
	assert.Equal(t, maxUrlFetchTokens, ff.calls())
}
