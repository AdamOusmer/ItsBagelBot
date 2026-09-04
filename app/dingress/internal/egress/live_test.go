// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package egress

import (
	"context"
	"strconv"
	"sync"
	"testing"

	discapi "ItsBagelBot/internal/discordapi"
	ddiscord "ItsBagelBot/internal/domain/discord"
	eventtwitch "ItsBagelBot/internal/domain/event/twitch"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

// guildRecorder is a discordGuildAPI that captures every call a handler
// fires, mirroring outgress's own test recorder
// (app/outgress/internal/worker/discord_live_test.go /discord_setup_test.go).
type guildRecorder struct {
	mu        sync.Mutex
	lastChan  string
	embeds    []ddiscord.Embed
	edits     []string
	roles     []string
	channels  []discapi.Snowflake
	createdCh []string
	createdRo []string
	panels    []string
	nextID    int
}

func (r *guildRecorder) nextSnowflake(prefix string) string {
	r.nextID++
	return prefix + strconv.Itoa(r.nextID)
}

func (r *guildRecorder) SendChat(context.Context, discapi.ChatPost) error { return nil }

func (r *guildRecorder) SendEmbed(_ context.Context, post discapi.EmbedPost) (discapi.Message, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastChan = post.ChannelID
	r.embeds = append(r.embeds, post.Embed)
	return discapi.Message{ChannelID: post.ChannelID, ID: r.nextSnowflake("msg-")}, nil
}

func (r *guildRecorder) SendPanel(_ context.Context, post discapi.EmbedPost, buttons []discapi.Button) (discapi.Message, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastChan = post.ChannelID
	r.embeds = append(r.embeds, post.Embed)
	for _, btn := range buttons {
		r.panels = append(r.panels, btn.CustomID)
	}
	return discapi.Message{ChannelID: post.ChannelID, ID: r.nextSnowflake("panel-")}, nil
}

func (r *guildRecorder) EditMessage(_ context.Context, _ discapi.Message, patch discapi.MessagePatch) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.edits = append(r.edits, patch.Content)
	return nil
}

func (r *guildRecorder) CreateChannel(_ context.Context, ch discapi.GuildChannel) (discapi.Snowflake, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := discapi.Snowflake{ID: r.nextSnowflake("ch-"), Name: ch.Spec.Name, Type: ch.Spec.Type}
	r.channels = append(r.channels, out)
	r.createdCh = append(r.createdCh, ch.Spec.Name)
	return out, nil
}

func (r *guildRecorder) DeleteChannel(context.Context, discapi.Snowflake) error { return nil }

func (r *guildRecorder) CreateRole(_ context.Context, role discapi.GuildRole) (discapi.Snowflake, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.createdRo = append(r.createdRo, role.Spec.Name)
	return discapi.Snowflake{ID: r.nextSnowflake("role-"), Name: role.Spec.Name}, nil
}

func (r *guildRecorder) AddMemberRole(_ context.Context, role discapi.MemberRole) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.roles = append(r.roles, "add:"+role.RoleID)
	return nil
}

func (r *guildRecorder) RemoveMemberRole(_ context.Context, role discapi.MemberRole) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.roles = append(r.roles, "remove:"+role.RoleID)
	return nil
}

func (r *guildRecorder) ListGuildChannels(context.Context, discapi.Guild) ([]discapi.Snowflake, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]discapi.Snowflake, len(r.channels))
	copy(out, r.channels)
	return out, nil
}

func (r *guildRecorder) ListGuildRoles(context.Context, discapi.Guild) ([]discapi.Snowflake, error) {
	return []discapi.Snowflake{{ID: "guild-1", Name: "@everyone"}}, nil
}

func (r *guildRecorder) GetGuild(_ context.Context, guild discapi.Guild) (discapi.Snowflake, error) {
	return discapi.Snowflake{ID: guild.ID, Name: "test"}, nil
}

var _ discordGuildAPI = (*guildRecorder)(nil)

