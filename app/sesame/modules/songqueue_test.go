// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"errors"
	"testing"
	"time"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/outgress"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// fakeSongQueue is an in-memory SongQueueStore: a current pointer plus the
// ordered pending line, mirroring the real store's rules.
type fakeSongQueue struct {
	current *engine.SongEntry
	up      []engine.SongEntry
}

func (f *fakeSongQueue) Add(_ context.Context, _ uint64, e engine.SongEntry, maxDepth int) (int, error) {
	for i := range f.up {
		if f.up[i].RequesterID == e.RequesterID {
			return 0, engine.ErrSongAlreadyQueued
		}
	}
	if len(f.up) >= maxDepth {
		return 0, engine.ErrSongQueueFull
	}
	e.EnqueuedAt = time.Now().UnixMilli()
	f.up = append(f.up, e)
	return len(f.up), nil
}

func (f *fakeSongQueue) RetractOwn(_ context.Context, _ uint64, requesterID string) (engine.SongEntry, bool, error) {
	for i := len(f.up) - 1; i >= 0; i-- {
		if f.up[i].RequesterID == requesterID {
			out := f.up[i]
			out.Position = i + 1
			f.up = append(f.up[:i], f.up[i+1:]...)
			return out, true, nil
		}
	}
	return engine.SongEntry{}, false, nil
}

func (f *fakeSongQueue) RemoveAt(_ context.Context, _ uint64, position int) (engine.SongEntry, bool, error) {
	if position < 1 || position > len(f.up) {
		return engine.SongEntry{}, false, nil
	}
	i := position - 1
	out := f.up[i]
	out.Position = i + 1
	f.up = append(f.up[:i], f.up[i+1:]...)
	return out, true, nil
}

func (f *fakeSongQueue) Advance(_ context.Context, _ uint64) (*engine.SongEntry, *engine.SongEntry, error) {
	if len(f.up) == 0 {
		out := f.current
		f.current = nil
		return out, nil, nil
	}
	head := f.up[0]
	prev := f.current
	f.current = &head
	f.up = f.up[1:]
	return prev, &head, nil
}

func (f *fakeSongQueue) Clear(_ context.Context, _ uint64) error {
	f.current = nil
	f.up = nil
	return nil
}

func (f *fakeSongQueue) Snapshot(_ context.Context, _ uint64, upNext int) (engine.SongQueueSnapshot, error) {
	snap := engine.SongQueueSnapshot{Current: f.current}
	n := upNext
	if n < 0 || n > len(f.up) {
		n = len(f.up)
	}
	snap.UpNext = make([]engine.SongEntry, n)
	copy(snap.UpNext, f.up[:n])
	for i := range snap.UpNext {
		snap.UpNext[i].Position = i + 1
	}
	return snap, nil
}

func songDeps(store engine.SongQueueStore, g engine.GossipCaller) engine.Deps {
	return engine.Deps{SongQueue: store, Gossip: g, Log: zap.NewNop()}
}

// songCtx builds a chat Context for broadcaster 100 with the given chatter.
// badges carries Twitch badge set_ids ("moderator", "broadcaster", ...).
func songCtx(chatterID, login string, badges ...string) *module.Context {
	env := lane.Envelope{
		Type:                 "channel.chat.message",
		BroadcasterUserID:    "100",
		BroadcasterUserLogin: "streamer",
		ChatterUserID:        chatterID,
		ChatterUserLogin:     login,
	}
	for _, b := range badges {
		env.Badges = append(env.Badges, lane.Badge{SetID: b})
	}
	return &module.Context{Env: env, BroadcasterID: 100, Log: zap.NewNop()}
}

func runSR(t *testing.T, m module.Module, c *module.Context, args string) []module.Output {
	t.Helper()
	cmd := findCmd(t, m, "sr")
	var col collector
	require.NoError(t, cmd.Run(context.Background(), c, args, col.emit))
	return col.out
}

func srTrack(id, name, artist string) gossiprpc.SpotifyTrack {
	return gossiprpc.SpotifyTrack{ID: id, Name: name, Artists: []string{artist}, DurationMS: 222000}
}

func srSearchGossip(tracks ...gossiprpc.SpotifyTrack) *fakeGossip {
	return &fakeGossip{replies: map[string]any{
		"spotify.search": gossiprpc.SpotifySearchReply{Tracks: tracks, ResolvedAs: "text"},
	}}
}

