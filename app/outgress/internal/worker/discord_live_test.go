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

func (r *guildRecorder) SendEmbed(_ context.Context, channelID, _ string, embed ddiscord.Embed) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastChan = channelID
	r.embeds = append(r.embeds, embed)
	r.calls++
	return r.nextSnowflake("msg-"), nil
}

func (r *guildRecorder) EditMessage(_ context.Context, _, _, content string, _ []ddiscord.Embed) error {
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

func (r *guildRecorder) AddMemberRole(_ context.Context, _, _, roleID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.roles = append(r.roles, "add:"+roleID)
	return nil
}

func (r *guildRecorder) RemoveMemberRole(_ context.Context, _, _, roleID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.roles = append(r.roles, "remove:"+roleID)
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
	ch, msg       string
	guild, twitch string
}

func (s *memLiveStore) PutLiveMessage(_ context.Context, _, channelID, messageID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ch, s.msg = channelID, messageID
	return nil
}

func (s *memLiveStore) GetLiveMessage(_ context.Context, _ string) (string, string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ch, s.msg, s.ch != "" && s.msg != ""
}

func (s *memLiveStore) DeleteLiveMessage(context.Context, string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ch, s.msg = "", ""
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
	if s.guild != guildID || s.twitch == "" {
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
	if name != ddiscord.ModuleName || !m.found {
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
	if kv.ch != "now-live" || kv.msg == "" {
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
	kv := &memLiveStore{ch: "now-live", msg: "msg-9"}
	mods := memModules{
		found:   true,
		enabled: true,
		cfg:     ddiscord.Config{GuildID: "g1", LiveChannelID: "now-live", TwitchLogin: "streamer"},
	}
	w := liveWorker(t, guild, kv, mods)
	w.announceDiscordLive(context.Background(), eventtwitch.StreamStatus{BroadcasterID: 42, Live: false})
	if len(guild.edits) != 1 || guild.edits[0] != ddiscord.OfflineContent {
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
	kv := &memLiveStore{ch: "now-live", msg: "msg-1"}
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
	w.announceDiscordClip(context.Background(), "42", "https://clips.twitch.tv/x", "viewer", "huge play")
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
	kv := &memLiveStore{ch: "now-live", msg: "msg-9"}
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
	if _, _, ok := kv.GetLiveMessage(context.Background(), "42"); ok {
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