// memLiveStore is a map-backed liveStore, mirroring outgress's memLiveStore.
type memLiveStore struct {
	mu            sync.Mutex
	msg           discapi.Message
	guild, twitch string
}

func (s *memLiveStore) PutLiveMessage(_ context.Context, _ liveMsgKey, m discapi.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msg = m
	return nil
}

func (s *memLiveStore) GetLiveMessage(_ context.Context, _ liveMsgKey) (discapi.Message, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.msg.ChannelID == "" || s.msg.ID == "" {
		return s.msg, false
	}
	return s.msg, true
}

func (s *memLiveStore) DeleteLiveMessage(context.Context, liveMsgKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msg = discapi.Message{}
	return nil
}

func (s *memLiveStore) PutGuild(_ context.Context, req GuildSetupRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.guild, s.twitch = req.GuildID, req.BroadcasterID
	return nil
}

func (s *memLiveStore) GetGuild(_ context.Context, req GuildSetupRequest) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.guild != req.GuildID || s.twitch == "" {
		return "", false
	}
	return s.twitch, true
}

func (s *memLiveStore) DeleteGuild(_ context.Context, req GuildSetupRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.guild == req.GuildID {
		s.guild, s.twitch = "", ""
	}
	return nil
}

func (s *memLiveStore) PutTicketDesk(context.Context, discapi.Guild) error { return nil }

var _ liveStore = (*memLiveStore)(nil)

// memModules is a map-backed discordModuleReader, mirroring outgress's
// memModules.
type memModules struct {
	cfg     ddiscord.Config
	enabled bool
	found   bool
}

func (m memModules) GetModule(_ context.Context, _ uint64, name string) (projection.ModuleView, bool, error) {
	if name != ddiscord.ModuleName || !m.found {
		return projection.ModuleView{}, false, nil
	}
	raw, err := codec.Marshal(m.cfg)
	if err != nil {
		return projection.ModuleView{}, false, err
	}
	return projection.ModuleView{Name: name, IsEnabled: m.enabled, Configs: raw}, true, nil
}

func liveWorker(guild *guildRecorder, kv liveStore, mods discordModuleReader) *Worker {
	return New(Config{Discord: guild, DiscordKV: kv, DiscordMods: mods, Log: zap.NewNop()})
}

func TestAnnounceDiscordLivePostsEmbed(t *testing.T) {
	guild := &guildRecorder{}
	kv := &memLiveStore{}
	mods := memModules{found: true, enabled: true, cfg: ddiscord.Config{GuildID: "g1", LiveChannelID: "now-live", TwitchLogin: "streamer"}}
	w := liveWorker(guild, kv, mods)

	w.announceDiscordLive(context.Background(), eventtwitch.StreamStatus{BroadcasterID: 42, Live: true})
	if len(guild.embeds) != 1 {
		t.Fatalf("embeds = %d, want 1", len(guild.embeds))
	}
	if guild.lastChan != "now-live" {
		t.Fatalf("channel = %q", guild.lastChan)
	}
	if kv.msg.ChannelID != "now-live" || kv.msg.ID == "" {
		t.Fatal("live message was not stored")
	}
}

func TestAnnounceDiscordLiveRespectsCategoryAllow(t *testing.T) {
	guild := &guildRecorder{}
	mods := memModules{found: true, enabled: true, cfg: ddiscord.Config{GuildID: "g1", LiveChannelID: "now-live", TwitchLogin: "streamer", CategoryAllow: "Minecraft"}}
	w := liveWorker(guild, &memLiveStore{}, mods)
	// No stream-info store means GameName is empty, which an allow-list rejects.
	w.announceDiscordLive(context.Background(), eventtwitch.StreamStatus{BroadcasterID: 42, Live: true})
	if len(guild.embeds) != 0 {
		t.Fatal("empty category must not post when an allow-list is set")
	}
}