func chatText(t *testing.T, out []module.Output) string {
	t.Helper()
	require.NotEmpty(t, out)
	require.Equal(t, outgress.TypeChat, out[0].Type)
	return out[0].Text
}

func TestSRAddsResolvedTrack(t *testing.T) {
	g := srSearchGossip(srTrack("t1", "Mr. Brightside", "The Killers"))
	store := &fakeSongQueue{}
	m := SongQueue(songDeps(store, g))

	out := runSR(t, m, songCtx("42", "alice"), "brightside by the killers")

	call := g.lastCall(t)
	assert.Equal(t, "spotify", call.provider)
	assert.Equal(t, "search", call.endpoint)
	assert.Equal(t, "100", call.req.ChannelID, "the broadcaster id scopes gossip's per-channel credential")
	assert.Equal(t, "brightside by the killers", call.req.Query)
	assert.Equal(t, 1, call.req.Limit)

	require.Len(t, store.up, 1)
	entry := store.up[0]
	assert.Equal(t, "t1", entry.TrackID)
	assert.Equal(t, "Mr. Brightside", entry.Title)
	assert.Equal(t, []string{"The Killers"}, entry.Artists)
	assert.Equal(t, "42", entry.RequesterID, "retract authorization keys on the twitch user id")
	assert.Equal(t, "alice", entry.RequesterName)
	assert.NotZero(t, entry.EnqueuedAt)

	text := chatText(t, out)
	assert.Contains(t, text, "@alice")
	assert.Contains(t, text, "Mr. Brightside")
	assert.Contains(t, text, "#1")
}

func TestSRAlreadyQueuedRejectsSecondRequest(t *testing.T) {
	g := srSearchGossip(srTrack("t2", "Human", "The Killers"))
	store := &fakeSongQueue{}
	m := SongQueue(songDeps(store, g))
	c := songCtx("42", "alice")

	runSR(t, m, c, "human")
	out := runSR(t, m, c, "another one")

	assert.Len(t, store.up, 1, "a second live request must not displace the first")
	assert.Contains(t, chatText(t, out), "!sr remove")
}

func TestSRProviderErrorSurfacesVerbatim(t *testing.T) {
	g := &fakeGossip{replies: map[string]any{
		"spotify.search": gossiprpc.SpotifySearchReply{Error: "no Spotify connection on file"},
	}}
	m := SongQueue(songDeps(&fakeSongQueue{}, g))

	out := runSR(t, m, songCtx("42", "alice"), "whatever")
	assert.Contains(t, chatText(t, out), "no Spotify connection on file")
}

func TestSRTransportErrorStaysGeneric(t *testing.T) {
	g := &fakeGossip{err: errors.New("connection reset")}
	m := SongQueue(songDeps(&fakeSongQueue{}, g))

	out := runSR(t, m, songCtx("42", "alice"), "whatever")
	assert.Contains(t, chatText(t, out), "music lookup is down")
}

func TestSRNoResultsIsFriendly(t *testing.T) {
	g := srSearchGossip()
	m := SongQueue(songDeps(&fakeSongQueue{}, g))

	out := runSR(t, m, songCtx("42", "alice"), "zzzz")
	assert.Contains(t, chatText(t, out), "no track found")
}

func TestSRRetractTouchesOnlyOwnLatest(t *testing.T) {
	g := srSearchGossip(
		srTrack("tA", "Song A", "Artist"),
		srTrack("tB", "Song B", "Artist"),
	)
	store := &fakeSongQueue{}
	m := SongQueue(songDeps(store, g))

	runSR(t, m, songCtx("1", "alice"), "song a")
	runSR(t, m, songCtx("2", "bob"), "song b")

	out := runSR(t, m, songCtx("1", "alice"), "remove")
	require.Len(t, store.up, 1, "only alice's request goes")
	assert.Equal(t, "2", store.up[0].RequesterID)
	assert.Contains(t, chatText(t, out), "Song A")

	out = runSR(t, m, songCtx("1", "alice"), "retract")
	assert.Contains(t, chatText(t, out), "don't have a queued song")
}

