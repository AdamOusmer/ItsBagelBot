// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"strconv"
	"sync"
	"testing"

	discapi "ItsBagelBot/app/outgress/internal/discord"
	ddiscord "ItsBagelBot/internal/domain/discord"
	eventtwitch "ItsBagelBot/internal/domain/event/twitch"
	"ItsBagelBot/internal/domain/rpc/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

// guildRecorder is a discordGuildAPI that captures live/clip/setup calls.
type guildRecorder struct {
	discordRecordingClient
	mu        sync.Mutex
	embeds    []ddiscord.Embed
	edits     []string
	roles     []string
	channels  []discapi.Snowflake
	createdCh []string
	createdRo []string
	nextID    int
}

func (r *guildRecorder) nextSnowflake(prefix string) string {
	r.nextID++
	return prefix + strconv.Itoa(r.nextID)
}

func (r *guildRecorder) SendEmbed(_ context.Context, post discapi.EmbedPost) (discapi.Message, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastChan = post.ChannelID
	r.embeds = append(r.embeds, post.Embed)
	r.calls++
	return discapi.Message{ChannelID: post.ChannelID, ID: r.nextSnowflake("msg-")}, nil
}

func (r *guildRecorder) EditMessage(_ context.Context, _ discapi.Message, content string, _ []ddiscord.Embed) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.edits = append(r.edits, content)
	return nil
}

func (r *guildRecorder) CreateChannel(_ context.Context, _ string, ch discapi.ChannelCreate) (discapi.Snowflake, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := discapi.Snowflake{ID: r.nextSnowflake("ch-"), Name: ch.Name, Type: ch.Type}
	r.channels = append(r.channels, out)
	r.createdCh = append(r.createdCh, ch.Name)
	return out, nil
}

func (r *guildRecorder) DeleteChannel(context.Context, string) error { return nil }

func (r *guildRecorder) CreateRole(_ context.Context, _ string, role discapi.RoleCreate) (discapi.Snowflake, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.createdRo = append(r.createdRo, role.Name)
	return discapi.Snowflake{ID: r.nextSnowflake("role-"), Name: role.Name}, nil
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

func (r *guildRecorder) ListGuildChannels(context.Context, string) ([]discapi.Snowflake, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]discapi.Snowflake, len(r.channels))
	copy(out, r.channels)
	return out, nil
}

func (r *guildRecorder) ListGuildRoles(context.Context, string) ([]discapi.Snowflake, error) {
	return []discapi.Snowflake{{ID: "guild-1", Name: "@everyone"}}, nil
}

func (r *guildRecorder) GetGuild(_ context.Context, guildID string) (discapi.Snowflake, error) {
	return discapi.Snowflake{ID: guildID, Name: "test"}, nil
}

var _ discordGuildAPI = (*guildRecorder)(nil)

type memLiveStore struct {
	mu            sync.Mutex
	msg           discapi.Message
	guild, twitch string
}

func (s *memLiveStore) PutLiveMessage(_ context.Context, _ string, m discapi.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msg = m
	return nil
}

func (s *memLiveStore) GetLiveMessage(_ context.Context, _ string) (discapi.Message, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.msg.ChannelID == "" {
		return s.msg, false
	}
	if s.msg.ID == "" {
		return s.msg, false
	}
	return s.msg, true
}

func (s *memLiveStore) DeleteLiveMessage(context.Context, string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msg = discapi.Message{}
	return nil
}

func (s *memLiveStore) PutGuild(_ context.Context, guildID, broadcasterID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.guild, s.twitch = guildID, broadcasterID
	return nil
}

func (s *memLiveStore) GetGuild(_ context.Context, guildID string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.guild != guildID {
		return "", false
	}
	if s.twitch == "" {
		return "", false
	}
	return s.twitch, true
}

func (s *memLiveStore) DeleteGuild(_ context.Context, guildID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.guild == guildID {
		s.guild, s.twitch = "", ""
	}
	return nil
}

type memModules struct {
	cfg     ddiscord.Config
	enabled bool
	found   bool
}

func (m memModules) GetModule(_ context.Context, _ uint64, name string) (projection.ModuleView, bool, error) {
	if name != ddiscord.ModuleName {
		return projection.ModuleView{}, false, nil
	}
	if !m.found {
		return projection.ModuleView{}, false, nil
	}
	raw, err := codec.Marshal(m.cfg)
	if err != nil {
		return projection.ModuleView{}, false, err
	}
	return projection.ModuleView{Name: name, IsEnabled: m.enabled, Configs: raw}, true, nil
}

func liveWorker(t *testing.T, guild *guildRecorder, kv discordLiveStore, mods discordModuleReader) *Worker {
	t.Helper()
	w := New(Config{Log: zap.NewNop(), Limiter: &scriptedLimiter{}})
	w.SetDiscord(guild, kv)
	w.discordMods = mods
	return w
}

func TestAnnounceDiscordLivePostsEmbed(t *testing.T) {
	guild := &guildRecorder{}
	kv := &memLiveStore{}
	mods := memModules{
		found:   true,
		enabled: true,
		cfg:     ddiscord.Config{GuildID: "g1", LiveChannelID: "now-live", TwitchLogin: "streamer"},
	}
	w := liveWorker(t, guild, kv, mods)

	w.announceDiscordLive(context.Background(), eventtwitch.StreamStatus{BroadcasterID: 42, Live: true})
	if len(guild.embeds) != 1 {
		t.Fatalf("embeds = %d, want 1", len(guild.embeds))
	}
	if guild.lastChan != "now-live" {
		t.Fatalf("channel = %q", guild.lastChan)
	}
	if kv.msg.ChannelID != "now-live" {
		t.Fatal("live message was not stored")
	}
	if kv.msg.ID == "" {
		t.Fatal("live message was not stored")
	}
}