func TestAnnounceDiscordOfflineEditsLiveMessage(t *testing.T) {
	guild := &guildRecorder{}
	kv := &memLiveStore{msg: discapi.Message{ChannelID: "now-live", ID: "msg-9"}}
	mods := memModules{found: true, enabled: true, cfg: ddiscord.Config{GuildID: "g1", LiveChannelID: "now-live", TwitchLogin: "streamer"}}
	w := liveWorker(guild, kv, mods)
	w.announceDiscordLive(context.Background(), eventtwitch.StreamStatus{BroadcasterID: 42, Live: false})
	if len(guild.edits) != 1 {
		t.Fatalf("edits = %v, want 1", guild.edits)
	}
	if guild.edits[0] != ddiscord.OfflineContent {
		t.Fatalf("edits = %v, want the offline line", guild.edits)
	}
}

func TestHandleStreamEventOnlinePostsDiscord(t *testing.T) {
	guild := &guildRecorder{}
	kv := &memLiveStore{}
	mods := memModules{found: true, enabled: true, cfg: ddiscord.Config{GuildID: "g1", LiveChannelID: "now-live", TwitchLogin: "streamer"}}
	w := liveWorker(guild, kv, mods)
	payload := []byte(`{"subscription":{"type":"stream.online"},"event":{"broadcaster_user_id":"42"}}`)
	if err := w.HandleStreamEvent(&bus.Message{Payload: payload}); err != nil {
		t.Fatalf("HandleStreamEvent: %v", err)
	}
	if len(guild.embeds) != 1 {
		t.Fatal("stream.online must post the go-live embed on this lane")
	}
}

func TestHandleStreamEventOfflineStillAnnouncesDiscord(t *testing.T) {
	guild := &guildRecorder{}
	kv := &memLiveStore{msg: discapi.Message{ChannelID: "now-live", ID: "msg-1"}}
	mods := memModules{found: true, enabled: true, cfg: ddiscord.Config{GuildID: "g1", LiveChannelID: "now-live", TwitchLogin: "streamer"}}
	w := liveWorker(guild, kv, mods)
	payload := []byte(`{"subscription":{"type":"stream.offline"},"event":{"broadcaster_user_id":"42"}}`)
	if err := w.HandleStreamEvent(&bus.Message{Payload: payload}); err != nil {
		t.Fatalf("HandleStreamEvent: %v", err)
	}
	if len(guild.edits) != 1 {
		t.Fatal("offline must edit the go-live message")
	}
}

func TestAnnounceDiscordLiveSkipsWhenModuleOff(t *testing.T) {
	guild := &guildRecorder{}
	mods := memModules{found: true, enabled: false, cfg: ddiscord.Config{GuildID: "g1", LiveChannelID: "now-live", TwitchLogin: "streamer"}}
	w := liveWorker(guild, &memLiveStore{}, mods)
	w.announceDiscordLive(context.Background(), eventtwitch.StreamStatus{BroadcasterID: 42, Live: true})
	if len(guild.embeds) != 0 {
		t.Fatal("disabled module must not post")
	}
}

func TestAnnounceDiscordLiveIsIdempotentPerStream(t *testing.T) {
	guild := &guildRecorder{}
	kv := &memLiveStore{}
	mods := memModules{found: true, enabled: true, cfg: ddiscord.Config{GuildID: "g1", LiveChannelID: "now-live", TwitchLogin: "streamer"}}
	w := liveWorker(guild, kv, mods)
	for i := 0; i < 3; i++ {
		w.announceDiscordLive(context.Background(), eventtwitch.StreamStatus{BroadcasterID: 42, Live: true})
	}
	if len(guild.embeds) != 1 {
		t.Fatalf("embeds = %d, want 1: a replayed stream.online must not post twice", len(guild.embeds))
	}
}