// A viewer typing a number must never remove someone else's entry: it falls
// back to their own retract; only mods get positional reach.
func TestSRRemoveNumberIsModOnlyPositional(t *testing.T) {
	g := srSearchGossip(
		srTrack("t1", "One", "A"),
		srTrack("t2", "Two", "B"),
		srTrack("t3", "Three", "C"),
	)
	store := &fakeSongQueue{}
	m := SongQueue(songDeps(store, g))
	runSR(t, m, songCtx("1", "alice"), "one")
	runSR(t, m, songCtx("2", "bob"), "two")
	runSR(t, m, songCtx("3", "carol"), "three")

	out := runSR(t, m, songCtx("3", "carol", "moderator"), "remove 2")
	require.Len(t, store.up, 2)
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "#2", "the mod removed position #2")
	assert.Contains(t, out[0].Text, "bob", "the confirmation names whose entry went")

	// Non-mod with a number: their OWN latest retracts, nobody else's.
	runSR(t, m, songCtx("3", "carol"), "remove 1")
	require.Len(t, store.up, 1)
	assert.Equal(t, "1", store.up[0].RequesterID, "carol's own remaining entry went, not alice's #1")
}

func TestSRNextIsModOnlyAndPromotesHead(t *testing.T) {
	g := srSearchGossip(srTrack("t1", "One", "A"))
	store := &fakeSongQueue{}
	m := SongQueue(songDeps(store, g))
	runSR(t, m, songCtx("42", "alice"), "one")

	out := runSR(t, m, songCtx("99", "randoviewer"), "next")
	assert.Empty(t, out, "mod verbs typed by a non-mod are silently ignored")
	assert.Nil(t, store.current)

	out = runSR(t, m, songCtx("7", "modder", "moderator"), "next")
	require.NotNil(t, store.current)
	assert.Equal(t, "One", store.current.Title)
	assert.Empty(t, store.up)
	assert.Contains(t, chatText(t, out), "Now playing")
	assert.Contains(t, chatText(t, out), "alice")

	out = runSR(t, m, songCtx("7", "modder", "moderator"), "next")
	assert.Nil(t, store.current)
	assert.Contains(t, chatText(t, out), "empty")
}

func TestSRClearIsModOnly(t *testing.T) {
	g := srSearchGossip(srTrack("t1", "One", "A"))
	store := &fakeSongQueue{}
	m := SongQueue(songDeps(store, g))
	runSR(t, m, songCtx("42", "alice"), "one")

	runSR(t, m, songCtx("99", "viewer"), "clear")
	assert.Len(t, store.up, 1)

	runSR(t, m, songCtx("7", "modder", "moderator"), "clear")
	assert.Empty(t, store.up)
	assert.Nil(t, store.current)
}

func TestSRViewShowsNowPlayingAndUpNext(t *testing.T) {
	g := srSearchGossip(srTrack("t1", "One", "A"))
	m := SongQueue(songDeps(&fakeSongQueue{}, g))

	out := runSR(t, m, songCtx("42", "alice"), "")
	assert.Contains(t, chatText(t, out), "Nothing queued")

	store := &fakeSongQueue{}
	g2 := srSearchGossip(srTrack("t1", "One", "A"), srTrack("t2", "Two", "B"))
	m2 := SongQueue(songDeps(store, g2))
	runSR(t, m2, songCtx("1", "alice"), "one")
	runSR(t, m2, songCtx("2", "bob"), "two")
	runSR(t, m2, songCtx("7", "modder", "moderator"), "next")

	out = runSR(t, m2, songCtx("9", "viewer"), "")
	text := chatText(t, out)
	assert.Contains(t, text, "Now playing: One (asked by alice)")
	assert.Contains(t, text, "1. One (by bob)", "bob's pending request renders with its requester")
}

func TestSRUnknownWordsAreQueriesNotVerbs(t *testing.T) {
	// Unknown words are QUERIES, not subcommands: "!sr Next Episode by Dr. Dre"
	// is a song called Next..., not a mod verb.
	g := srSearchGossip(srTrack("tn", "Next Episode", "Dr. Dre"))
	store := &fakeSongQueue{}
	m := SongQueue(songDeps(store, g))

	runSR(t, m, songCtx("42", "alice"), "Next Episode by Dr. Dre")
	require.Len(t, store.up, 1)
	assert.Equal(t, "tn", store.up[0].TrackID)
}

func TestSRIsInertWithoutStoreOrGossip(t *testing.T) {
	m := SongQueue(engine.Deps{Log: zap.NewNop()})
	out := runSR(t, m, songCtx("42", "alice"), "anything")
	assert.Empty(t, out, "an unwired module stays inert")

	// Wired store but no gossip caller: adds cannot resolve, stay silent.
	m2 := SongQueue(songDeps(&fakeSongQueue{}, nil))
	out = runSR(t, m2, songCtx("42", "alice"), "anything")
	assert.Empty(t, out)
}

