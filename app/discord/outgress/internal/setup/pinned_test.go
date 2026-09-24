// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package setup

import (
	"context"
	"slices"
	"strings"
	"testing"

	discapi "ItsBagelBot/internal/discordapi"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"
)

func fillWithPins(t *testing.T, pins map[string]string) (*guildRecorder, GuildSetupResult) {
	t.Helper()
	guild := &guildRecorder{}
	for _, id := range pins {
		guild.roles = append(guild.roles, discapi.Snowflake{ID: id, Name: id})
	}
	w := setupWorker(guild, discordstore.NewMem())
	got, err := w.SetupGuild(context.Background(), GuildSetupRequest{
		GuildID: "guild-1", BroadcasterID: "42", PinnedRoles: pins,
	})
	if err != nil {
		t.Fatalf("SetupGuild: %v", err)
	}
	return guild, got
}

func TestSetupAdoptsPinnedRolesInsteadOfCreatingThem(t *testing.T) {
	guild, got := fillWithPins(t, map[string]string{
		ddiscord.SlotMods:   "existing-mods",
		ddiscord.SlotMember: "existing-member",
	})

	if got.ModsRoleID != "existing-mods" {
		t.Fatalf("mods = %q, want the pinned id", got.ModsRoleID)
	}
	if got.MemberRoleID != "existing-member" {
		t.Fatalf("member = %q, want the pinned id", got.MemberRoleID)
	}
	if slices.Contains(guild.createdRo, ddiscord.RoleMods) {
		t.Fatalf("a pinned slot created a role anyway: %v", guild.createdRo)
	}
	if !slices.Contains(guild.createdRo, ddiscord.RoleOwner) {
		t.Fatalf("unpinned Owner was not created: %v", guild.createdRo)
	}
}

func TestPinnedRoleIsUsedInChannelOverwrites(t *testing.T) {
	guild, _ := fillWithPins(t, map[string]string{ddiscord.SlotMods: "existing-mods"})

	spec, ok := guild.specs["staff"]
	if !ok {
		t.Fatal("the Staff category was never created")
	}
	var found bool
	for _, o := range spec.PermissionOverwrites {
		if o.ID == "existing-mods" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Staff overwrites = %+v, want the pinned mods id", spec.PermissionOverwrites)
	}
}

func TestUnknownPinnedSlotIsIgnored(t *testing.T) {
	guild, got := fillWithPins(t, map[string]string{"janitor": "nope"})

	if got.ModsRoleID == "nope" || got.OwnerRoleID == "nope" {
		t.Fatalf("an unknown slot leaked into a real one: %+v", got)
	}
	if !slices.Contains(guild.createdRo, ddiscord.RoleMods) {
		t.Fatalf("a bogus pin suppressed a real create: %v", guild.createdRo)
	}
}

func TestSetupCreatesStaffOnlyReadOnlyArchiveCategory(t *testing.T) {
	guild, got := fillWithPins(t, nil)

	if got.TicketArchiveCategoryID == "" {
		t.Fatal("the fill produced no archive category id")
	}
	spec, ok := guild.specs["archive"]
	if !ok {
		t.Fatalf("Archive was never created: %v", guild.createdCh)
	}
	if spec.Type != ddiscord.ChannelCategory {
		t.Fatalf("Archive type = %d, want a category", spec.Type)
	}
	assertArchiveOverwrites(t, spec.PermissionOverwrites)
}

func assertArchiveOverwrites(t *testing.T, overwrites []discapi.PermissionOverwrite) {
	t.Helper()
	if len(overwrites) < 2 {
		t.Fatalf("overwrites = %+v, want an @everyone deny plus staff allows", overwrites)
	}
	if overwrites[0].ID != "guild-1" || overwrites[0].Deny != "1024" {
		t.Fatalf("first overwrite = %+v, want @everyone denied VIEW", overwrites[0])
	}
	for _, o := range overwrites[1:] {
		if o.Allow != "1024" || o.Deny != "2048" {
			t.Fatalf("staff overwrite = %+v, want allow VIEW / deny SEND", o)
		}
	}
}

func TestArchiveIsPartOfTheTemplateNameSet(t *testing.T) {
	var names []string
	for _, spec := range ddiscord.CommunityChannels() {
		names = append(names, strings.ToLower(spec.Name))
	}
	if !slices.Contains(names, "archive") {
		t.Fatal("Archive is missing from CommunityChannels")
	}
}

func TestSetupDropsAPinWhoseRoleIsGone(t *testing.T) {
	guild := &guildRecorder{}
	got, err := setupWorker(guild, discordstore.NewMem()).SetupGuild(context.Background(), GuildSetupRequest{
		GuildID: "guild-1", BroadcasterID: "42",
		PinnedRoles: map[string]string{ddiscord.SlotMods: "deleted-role"},
	})
	if err != nil {
		t.Fatalf("SetupGuild: %v", err)
	}
	if got.ModsRoleID == "deleted-role" {
		t.Fatal("a role the guild does not have was adopted anyway")
	}
	if !slices.Contains(guild.createdRo, ddiscord.RoleMods) {
		t.Fatalf("the dropped pin did not fall back to creating Mods: %v", guild.createdRo)
	}
	if !slices.Equal(got.DroppedPins, []string{ddiscord.SlotMods}) {
		t.Fatalf("dropped = %v, want [%s]", got.DroppedPins, ddiscord.SlotMods)
	}
}

func TestDroppedPinsAreSorted(t *testing.T) {
	guild := &guildRecorder{roles: []discapi.Snowflake{{ID: "live-vip", Name: "VIP"}}}
	got, err := setupWorker(guild, discordstore.NewMem()).SetupGuild(context.Background(), GuildSetupRequest{
		GuildID: "guild-1", BroadcasterID: "42",
		PinnedRoles: map[string]string{
			ddiscord.SlotMods: "gone-1", ddiscord.SlotOwner: "gone-2",
			ddiscord.SlotMember: "gone-3", ddiscord.SlotVIP: "live-vip",
		},
	})
	if err != nil {
		t.Fatalf("SetupGuild: %v", err)
	}
	want := []string{ddiscord.SlotMember, ddiscord.SlotMods, ddiscord.SlotOwner}
	if !slices.Equal(got.DroppedPins, want) {
		t.Fatalf("dropped = %v, want %v", got.DroppedPins, want)
	}
	if got.VIPRoleID != "live-vip" {
		t.Fatalf("vip = %q, want the pin that IS live to survive", got.VIPRoleID)
	}
}