func TestAnnounceDiscordOfflineForgetsTheMessage(t *testing.T) {
	guild := &guildRecorder{}
	kv := &memLiveStore{msg: discapi.Message{ChannelID: "now-live", ID: "msg-9"}}
	mods := memModules{found: true, enabled: true, cfg: ddiscord.Config{GuildID: "g1", LiveChannelID: "now-live", TwitchLogin: "streamer"}}
	w := liveWorker(guild, kv, mods)
	w.announceDiscordLive(context.Background(), eventtwitch.StreamStatus{BroadcasterID: 42, Live: false})
	w.announceDiscordLive(context.Background(), eventtwitch.StreamStatus{BroadcasterID: 42, Live: false})
	if len(guild.edits) != 1 {
		t.Fatalf("edits = %d, want 1: the key must be dropped after the offline edit", len(guild.edits))
	}
	if _, ok := kv.GetLiveMessage(context.Background(), liveMsgKey{BroadcasterID: "42"}); ok {
		t.Fatal("live message key survived the offline edit")
	}
}

func TestAnnounceDiscordLiveSkipsWithoutLogin(t *testing.T) {
	guild := &guildRecorder{}
	mods := memModules{found: true, enabled: true, cfg: ddiscord.Config{GuildID: "g1", LiveChannelID: "now-live"}}
	w := liveWorker(guild, &memLiveStore{}, mods)
	w.announceDiscordLive(context.Background(), eventtwitch.StreamStatus{BroadcasterID: 42, Live: true})
	if len(guild.embeds) != 0 {
		t.Fatal("no login means no watch link; the embed must be skipped")
	}
}

func TestAnnounceDiscordClipPostsEmbed(t *testing.T) {
	guild := &guildRecorder{}
	mods := memModules{found: true, enabled: true, cfg: ddiscord.Config{GuildID: "g1", ClipsChannelID: "clips"}}
	w := liveWorker(guild, nil, mods)
	w.announceDiscordClip(context.Background(), clipJob{
		BroadcasterID: "42",
		Embed:         ddiscord.ClipEmbed(ddiscord.ClipCard{URL: "https://clips.twitch.tv/x", Clipper: "viewer", Title: "huge play"}),
	})
	if len(guild.embeds) != 1 {
		t.Fatalf("embeds = %d", len(guild.embeds))
	}
	if guild.lastChan != "clips" {
		t.Fatalf("channel = %q", guild.lastChan)
	}
	if guild.embeds[0].Title != "huge play" {
		t.Fatalf("title = %q", guild.embeds[0].Title)
	}
}

func TestHandleClipCreatedPostsEmbed(t *testing.T) {
	guild := &guildRecorder{}
	mods := memModules{found: true, enabled: true, cfg: ddiscord.Config{GuildID: "g1", ClipsChannelID: "clips"}}
	w := liveWorker(guild, nil, mods)
	payload, err := codec.Marshal(map[string]any{
		"broadcaster_id": "42", "clip_id": "c1", "url": "https://clips.twitch.tv/x", "clipper": "viewer", "title": "huge play",
	})
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	if err := w.HandleClipCreated(&bus.Message{Payload: payload}); err != nil {
		t.Fatalf("HandleClipCreated: %v", err)
	}
	if len(guild.embeds) != 1 {
		t.Fatalf("embeds = %d, want 1", len(guild.embeds))
	}
	if guild.lastChan != "clips" {
		t.Fatalf("channel = %q", guild.lastChan)
	}
}

func TestHandleClipCreatedDropsMalformedPayload(t *testing.T) {
	guild := &guildRecorder{}
	w := liveWorker(guild, nil, memModules{found: true, enabled: true})
	if err := w.HandleClipCreated(&bus.Message{Payload: []byte("not json")}); err != nil {
		t.Fatalf("HandleClipCreated must ack malformed payloads, got err = %v", err)
	}
	if len(guild.embeds) != 0 {
		t.Fatal("malformed payload must not post")
	}
}

// fakeStreamInfo is a canned streamInfoReader, for liveInfo's Helix-fallback
// tests below where memModules alone (used by every other test in this
// file) cannot express "the projection has/has not caught up yet".
type fakeStreamInfo struct {
	info  projection.StreamInfo
	found bool
}