// songDepsLive is songDeps with the live-state stub the gate cases need.
func songDepsLive(store engine.SongQueueStore, g engine.GossipCaller, live bool) engine.Deps {
	d := songDeps(store, g)
	d.Live = &fakeLive{live: live}
	return d
}

// The chat path's two switches: the enable/perm gate, and the govee-shaped
// live gate. A legacy blob carries neither key and must keep queueing, or
// enabling the module would have silently stopped working under the channels
// already using it.
func TestSRPathGates(t *testing.T) {
	cases := []struct {
		name   string
		config string
		live   bool
		queued bool
		says   string // the refusal chat says why; empty when the add lands
	}{
		{"disabled path says it is off", `{"sr":{"enabled":false,"perm":"everyone"}}`, true, false, "turned off"},
		{"perm tier says it is limited", `{"sr":{"enabled":true,"perm":"mod"}}`, true, false, "smaller group"},
		{"live-only says it is offline", `{"sr":{"enabled":true,"perm":"everyone","allowOffline":false}}`, false, false, "while the stream is live"},
		{"allowOffline queues while offline", `{"sr":{"enabled":true,"perm":"everyone","allowOffline":true}}`, false, true, ""},
		{"legacy blob keeps queueing", `{"maxDepth":10}`, false, true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := srSearchGossip(srTrack("t1", "Mr. Brightside", "The Killers"))
			store := &fakeSongQueue{}
			c := songCtx("42", "alice")
			c.Config = []byte(tc.config)

			out := runSR(t, SongQueue(songDepsLive(store, g, tc.live)), c, "brightside")
			if !tc.queued {
				assert.Empty(t, store.up)
				// A closed gate must not spend a Spotify lookup either.
				assert.Empty(t, g.calls)
				// ...but it must say why: silence reads as a broken bot.
				assert.Contains(t, chatText(t, out), tc.says)
				return
			}
			require.Len(t, store.up, 1)
			assert.Contains(t, chatText(t, out), "Mr. Brightside")
		})
	}
}

const songRedeemJSON = `{"id":"redeem-1","broadcaster_user_id":"100","user_id":"42","user_name":"Alice","user_login":"alice","user_input":"brightside","reward":{"id":"rw-sr","title":"Song request","cost":500}}`

func songRedeemCtx(config string) *module.Context {
	return &module.Context{
		Env:           lane.Envelope{Type: redemptionAddType, Event: []byte(songRedeemJSON)},
		BroadcasterID: 100,
		Config:        []byte(config),
		Log:           zap.NewNop(),
	}
}

// The channel-points path. Every refusal refunds: a viewer who spent points
// and got no song back is the one outcome worse than not having the feature.
// A redemption of any other reward is not this module's business at all.
func TestSongRedeemGates(t *testing.T) {
	cases := []struct {
		name   string
		config string
		live   bool
		want   string // queued | refund | ignored
	}{
		{"offline refunds", `{"redeem":{"enabled":true,"rewardId":"rw-sr","onRedeem":"fulfill"}}`, false, "refund"},
		{"allowOffline queues", `{"redeem":{"enabled":true,"rewardId":"rw-sr","onRedeem":"fulfill","allowOffline":true}}`, false, "queued"},
		{"disabled path refunds", `{"redeem":{"enabled":false,"rewardId":"rw-sr","onRedeem":"fulfill"}}`, true, "refund"},
		{"another reward is ignored", `{"redeem":{"enabled":true,"rewardId":"rw-other","onRedeem":"fulfill"}}`, true, "ignored"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeSongQueue{}
			m := SongQueue(songDepsLive(store, srSearchGossip(srTrack("t1", "Mr. Brightside", "The Killers")), tc.live))
			h := m.Events[redemptionAddType]
			require.NotNil(t, h)

			var col collector
			require.NoError(t, h(context.Background(), songRedeemCtx(tc.config), col.emit))
			assertSongRedeem(t, tc.want, store, col.out)
		})
	}
}

func assertSongRedeem(t *testing.T, want string, store *fakeSongQueue, out []module.Output) {
	t.Helper()
	switch want {
	case "queued":
		require.Len(t, store.up, 1)
		require.NotEmpty(t, out)
		assert.Equal(t, outgress.RedemptionFulfilled, out[len(out)-1].Status)
	case "refund":
		assert.Empty(t, store.up)
		assertRefund(t, out)
	default:
		assert.Empty(t, out)
		assert.Empty(t, store.up)
	}
}
