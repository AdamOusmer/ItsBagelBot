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
	w := setupWorker(guild, discordstore.NewMem())
	got, err := w.SetupGuild(context.Background(), GuildSetupRequest{
		GuildID: "guild-1", BroadcasterID: "42", PinnedRoles: pins,
	})
	if err != nil {
		t.Fatalf("SetupGuild: %v", err)
	}
	return guild, got
}

// A pinned slot must be ADOPTED, not created: the whole point is attaching
// to the role a server's members already hold. Creating a second role beside
// it is the failure this prevents.
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
	// Unpinned slots still get created, so a partial pin set is usable.
	if !slices.Contains(guild.createdRo, ddiscord.RoleOwner) {
		t.Fatalf("unpinned Owner was not created: %v", guild.createdRo)
	}
}

// The pinned id has to reach the channel gates too, or the Staff category
// allows a role nobody holds while the real staff role is locked out.
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

// A pin for a slot that is not part of the template must be ignored rather
// than crashing or attaching itself to some other role.
func TestUnknownPinnedSlotIsIgnored(t *testing.T) {
	guild, got := fillWithPins(t, map[string]string{"janitor": "nope"})

	if got.ModsRoleID == "nope" || got.OwnerRoleID == "nope" {
		t.Fatalf("an unknown slot leaked into a real one: %+v", got)
	}
	if !slices.Contains(guild.createdRo, ddiscord.RoleMods) {
		t.Fatalf("a bogus pin suppressed a real create: %v", guild.createdRo)
	}
}

// The Archive category holds closed tickets, so it must be created, bound
// to its result slot, staff-gated, and read-only: a record anyone can post
// into is not a record.
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
		// 1024 = VIEW only, 2048 = SEND denied: staff read the archive, they
		// do not write in it.
		if o.Allow != "1024" || o.Deny != "2048" {
			t.Fatalf("staff overwrite = %+v, want allow VIEW / deny SEND", o)
		}
	}
}

// The Archive must not make an otherwise-empty server look lived-in, or a
// second fill attempt would refuse to finish the first one.
func TestArchiveIsPartOfTheTemplateNameSet(t *testing.T) {
	var names []string
	for _, spec := range ddiscord.CommunityChannels() {
		names = append(names, strings.ToLower(spec.Name))
	}
	if !slices.Contains(names, "archive") {
		t.Fatal("Archive is missing from CommunityChannels")
	}
}