func (f fakeStreamInfo) GetStreamInfo(context.Context, string) (projection.StreamInfo, bool, error) {
	return f.info, f.found, nil
}

var _ streamInfoReader = fakeStreamInfo{}

// fakeFallback is a streamInfoFallback that counts calls and returns a
// canned result, standing in for the outgress RPC client (streaminfo_rpc.go)
// so these tests need no NATS connection.
type fakeFallback struct {
	calls  int
	result projection.StreamInfo
	ok     bool
}

func (f *fakeFallback) Lookup(context.Context, string) (projection.StreamInfo, bool) {
	f.calls++
	return f.result, f.ok
}

var _ streamInfoFallback = (*fakeFallback)(nil)

func liveWorkerWithFallback(guild *guildRecorder, mods discordModuleReader, info streamInfoReader, fb streamInfoFallback) *Worker {
	return New(Config{Discord: guild, DiscordKV: &memLiveStore{}, DiscordMods: mods, StreamInfo: info, HelixFallback: fb, Log: zap.NewNop()})
}

// TestAnnounceDiscordLiveSkipsFallbackWhenProjectionHasCategory is liveInfo's
// projection-hit path: the projected GameName is already non-empty, so it is
// used as-is and the outgress RPC is never dialed at all.
func TestAnnounceDiscordLiveSkipsFallbackWhenProjectionHasCategory(t *testing.T) {
	guild := &guildRecorder{}
	mods := memModules{found: true, enabled: true, cfg: ddiscord.Config{
		GuildID: "g1", LiveChannelID: "now-live", TwitchLogin: "streamer", CategoryAllow: "Minecraft",
	}}
	info := fakeStreamInfo{found: true, info: projection.StreamInfo{Title: "projected title", GameName: "Minecraft"}}
	fb := &fakeFallback{ok: true, result: projection.StreamInfo{Title: "should not be used", GameName: "Minecraft"}}
	w := liveWorkerWithFallback(guild, mods, info, fb)

	w.announceDiscordLive(context.Background(), eventtwitch.StreamStatus{BroadcasterID: 42, Live: true})

	if fb.calls != 0 {
		t.Fatalf("fallback calls = %d, want 0: a caught-up projection must not dial outgress", fb.calls)
	}
	if len(guild.embeds) != 1 || guild.embeds[0].Title != "projected title" {
		t.Fatalf("embeds = %v, want one embed titled from the projection", guild.embeds)
	}
}

// TestAnnounceDiscordLiveFallsBackToOutgressWhenAllowListed is liveInfo's
// allow-listed miss path: the projection has not caught up (empty
// GameName) and this broadcaster set a category allow-list, so liveInfo
// must ask outgress over RPC and use its answer instead of skipping the
// post the way TestAnnounceDiscordLiveRespectsCategoryAllow does without a
// fallback wired in.
func TestAnnounceDiscordLiveFallsBackToOutgressWhenAllowListed(t *testing.T) {
	guild := &guildRecorder{}
	mods := memModules{found: true, enabled: true, cfg: ddiscord.Config{
		GuildID: "g1", LiveChannelID: "now-live", TwitchLogin: "streamer", CategoryAllow: "Minecraft",
	}}
	info := fakeStreamInfo{found: false}
	fb := &fakeFallback{ok: true, result: projection.StreamInfo{Title: "fresh title", GameName: "Minecraft", ViewerCount: 12}}
	w := liveWorkerWithFallback(guild, mods, info, fb)

	w.announceDiscordLive(context.Background(), eventtwitch.StreamStatus{BroadcasterID: 42, Live: true})

	if fb.calls != 1 {
		t.Fatalf("fallback calls = %d, want 1: an allow-listed broadcaster with a missing category must hit outgress", fb.calls)
	}
	if len(guild.embeds) != 1 {
		t.Fatalf("embeds = %d, want 1", len(guild.embeds))
	}
	if guild.embeds[0].Title != "fresh title" {
		t.Fatalf("embed title = %q, want the outgress reply's title", guild.embeds[0].Title)
	}
	if len(guild.embeds[0].Fields) == 0 || guild.embeds[0].Fields[0].Value != "Minecraft" {
		t.Fatalf("embed fields = %v, want a Category field from the outgress reply", guild.embeds[0].Fields)
	}
}

