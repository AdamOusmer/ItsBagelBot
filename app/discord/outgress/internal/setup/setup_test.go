// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package setup

import (
	"context"
	"strconv"
	"sync"
	"testing"

	discapi "ItsBagelBot/internal/discordapi"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"

	"go.uber.org/zap"
)

// guildRecorder is a map-backed discordGuildAPI, mirroring the pre-split
// egress test's fake of the same name.
type guildRecorder struct {
	mu        sync.Mutex
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

func (r *guildRecorder) SendPanel(_ context.Context, post discapi.EmbedPost, buttons []discapi.Button) (discapi.Message, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, btn := range buttons {
		r.panels = append(r.panels, btn.CustomID)
	}
	return discapi.Message{ChannelID: post.ChannelID, ID: r.nextSnowflake("panel-")}, nil
}

func (r *guildRecorder) CreateChannel(_ context.Context, ch discapi.GuildChannel) (discapi.Snowflake, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := discapi.Snowflake{ID: r.nextSnowflake("ch-"), Name: ch.Spec.Name, Type: ch.Spec.Type}
	r.channels = append(r.channels, out)
	r.createdCh = append(r.createdCh, ch.Spec.Name)
	return out, nil
}

func (r *guildRecorder) CreateRole(_ context.Context, role discapi.GuildRole) (discapi.Snowflake, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.createdRo = append(r.createdRo, role.Spec.Name)
	return discapi.Snowflake{ID: r.nextSnowflake("role-"), Name: role.Spec.Name}, nil
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

var _ discordGuildAPI = (*guildRecorder)(nil)

func setupWorker(guild *guildRecorder, store discordstore.Store) *Worker {
	return New(Config{Discord: guild, Store: store, Log: zap.NewNop()})
}

func setupGuild1(t *testing.T, w *Worker, broadcasterID string) GuildSetupResult {
	t.Helper()
	got, err := w.SetupGuild(context.Background(), GuildSetupRequest{GuildID: "guild-1", BroadcasterID: broadcasterID})
	if err != nil {
		t.Fatalf("SetupGuild: %v", err)
	}
	return got
}

// foreignChannels fakes a lived-in server: enough channels the template
// does not know about.
func foreignChannels() []discapi.Snowflake {
	out := make([]discapi.Snowflake, 0, ddiscord.LivingCommunityMinChannels)
	for i := 0; i < ddiscord.LivingCommunityMinChannels; i++ {
		out = append(out, discapi.Snowflake{ID: "ch-" + strconv.Itoa(i), Name: "existing-" + strconv.Itoa(i)})
	}
	return out
}

func assertBound(t *testing.T, store *discordstore.Mem, broadcasterID string) {
	t.Helper()
	b, ok := store.Broadcaster(context.Background(), discordstore.Guild{ID: "guild-1"})
	if !ok {
		t.Fatal("guild-1 must be bound")
	}
	if b.ID != broadcasterID {
		t.Fatalf("bound broadcaster = %s, want %s", b.ID, broadcasterID)
	}
}

func assertFilled(t *testing.T, got GuildSetupResult) {
	t.Helper()
	if got.Refused != "" {
		t.Fatalf("refused = %q", got.Refused)
	}
	if msg := filledGaps(got); msg != "" {
		t.Fatal(msg)
	}
}

func filledGaps(got GuildSetupResult) string {
	if got.GuildID != "guild-1" {
		return "guild = " + got.GuildID
	}
	for _, slot := range []struct{ id, name string }{
		{got.LiveChannelID, "live channel"},
		{got.ClipsChannelID, "clips channel"},
		{got.VoiceHubID, "voice hub"},
		{got.LogChannelID, "logs channel"},
		{got.TicketChannelID, "ticket channel"},
		{got.TicketCategoryID, "ticket category"},
	} {
		if slot.id == "" {
			return "missing " + slot.name
		}
	}
	return ""
}

func TestSetupGuildCreatesMissingRolesAndBindsChannels(t *testing.T) {
	guild := &guildRecorder{}
	store := discordstore.NewMem()

	got := setupGuild1(t, setupWorker(guild, store), "42")

	assertFilled(t, got)
	assertBound(t, store, "42")
	wantRoles := map[string]bool{"Owner": true, "Lead Mod": true, "Mods": true, "Regulars": true, "Member": true}
	for _, name := range guild.createdRo {
		delete(wantRoles, name)
	}
	if len(wantRoles) != 0 {
		t.Fatalf("missing roles %v", wantRoles)
	}
	if len(guild.panels) == 0 {
		t.Fatal("setup must post the ticket desk button")
	}
	if guild.panels[0] != discapi.CustomTicketOpen {
		t.Fatalf("desk button = %v", guild.panels)
	}
}

func TestSetupGuildRefusesALivedInServerButStillBinds(t *testing.T) {
	guild := &guildRecorder{channels: foreignChannels()}
	store := discordstore.NewMem()

	got := setupGuild1(t, setupWorker(guild, store), "42")

	if got.Refused == "" {
		t.Fatal("lived-in guild must refuse the fill")
	}
	if len(guild.createdCh) != 0 {
		t.Fatal("lived-in guild must not create channels")
	}
	if got.GuildID != "guild-1" {
		t.Fatalf("refused setup returns the guild id: %+v", got)
	}
	if got.ClipsChannelID != "" {
		t.Fatalf("refused setup adopts nothing here: %+v", got)
	}
	assertBound(t, store, "42")
}

func TestSetupGuildRefusesAGuildBoundToAnotherBroadcasterBeforeAnyWrite(t *testing.T) {
	guild := &guildRecorder{}
	store := discordstore.NewMem()
	store.PutGuild(discordstore.Guild{ID: "guild-1"}, discordstore.Broadcaster{ID: "7"})

	_, err := setupWorker(guild, store).SetupGuild(context.Background(), GuildSetupRequest{GuildID: "guild-1", BroadcasterID: "42"})

	if err != ErrGuildBoundElsewhere {
		t.Fatalf("err = %v, want ErrGuildBoundElsewhere", err)
	}
	if len(guild.createdCh)+len(guild.createdRo) != 0 {
		t.Fatal("a refused caller must not touch the server")
	}
	assertBound(t, store, "7")
}

func TestSetupGuildCompletesAPartialFill(t *testing.T) {
	guild := &guildRecorder{}
	// A fill cut short by a timeout left eight template channels behind.
	for _, name := range []string{"Welcome", "welcome", "rules", "Announcements", "now-live", "clips", "announcements", "Community"} {
		guild.channels = append(guild.channels, discapi.Snowflake{ID: "old-" + name, Name: name})
	}
	store := discordstore.NewMem()
	store.PutGuild(discordstore.Guild{ID: "guild-1"}, discordstore.Broadcaster{ID: "42"})

	got := setupGuild1(t, setupWorker(guild, store), "42")

	assertFilled(t, got)
	if got.LiveChannelID != "old-now-live" {
		t.Fatalf("existing live channel must be reused: %+v", got)
	}
	if got.ClipsChannelID != "old-clips" {
		t.Fatalf("existing clips channel must be reused: %+v", got)
	}
	for _, name := range guild.createdCh {
		if name == "now-live" || name == "clips" {
			t.Fatalf("%s was created twice", name)
		}
	}
}

func TestSetupGuildAdoptsMatchingChannelsOnALivedInServer(t *testing.T) {
	guild := &guildRecorder{channels: append(foreignChannels(), discapi.Snowflake{ID: "their-clips", Name: "Clips"})}

	got := setupGuild1(t, setupWorker(guild, discordstore.NewMem()), "42")

	if got.Refused == "" {
		t.Fatal("lived-in guild must refuse the fill")
	}
	if len(guild.createdCh) != 0 {
		t.Fatal("lived-in guild must not create channels")
	}
	if got.ClipsChannelID != "their-clips" {
		t.Fatalf("clips channel must be adopted by name, got %q", got.ClipsChannelID)
	}
}

func TestUnbindGuildOnlyForTheBoundBroadcaster(t *testing.T) {
	store := discordstore.NewMem()
	store.PutGuild(discordstore.Guild{ID: "guild-1"}, discordstore.Broadcaster{ID: "42"})
	w := setupWorker(&guildRecorder{}, store)

	if err := w.UnbindGuild(context.Background(), GuildSetupRequest{GuildID: "guild-1", BroadcasterID: "7"}); err != ErrGuildBoundElsewhere {
		t.Fatalf("err = %v, want ErrGuildBoundElsewhere", err)
	}
	if err := w.UnbindGuild(context.Background(), GuildSetupRequest{GuildID: "guild-1", BroadcasterID: "42"}); err != nil {
		t.Fatalf("UnbindGuild: %v", err)
	}
	if _, ok := store.Broadcaster(context.Background(), discordstore.Guild{ID: "guild-1"}); ok {
		t.Fatal("binding survived unbind")
	}
}

func TestGuildLayoutRequiresTheBinding(t *testing.T) {
	guild := &guildRecorder{channels: []discapi.Snowflake{{ID: "c1", Name: "general"}}}
	store := discordstore.NewMem()
	store.PutGuild(discordstore.Guild{ID: "guild-1"}, discordstore.Broadcaster{ID: "42"})
	w := setupWorker(guild, store)

	if _, err := w.GuildLayout(context.Background(), GuildSetupRequest{GuildID: "guild-1", BroadcasterID: "7"}); err != ErrGuildBoundElsewhere {
		t.Fatalf("err = %v, want ErrGuildBoundElsewhere", err)
	}
	layout, err := w.GuildLayout(context.Background(), GuildSetupRequest{GuildID: "guild-1", BroadcasterID: "42"})
	if err != nil {
		t.Fatalf("GuildLayout: %v", err)
	}
	if len(layout.Channels) != 1 {
		t.Fatalf("channels = %+v", layout.Channels)
	}
	if layout.Channels[0].ID != "c1" {
		t.Fatalf("channels = %+v", layout.Channels)
	}
}

func TestPostDiscordRequiresChannelAndContent(t *testing.T) {
	guild := &guildRecorder{}
	w := setupWorker(guild, discordstore.NewMem())

	if err := w.PostDiscord(context.Background(), "", "hi"); err != discapi.ErrBadRequest {
		t.Fatalf("err = %v, want ErrBadRequest", err)
	}
	if err := w.PostDiscord(context.Background(), "chan", ""); err != discapi.ErrBadRequest {
		t.Fatalf("err = %v, want ErrBadRequest", err)
	}
}

func TestPostDiscordRequiresAClient(t *testing.T) {
	w := New(Config{Log: zap.NewNop()})
	if err := w.PostDiscord(context.Background(), "chan", "hi"); err != discapi.ErrAuth {
		t.Fatalf("err = %v, want ErrAuth", err)
	}
}

// The permission bitfield must reach Discord as a decimal STRING, not a
// number: role permissions exceed 53 bits, so a JSON number would lose
// precision, and Discord rejects the field outright if it is not a string.
// An unprivileged role sends nothing rather than "0".
// roleSpecNamed returns the CommunityRoles() entry with the given name, or
// the zero value if none matches. Pulled out of
// TestRolePermissionsAreStringEncoded, which used to run this same
// loop-and-match three times inline (a "Bumpy Road" of repeated
// loop-containing-a-conditional blocks); now the test body is assertions.
func roleSpecNamed(name string) ddiscord.RoleSpec {
	for _, r := range ddiscord.CommunityRoles() {
		if r.Name == name {
			return r
		}
	}
	return ddiscord.RoleSpec{}
}

func TestRolePermissionsAreStringEncoded(t *testing.T) {
	leadMod := roleSpecNamed("Lead Mod")
	mods := roleSpecNamed("Mods")
	regulars := roleSpecNamed("Regulars")

	if got := rolePermissions(leadMod); got != "8" {
		t.Fatalf("Lead Mod permissions = %q, want \"8\" (Administrator)", got)
	}
	// Mods now holds a real moderation set, so it must serialize too. The
	// point of the test is the ENCODING, not the value: a number here would
	// lose precision above 53 bits, which PermModerator exceeds via the
	// timeout bit (1<<40).
	if got := rolePermissions(mods); got == "" || got == "0" {
		t.Fatalf("Mods permissions = %q, want the moderator set", got)
	}
	if got := rolePermissions(regulars); got != "" {
		t.Fatalf("Regulars permissions = %q, want empty (grants nothing)", got)
	}
}
