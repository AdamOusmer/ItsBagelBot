// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import (
	"reflect"
	"strings"
	"testing"
)

// configReaders names, for every Config field, the thing that READS it.
//
// This exists because four fields (the subs/VIP room ids) shipped with a
// setup fill that produced them, an RPC reply that carried them, and nothing
// anywhere that ever read them -- they were dead weight in the blob for
// months, and a dashboard control for them would have written into a void.
// A field with nowhere to be read is either a bug or a deletion, and both
// are cheaper to notice here than in a support thread.
//
// An entry of the exact form "Config.Method" (no spaces) names a method on
// Config and is checked to exist. Any other entry names the function or
// package that consumes the field; those are documentation, verified by a
// human at review time.
var configReaders = map[string]string{
	"GuildID":            "Config.Connected",
	"LiveChannelID":      "app/discord/engine/modules.Live",
	"ClipsChannelID":     "app/discord/engine/modules.Clip",
	"WelcomeChannelID":   "app/discord/engine/modules.Welcome",
	"VoiceHubID":         "app/discord/engine/modules.Voice",
	"LogChannelID":       "Config.TicketLogChannel",
	"TicketChannelID":    "app/discord/engine/modules.Ticket (EnsureDesk)",
	"TicketCategoryID":   "app/discord/engine/modules.Ticket (channel parent)",
	"OwnerRoleID":        "Config.StaffRoleIDs",
	"LeadModRoleID":      "Config.StaffRoleIDs",
	"ModsRoleID":         "Config.StaffRoleIDs",
	"VIPRoleID":          "setup roleSlot adoption + dashboard picker (no engine autorole by design)",
	"SubscriberRoleID":   "setup roleSlot adoption + dashboard picker (no engine autorole by design)",
	"RegularsRoleID":     "setup roleSlot adoption + dashboard picker (no engine autorole by design)",
	"MemberRoleID":       "modules.autorole join role (welcome.go)",
	"SubsChannelID":      "Config.TierRooms",
	"SubsCategoryID":     "Config.TierRooms",
	"VIPChannelID":       "Config.TierRooms",
	"VIPCategoryID":      "Config.TierRooms",
	"LiveEnabled":        "Config.LiveOn",
	"ClipsEnabled":       "Config.ClipsOn",
	"WelcomeEnabled":     "Config.WelcomeOn",
	"GoodbyeEnabled":     "Config.GoodbyeOn",
	"VoiceEnabled":       "Config.VoiceOn",
	"TicketsEnabled":     "Config.TicketsOn",
	"LogsEnabled":        "Config.LogsOn",
	"LevelsEnabled":      "Config.LevelsOn",
	"LinkGuardEnabled":   "Config.LinkGuardOn",
	"SubscribersEnabled": "Config.SubscribersOn",
	"CategoryAllow":      "Config.CategoryAllowed",
	"CategoryDeny":       "Config.CategoryAllowed",
	"LinkAllowList":      "Config.LinkAllowed",
	"TwitchLogin":        "app/discord/engine/modules.Live (watch link)",

	"TicketArchiveCategoryID": "Config.TicketArchiveCategory",
	"TicketStaffRoles":        "Config.TicketStaffRoleIDs",
	"TicketOpenLimit":         "Config.TicketOpenLimitN",
	"TicketTranscriptEnabled": "Config.TicketTranscriptOn",
	"TicketLogChannelID":      "Config.TicketLogChannel",
	"TicketPanelTitle":        "Config.TicketPanel",
	"TicketPanelBody":         "Config.TicketPanel",
	"TicketPanelColor":        "Config.TicketPanel",
	"TicketPanelButton":       "Config.TicketPanel",
	"PinnedRoles":             "Config.PinnedRole",
	"AutoRoleEnabled":         "Config.AutoRoleOn",
}

func TestEveryConfigFieldHasADocumentedReader(t *testing.T) {
	typ := reflect.TypeOf(Config{})
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i).Name
		reader, ok := configReaders[field]
		if !ok || strings.TrimSpace(reader) == "" {
			t.Fatalf("Config.%s has no reader listed in configReaders. "+
				"Add the accessor or consumer that reads it, or delete the field.", field)
		}
		method, isMethod := strings.CutPrefix(reader, "Config.")
		if !isMethod || strings.Contains(method, " ") {
			continue
		}
		if _, found := typ.MethodByName(method); !found {
			t.Fatalf("Config.%s names reader %q, but Config has no method %q", field, reader, method)
		}
	}
}

// The reverse direction: a field deleted from Config must lose its entry
// too, or the table rots into a list of things that used to exist.
func TestConfigReadersHasNoStaleEntries(t *testing.T) {
	typ := reflect.TypeOf(Config{})
	known := make(map[string]bool, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		known[typ.Field(i).Name] = true
	}
	for field := range configReaders {
		if !known[field] {
			t.Fatalf("configReaders lists %q, which is no longer a Config field", field)
		}
	}
}

// Every field must also carry a camelCase json tag: the console's
// DiscordConfig is keyed by these exact strings, and a missing or snake_case
// tag silently drops the setting on the way to the dashboard.
func TestEveryConfigFieldHasACamelCaseJSONTag(t *testing.T) {
	typ := reflect.TypeOf(Config{})
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		tag := f.Tag.Get("json")
		if tag == "" {
			t.Fatalf("Config.%s has no json tag", f.Name)
		}
		name, _, _ := strings.Cut(tag, ",")
		if name == "" || strings.ContainsAny(name, "_-") || name[0] < 'a' || name[0] > 'z' {
			t.Fatalf("Config.%s json tag %q is not camelCase", f.Name, name)
		}
	}
}