// TestAnnounceDiscordLiveNonAllowListedNeverCallsOutgress is liveInfo's
// bound on Helix spend: a broadcaster with no category allow-list set gets
// the same empty-category-is-fine treatment it always had (CategoryAllowed
// with no allow list is unconditionally true), so the fallback must not be
// dialed for them even though their projection is just as behind as the
// allow-listed broadcaster above.
func TestAnnounceDiscordLiveNonAllowListedNeverCallsOutgress(t *testing.T) {
	guild := &guildRecorder{}
	mods := memModules{found: true, enabled: true, cfg: ddiscord.Config{
		GuildID: "g1", LiveChannelID: "now-live", TwitchLogin: "streamer",
	}}
	info := fakeStreamInfo{found: false}
	fb := &fakeFallback{ok: true, result: projection.StreamInfo{Title: "should never be fetched", GameName: "Minecraft"}}
	w := liveWorkerWithFallback(guild, mods, info, fb)

	w.announceDiscordLive(context.Background(), eventtwitch.StreamStatus{BroadcasterID: 42, Live: true})

	if fb.calls != 0 {
		t.Fatalf("fallback calls = %d, want 0: no allow-list means no bound Helix spend to protect and no reason to ask outgress", fb.calls)
	}
	if len(guild.embeds) != 1 {
		t.Fatalf("embeds = %d, want 1: a broadcaster with no allow-list still posts on an empty category", len(guild.embeds))
	}
}

// TestAnnounceDiscordLiveFallbackFailureStaysBestEffort covers the outgress
// RPC failing (timeout, no responder, an error reply) for an allow-listed
// broadcaster. The fallback is attempted (proving a slow/broken outgress
// does not get silently skipped), and the handler completes cleanly with
// no error and no panic -- the same graceful, conservative outcome the
// original outgress-resident code had on a Helix failure: with the category
// still unknown and an allow-list demanding one, the post is skipped rather
// than guessed at, exactly as TestAnnounceDiscordLiveRespectsCategoryAllow
// already covers for the "no fallback wired at all" case. What this test
// adds is proof that reaching for outgress and having it fail is handled
// through that same ordinary path -- not an error, not a block, not a
// second embed once retried.
func TestAnnounceDiscordLiveFallbackFailureStaysBestEffort(t *testing.T) {
	guild := &guildRecorder{}
	kv := &memLiveStore{}
	mods := memModules{found: true, enabled: true, cfg: ddiscord.Config{
		GuildID: "g1", LiveChannelID: "now-live", TwitchLogin: "streamer", CategoryAllow: "Minecraft",
	}}
	info := fakeStreamInfo{found: false}
	fb := &fakeFallback{ok: false}
	w := New(Config{Discord: guild, DiscordKV: kv, DiscordMods: mods, StreamInfo: info, HelixFallback: fb, Log: zap.NewNop()})

	payload := []byte(`{"subscription":{"type":"stream.online"},"event":{"broadcaster_user_id":"42"}}`)
	if err := w.HandleStreamEvent(&bus.Message{Payload: payload}); err != nil {
		t.Fatalf("HandleStreamEvent must always ack, got err = %v", err)
	}

	if fb.calls != 1 {
		t.Fatalf("fallback calls = %d, want 1: the allow-listed broadcaster must still try outgress", fb.calls)
	}
	if len(guild.embeds) != 0 {
		t.Fatalf("embeds = %d, want 0: an unresolved category on an allow-listed broadcaster must not post", len(guild.embeds))
	}
	if _, ok := kv.GetLiveMessage(context.Background(), liveMsgKey{BroadcasterID: "42"}); ok {
		t.Fatal("no message was posted, so none should be remembered")
	}
}