func TestAnnounceDiscordLiveRespectsCategoryAllow(t *testing.T) {
	guild := &guildRecorder{}
	mods := memModules{
		found:   true,
		enabled: true,
		cfg:     ddiscord.Config{GuildID: "g1", LiveChannelID: "now-live", TwitchLogin: "streamer", CategoryAllow: "Minecraft"},
	}
	w := liveWorker(t, guild, &memLiveStore{}, mods)
	// No stream-info store means GameName is empty, which an allow-list rejects.
	w.announceDiscordLive(context.Background(), eventtwitch.StreamStatus{BroadcasterID: 42, Live: true})
	if len(guild.embeds) != 0 {
		t.Fatal("empty category must not post when an allow-list is set")
	}
}

func TestAnnounceDiscordOfflineEditsLiveMessage(t *testing.T) {
	guild := &guildRecorder{}
	kv := &memLiveStore{msg: discapi.Message{ChannelID: "now-live", ID: "msg-9"}}
	mods := memModules{
		found:   true,
		enabled: true,
		cfg:     ddiscord.Config{GuildID: "g1", LiveChannelID: "now-live", TwitchLogin: "streamer"},
	}
	w := liveWorker(t, guild, kv, mods)
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
	mods := memModules{
		found:   true,
		enabled: true,
		cfg:     ddiscord.Config{GuildID: "g1", LiveChannelID: "now-live", TwitchLogin: "streamer"},
	}
	w := liveWorker(t, guild, kv, mods)
	payload := []byte(`{"subscription":{"type":"stream.online"},"event":{"broadcaster_user_id":"42"}}`)
	if err := w.HandleStreamEvent(&bus.Message{Payload: payload}); err != nil {
		t.Fatalf("HandleStreamEvent: %v", err)
	}
	if len(guild.embeds) != 1 {
		t.Fatal("stream.online must post the go-live embed on this lane, not via sesame")
	}
}

func TestHandleStreamEventOfflineStillAnnouncesDiscord(t *testing.T) {
	guild := &guildRecorder{}
	kv := &memLiveStore{msg: discapi.Message{ChannelID: "now-live", ID: "msg-1"}}
	mods := memModules{
		found:   true,
		enabled: true,
		cfg:     ddiscord.Config{GuildID: "g1", LiveChannelID: "now-live", TwitchLogin: "streamer"},
	}
	w := liveWorker(t, guild, kv, mods)
	payload := []byte(`{"subscription":{"type":"stream.offline"},"event":{"broadcaster_user_id":"42"}}`)
	if err := w.HandleStreamEvent(&bus.Message{Payload: payload}); err != nil {
		t.Fatalf("HandleStreamEvent: %v", err)
	}
	if len(guild.edits) != 1 {
		t.Fatal("offline must edit the go-live message even though mod re-verify is skipped")
	}
}

func TestAnnounceDiscordClipPostsEmbed(t *testing.T) {
	guild := &guildRecorder{}
	mods := memModules{
		found:   true,
		enabled: true,
		cfg:     ddiscord.Config{GuildID: "g1", ClipsChannelID: "clips"},
	}
	w := liveWorker(t, guild, nil, mods)
	w.announceDiscordClip(context.Background(), "42", ddiscord.ClipEmbed("https://clips.twitch.tv/x", "viewer", "huge play"))
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

func TestAnnounceDiscordLiveSkipsWhenModuleOff(t *testing.T) {
	guild := &guildRecorder{}
	mods := memModules{found: true, enabled: false, cfg: ddiscord.Config{GuildID: "g1", LiveChannelID: "now-live", TwitchLogin: "streamer"}}
	w := liveWorker(t, guild, &memLiveStore{}, mods)
	w.announceDiscordLive(context.Background(), eventtwitch.StreamStatus{BroadcasterID: 42, Live: true})
	if len(guild.embeds) != 0 {
		t.Fatal("disabled module must not post")
	}
}

func TestAnnounceDiscordLiveIsIdempotentPerStream(t *testing.T) {
	guild := &guildRecorder{}
	kv := &memLiveStore{}
	mods := memModules{
		found:   true,
		enabled: true,
		cfg:     ddiscord.Config{GuildID: "g1", LiveChannelID: "now-live", TwitchLogin: "streamer"},
	}
	w := liveWorker(t, guild, kv, mods)
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
	mods := memModules{
		found:   true,
		enabled: true,
		cfg:     ddiscord.Config{GuildID: "g1", LiveChannelID: "now-live", TwitchLogin: "streamer"},
	}
	w := liveWorker(t, guild, kv, mods)
	w.announceDiscordLive(context.Background(), eventtwitch.StreamStatus{BroadcasterID: 42, Live: false})
	w.announceDiscordLive(context.Background(), eventtwitch.StreamStatus{BroadcasterID: 42, Live: false})
	if len(guild.edits) != 1 {
		t.Fatalf("edits = %d, want 1: the key must be dropped after the offline edit", len(guild.edits))
	}
	if _, ok := kv.GetLiveMessage(context.Background(), "42"); ok {
		t.Fatal("live message key survived the offline edit")
	}
}

func TestAnnounceDiscordLiveSkipsWithoutLogin(t *testing.T) {
	guild := &guildRecorder{}
	mods := memModules{found: true, enabled: true, cfg: ddiscord.Config{GuildID: "g1", LiveChannelID: "now-live"}}
	w := liveWorker(t, guild, &memLiveStore{}, mods)
	w.announceDiscordLive(context.Background(), eventtwitch.StreamStatus{BroadcasterID: 42, Live: true})
	if len(guild.embeds) != 0 {
		t.Fatal("no login means no watch link; the embed must be skipped")
	}
}
