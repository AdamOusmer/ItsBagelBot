// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type fakeSongQueue struct {
	current *engine.SongEntry
	up      []engine.SongEntry
}

func (f *fakeSongQueue) Add(_ context.Context, _ uint64, e engine.SongEntry, limits engine.SongQueueLimits) (int, error) {
	if limits.PerRequester > 0 {
		mine := 0
		for i := range f.up {
			if f.up[i].RequesterID == e.RequesterID {
				mine++
			}
		}
		if mine >= limits.PerRequester {
			return 0, engine.ErrSongQuotaReached
		}
	}
	if limits.MaxDepth > 0 && len(f.up) >= limits.MaxDepth {
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

func (f *fakeSongQueue) SyncPlaying(_ context.Context, _ uint64, trackID string) (bool, error) {
	if trackID == "" {
		return false, nil
	}
	if f.current != nil && f.current.TrackID == trackID {
		return false, nil
	}
	for i := range f.up {
		if f.up[i].TrackID == trackID {
			entry := f.up[i]
			f.current = &entry
			f.up = f.up[i+1:]
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeSongQueue) SyncQueue(ctx context.Context, id uint64, player engine.PlayerQueueIDs) (bool, error) {
	changed := f.syncQueueCurrent(ctx, id, player.CurrentID)
	if len(player.UpcomingIDs) == 0 {
		return changed, nil
	}
	return f.dropSkippedBefore(player.UpcomingIDs[0]) || changed, nil
}

func (f *fakeSongQueue) syncQueueCurrent(ctx context.Context, id uint64, currentID string) bool {
	if currentID == "" {
		return f.clearCurrent()
	}
	changed, _ := f.SyncPlaying(ctx, id, currentID)
	if changed {
		return true
	}
	if f.current != nil && f.current.TrackID != currentID {
		return f.clearCurrent()
	}
	return false
}

func (f *fakeSongQueue) clearCurrent() bool {
	if f.current == nil {
		return false
	}
	f.current = nil
	return true
}

func (f *fakeSongQueue) dropSkippedBefore(firstID string) bool {
	for i, entry := range f.up {
		if entry.TrackID != firstID {
			continue
		}
		if i == 0 {
			return false
		}
		f.up = f.up[i:]
		return true
	}
	return false
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

func srTrack(id, name, artist string) gossiprpc.SpotifyTrack {
	return gossiprpc.SpotifyTrack{ID: id, Name: name, Artists: []string{artist}, DurationMS: 222000}
}

func srSearchGossip(tracks ...gossiprpc.SpotifyTrack) *fakeGossip {
	return &fakeGossip{replies: map[string]any{
		"spotify.search": gossiprpc.SpotifySearchReply{Tracks: tracks, ResolvedAs: "text"},
		"spotify.queue":  gossiprpc.SpotifyPlayerReply{},
		"spotify.next":   gossiprpc.SpotifyPlayerReply{},
	}}
}

func nowPlayingGossip(reply gossiprpc.SpotifyNowPlayingReply) *fakeGossip {
	return &fakeGossip{replies: map[string]any{"spotify.nowplaying": reply}}
}

func playing(tr gossiprpc.SpotifyTrack) gossiprpc.SpotifyNowPlayingReply {
	return gossiprpc.SpotifyNowPlayingReply{IsPlaying: true, Track: &tr}
}

func chatText(t *testing.T, out []module.Output) string {
	t.Helper()
	require.NotEmpty(t, out)
	require.Equal(t, outgress.TypeChat, out[0].Type)
	return out[0].Text
}

func entry(id, title, requesterID, requester string) engine.SongEntry {
	return engine.SongEntry{TrackID: id, Title: title, RequesterID: requesterID, RequesterName: requester}
}

func TestSongRequestAddsResolvedTrack(t *testing.T) {
	g := srSearchGossip(srTrack("t1", "Mr. Brightside", "The Killers"))
	store := &fakeSongQueue{}
	out := runChat(t, SongQueue(songDeps(store, g)), chatCtx("42", "alice"), "!sr brightside by the killers")
	require.Len(t, g.calls, 4)
	search, queueRead, sync, push := g.calls[0], g.calls[1], g.calls[2], g.calls[3]
	assert.Equal(t, "playerqueue", queueRead.endpoint)
	assert.Equal(t, "nowplaying", sync.endpoint)
	assert.Equal(t, "spotify", search.provider)
	assert.Equal(t, "search", search.endpoint)
	assert.Equal(t, "100", search.req.ChannelID, "the broadcaster id scopes gossip's per-channel credential")
	assert.Equal(t, "brightside by the killers", search.req.Query)
	assert.Equal(t, 1, search.req.Limit)
	assert.Equal(t, "queue", push.endpoint)
	assert.Equal(t, "t1", push.req.TrackID)
	assert.Equal(t, "100", push.req.ChannelID)
	require.Len(t, store.up, 1)
	got := store.up[0]
	assert.NotZero(t, got.EnqueuedAt)
	got.EnqueuedAt = 0
	assert.Equal(t, engine.SongEntry{
		TrackID: "t1", Title: "Mr. Brightside", Artists: []string{"The Killers"}, DurationMS: 222000,
		RequesterID: "42", RequesterName: "alice",
	}, got, "retract authorization keys on the twitch user id")
	text := chatText(t, out)
	assert.Contains(t, text, "@alice")
	assert.Contains(t, text, "Mr. Brightside")
	assert.Contains(t, text, "#1")
}

type srCase struct {
	name      string
	text      string
	badges    []string
	chatter   string
	config    string
	live      liveState
	repeat    int
	store     fakeSongQueue
	replies   map[string]any
	drop      string
	gossipErr error
	noTracks  bool
	noStore   bool
	noGossip  bool
	silent    bool
	noCall    bool
	queued    int
	current   string
	contains  []string
	excludes  []string
}

func (tc srCase) deps(store *fakeSongQueue, g *fakeGossip) engine.Deps {
	d := songDeps(store, g)
	d.Live = tc.live.store()
	if tc.noStore {
		d.SongQueue = nil
	}
	if tc.noGossip {
		d.Gossip = nil
	}
	return d
}

func (tc srCase) gossip() *fakeGossip {
	g := srSearchGossip(srTrack("t1", "Mr. Brightside", "The Killers"))
	if tc.noTracks {
		g = srSearchGossip()
	}
	for route, reply := range tc.replies {
		g.replies[route] = reply
	}
	delete(g.replies, tc.drop)
	g.err = tc.gossipErr
	return g
}

func TestSongRequest(t *testing.T) {
	cases := []srCase{
		{name: "a second request queues without a per viewer cap by default", text: "!sr human", repeat: 2, queued: 2, contains: []string{"#2"}},
		{name: "everyone is capped by the everyone quota", config: `{"quotas":{"everyone":1}}`, repeat: 2, queued: 1, contains: []string{"limit (1"}},
		{name: "subs are capped by the sub quota", config: `{"quotas":{"everyone":1,"sub":2}}`, badges: []string{"subscriber"}, repeat: 3, queued: 2, contains: []string{"limit (2"}},
		{name: "a missing mod quota means unlimited", config: `{"quotas":{"everyone":1}}`, badges: []string{"moderator"}, repeat: 3, queued: 3},
		{name: "the broadcaster is never capped", config: `{"quotas":{"everyone":1,"mod":1}}`, chatter: "100", badges: []string{"broadcaster"}, repeat: 3, queued: 3},
		{name: "a player refusal rolls the add back and says why", queued: 0,
			replies:  map[string]any{"spotify.queue": gossiprpc.SpotifyPlayerReply{Error: "no active Spotify device, start playing something first"}},
			contains: []string{"no active Spotify device"}},
		{name: "a player transport failure rolls the add back without claiming a position", drop: "spotify.queue", queued: 0, excludes: []string{"#1"}},
		{name: "a provider error surfaces verbatim", replies: map[string]any{"spotify.search": gossiprpc.SpotifySearchReply{Error: "no Spotify connection on file"}},
			contains: []string{"no Spotify connection on file"}},
		{name: "a transport error stays generic", gossipErr: errors.New("connection reset"), contains: []string{"music lookup is down"}},
		{name: "no results is friendly", text: "!sr zzzz", noTracks: true, contains: []string{"no track found"}},
		{name: "the played head reconciles before the position is chosen", text: "!sr human", queued: 1, current: "t1", contains: []string{"#1"},
			store:   fakeSongQueue{up: []engine.SongEntry{entry("t1", "Played Already", "7", "bob")}},
			replies: map[string]any{"spotify.nowplaying": playing(srTrack("t1", "Played Already", "Someone"))}},
		{name: "an unwired store keeps the module inert", noStore: true, silent: true},
		{name: "an unwired gossip keeps the module inert", noGossip: true, silent: true},
		{name: "a disabled path says it is off", config: `{"sr":{"enabled":false,"perm":"everyone"}}`, live: liveOnline, noCall: true, contains: []string{"turned off"}},
		{name: "a perm tier says it is limited", config: `{"sr":{"enabled":true,"perm":"mod"}}`, live: liveOnline, noCall: true, contains: []string{"smaller group"}},
		{name: "live only says it is offline", config: `{"sr":{"enabled":true,"perm":"everyone","allowOffline":false}}`, live: liveOffline, noCall: true, contains: []string{"while the stream is live"}},
		{name: "allowOffline queues while offline", config: `{"sr":{"enabled":true,"perm":"everyone","allowOffline":true}}`, live: liveOffline, queued: 1, contains: []string{"Mr. Brightside"}},
		{name: "a legacy blob keeps queueing", config: `{"maxDepth":10}`, live: liveOffline, queued: 1, contains: []string{"Mr. Brightside"}},
		{name: "a partial sr record keeps queueing", config: `{"sr":{"perm":"everyone"}}`, live: liveOnline, queued: 1, contains: []string{"Mr. Brightside"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := tc.store
			g := tc.gossip()
			who := tc.chatter
			if who == "" {
				who = "42"
			}
			text := tc.text
			if text == "" {
				text = "!sr brightside"
			}
			m := SongQueue(tc.deps(&store, g))
			var out []module.Output
			for i := range max(tc.repeat, 1) {
				out = runChat(t, m, withConfig(chatCtx(who, "alice", tc.badges...), tc.config), fmt.Sprintf("%s %d", text, i))
			}
			if tc.silent {
				assert.Empty(t, out)
				return
			}
			assertText(t, chatText(t, out), textWant{"", tc.contains, tc.excludes})
			assert.Len(t, store.up, tc.queued)
			if tc.current != "" {
				require.NotNil(t, store.current)
				assert.Equal(t, tc.current, store.current.TrackID)
			}
			if tc.noCall {
				assert.Empty(t, g.calls)
			}
		})
	}
}

func TestSongRequestRetractTouchesOnlyOwnLatest(t *testing.T) {
	g := srSearchGossip(srTrack("tA", "Song A", "Artist"), srTrack("tB", "Song B", "Artist"))
	store := &fakeSongQueue{}
	m := SongQueue(songDeps(store, g))
	runChat(t, m, chatCtx("1", "alice"), "!sr song a")
	runChat(t, m, chatCtx("2", "bob"), "!sr song b")

	out := runChat(t, m, chatCtx("1", "alice"), "!sr remove")
	require.Len(t, store.up, 1, "only alice's request goes")
	assert.Equal(t, "2", store.up[0].RequesterID)
	assert.Contains(t, chatText(t, out), "Song A")

	out = runChat(t, m, chatCtx("1", "alice"), "!sr retract")
	assert.Contains(t, chatText(t, out), "don't have a queued song")
}

func TestSongRequestRemoveNumberIsModOnlyPositional(t *testing.T) {
	store := &fakeSongQueue{up: []engine.SongEntry{entry("t1", "One", "1", "alice"), entry("t2", "Two", "2", "bob"), entry("t3", "Three", "3", "carol")}}
	m := SongQueue(songDeps(store, srSearchGossip()))

	out := runChat(t, m, chatCtx("3", "carol", "moderator"), "!sr remove 2")
	require.Len(t, store.up, 2)
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "#2", "the mod removed position #2")
	assert.Contains(t, out[0].Text, "bob", "the confirmation names whose entry went")

	runChat(t, m, chatCtx("3", "carol"), "!sr remove 1")
	require.Len(t, store.up, 1)
	assert.Equal(t, "1", store.up[0].RequesterID, "carol's own remaining entry went, not alice's #1")
}

func TestSongRequestNextIsModOnlyAndPromotesHead(t *testing.T) {
	store := &fakeSongQueue{up: []engine.SongEntry{entry("t1", "One", "42", "alice")}}
	g := srSearchGossip()
	m := SongQueue(songDeps(store, g))

	out := runChat(t, m, chatCtx("99", "randoviewer"), "!sr next")
	assert.Empty(t, out, "mod verbs typed by a non-mod are silently ignored")
	assert.Nil(t, store.current)

	setSkipSnapshots(g,
		gossiprpc.SpotifyQueueReply{Current: &gossiprpc.SpotifyTrack{ID: "external"}, UpNext: []gossiprpc.SpotifyTrack{{ID: "t1"}}},
		gossiprpc.SpotifyQueueReply{Current: &gossiprpc.SpotifyTrack{ID: "t1"}})
	out = runChat(t, m, chatCtx("7", "modder", "moderator"), "!sr next")
	require.NotNil(t, store.current)
	assert.Equal(t, "One", store.current.Title)
	assert.Empty(t, store.up)
	assertText(t, chatText(t, out), textWant{"", []string{"Now playing", "alice"}, nil})

	setSkipSnapshots(g,
		gossiprpc.SpotifyQueueReply{Current: &gossiprpc.SpotifyTrack{ID: "t1"}},
		gossiprpc.SpotifyQueueReply{Current: &gossiprpc.SpotifyTrack{ID: "external"}})
	out = runChat(t, m, chatCtx("7", "modder", "moderator"), "!sr next")
	assert.Nil(t, store.current)
	assert.Contains(t, chatText(t, out), "empty")
}

func TestSongRequestClearIsModOnly(t *testing.T) {
	store := &fakeSongQueue{up: []engine.SongEntry{entry("t1", "One", "42", "alice")}}
	m := SongQueue(songDeps(store, srSearchGossip()))
	runChat(t, m, chatCtx("99", "viewer"), "!sr clear")
	assert.Len(t, store.up, 1)
	runChat(t, m, chatCtx("7", "modder", "moderator"), "!sr clear")
	assert.Empty(t, store.up)
	assert.Nil(t, store.current)
}

func TestSongRequestViewShowsNowPlayingAndUpNext(t *testing.T) {
	g := srSearchGossip()
	m := SongQueue(songDeps(&fakeSongQueue{}, g))
	assert.Contains(t, chatText(t, runChat(t, m, chatCtx("42", "alice"), "!sr")), "Nothing queued")

	store := &fakeSongQueue{up: []engine.SongEntry{entry("t1", "One", "1", "alice"), entry("t2", "Two", "2", "bob")}}
	m = SongQueue(songDeps(store, g))
	setSkipSnapshots(g,
		gossiprpc.SpotifyQueueReply{Current: &gossiprpc.SpotifyTrack{ID: "external"}, UpNext: []gossiprpc.SpotifyTrack{{ID: "t1"}, {ID: "t2"}}},
		gossiprpc.SpotifyQueueReply{Current: &gossiprpc.SpotifyTrack{ID: "t1"}, UpNext: []gossiprpc.SpotifyTrack{{ID: "t2"}}})
	runChat(t, m, chatCtx("7", "modder", "moderator"), "!sr next")
	text := chatText(t, runChat(t, m, chatCtx("9", "viewer"), "!sr"))
	assert.Contains(t, text, "Now playing: One (asked by alice)")
	assert.Contains(t, text, "1. Two (by bob)", "bob's pending request renders with its requester")
}

func TestSRViewRenumbersAfterSpotifySkippedPendingTrack(t *testing.T) {
	store := &fakeSongQueue{up: []engine.SongEntry{entry("a", "Skipped", "", "alice"), entry("b", "Survivor", "", "bob")}}
	g := srSearchGossip()
	g.replies["spotify.playerqueue"] = gossiprpc.SpotifyQueueReply{
		Current: &gossiprpc.SpotifyTrack{ID: "external"},
		UpNext:  []gossiprpc.SpotifyTrack{{ID: "b"}},
	}
	text := chatText(t, runChat(t, SongQueue(songDeps(store, g)), chatCtx("9", "viewer"), "!sr"))
	assert.Contains(t, text, "1. Survivor (by bob)")
	assert.NotContains(t, text, "Skipped")
}

func TestSongRequestUnknownWordsAreQueriesNotVerbs(t *testing.T) {
	store := &fakeSongQueue{}
	g := srSearchGossip(srTrack("tn", "Next Episode", "Dr. Dre"))
	runChat(t, SongQueue(songDeps(store, g)), chatCtx("42", "alice"), "!sr Next Episode by Dr. Dre")
	require.Len(t, store.up, 1)
	assert.Equal(t, "tn", store.up[0].TrackID)
}

func TestSongQueueCommands(t *testing.T) {
	queue := []engine.SongEntry{entry("t1", "One", "42", "alice"), entry("t2", "Two", "2", "b")}
	six := make([]engine.SongEntry, 0, 6)
	for i := 1; i <= 6; i++ {
		id := strconv.Itoa(i)
		six = append(six, entry("t"+id, "Song "+id, id, "v"+id))
	}
	cases := []struct {
		name     string
		text     string
		chatter  string
		badges   []string
		config   string
		store    fakeSongQueue
		gossip   *fakeGossip
		contains []string
		excludes []string
		queued   int
	}{
		{name: "song falls back to the queue when nothing plays", text: "!song", gossip: nowPlayingGossip(gossiprpc.SpotifyNowPlayingReply{}),
			contains: []string{"Nothing queued"}},
		{name: "song reads the live player, not the queue", text: "!song", gossip: nowPlayingGossip(playing(srTrack("t9", "Unrequested", "Some Artist"))),
			contains: []string{"Unrequested", "Some Artist"}, excludes: []string{"Nothing queued"}},
		{name: "current credits the matching requester", text: "!current", gossip: nowPlayingGossip(playing(srTrack("t1", "One", "A"))),
			store:    fakeSongQueue{current: &engine.SongEntry{TrackID: "t1", Title: "One", Artists: []string{"A"}, RequesterID: "7", RequesterName: "alice"}},
			contains: []string{"alice"}},
		{name: "a broadcaster started track has no requester to credit", text: "!np", gossip: nowPlayingGossip(playing(srTrack("t2", "Two", "B"))),
			store:    fakeSongQueue{current: &engine.SongEntry{TrackID: "t1", Title: "One", Artists: []string{"A"}, RequesterID: "7", RequesterName: "alice"}},
			contains: []string{"Two"}, excludes: []string{"alice"}},
		{name: "song surfaces the provider reason", text: "!song", gossip: nowPlayingGossip(gossiprpc.SpotifyNowPlayingReply{Error: "no Spotify app set up for this channel"}),
			contains: []string{"no Spotify app set up"}},
		{name: "song keeps transport errors generic", text: "!song", gossip: &fakeGossip{err: errors.New("connection reset")},
			contains: []string{"music lookup is down"}},
		{name: "song honours the broadcaster's template", text: "!song", config: `{"currentMessage":"jamming to {title} right now"}`,
			gossip: nowPlayingGossip(playing(srTrack("t1", "One", "A"))), contains: []string{"jamming to One right now"}},
		{name: "srlist shows five deep", text: "!songlist", chatter: "42", store: fakeSongQueue{up: six}, gossip: srSearchGossip(),
			contains: []string{"Song 5"}, excludes: []string{"Song 6"}, queued: 6},
		{name: "clear empties the queue", text: "!clear", chatter: "9", badges: []string{"moderator"}, store: fakeSongQueue{up: queue}, gossip: srSearchGossip(),
			contains: []string{"cleared"}},
		{name: "remove retracts the chatter's own request", text: "!remove", store: fakeSongQueue{up: []engine.SongEntry{entry("t1", "Mine", "42", "alice")}}, gossip: srSearchGossip(),
			contains: []string{"Mine"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := tc.store
			who := tc.chatter
			if who == "" {
				who = "42"
			}
			out := runChat(t, SongQueue(songDeps(&store, tc.gossip)), withConfig(chatCtx(who, "alice", tc.badges...), tc.config), tc.text)
			assertText(t, chatText(t, out), textWant{"", tc.contains, tc.excludes})
			assert.Len(t, store.up, tc.queued)
		})
	}
}

func TestSongCommandDegradesToNowPlayingWhenGossipLacksPlayerQueue(t *testing.T) {
	g := &fakeGossip{replies: map[string]any{
		"spotify.playerqueue": gossiprpc.SpotifyQueueReply{Error: "unknown endpoint"},
		"spotify.nowplaying":  playing(srTrack("t9", "Unrequested", "Some Artist")),
	}}
	out := runChat(t, SongQueue(songDeps(&fakeSongQueue{}, g)), chatCtx("42", "alice"), "!song")
	require.Len(t, g.calls, 2)
	assert.Equal(t, "playerqueue", g.calls[0].endpoint, "an old gossip is asked for the queue snapshot first")
	assert.Equal(t, "nowplaying", g.calls[1].endpoint, "the queue read's unknown-endpoint error falls back to the live player")
	assertText(t, chatText(t, out), textWant{"", []string{"Unrequested", "Some Artist"}, nil})
}

func TestSongCommandReadsTheLivePlayerScopedToTheChannel(t *testing.T) {
	g := nowPlayingGossip(playing(srTrack("t9", "Unrequested", "Some Artist")))
	runChat(t, SongQueue(songDeps(&fakeSongQueue{}, g)), chatCtx("42", "alice"), "!song")
	call := g.lastCall(t)
	assert.Equal(t, "spotify", call.provider)
	assert.Equal(t, "nowplaying", call.endpoint)
	assert.Equal(t, "100", call.req.ChannelID, "the broadcaster id scopes gossip's per-channel credential")
}
