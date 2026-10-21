// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import (
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var configReaders = map[string]string{
	"GuildID":            "Config.Connected",
	"LiveChannelID":      "app/discord/engine/modules.Live",
	"ClipsChannelID":     "app/discord/engine/modules.Clip",
	"WelcomeChannelID":   "app/discord/engine/modules.Welcome",
	"VoiceHubID":         "app/discord/engine/modules.VoiceState",
	"LogChannelID":       "LogsSettings.ChannelFor",
	"TicketChannelID":    "app/discord/engine/modules.Ticket (EnsureDesk)",
	"TicketCategoryID":   "app/discord/engine/modules.Ticket (channel parent)",
	"OwnerRoleID":        "RolesSettings.StaffRoleIDs (RolesSettings.IsModStaff)",
	"LeadModRoleID":      "RolesSettings.StaffRoleIDs (RolesSettings.IsModStaff)",
	"ModsRoleID":         "RolesSettings.StaffRoleIDs (RolesSettings.IsModStaff)",
	"VIPRoleID":          "setup roleSlot adoption + dashboard picker (no engine autorole by design)",
	"SubscriberRoleID":   "setup roleSlot adoption + dashboard picker (no engine autorole by design)",
	"RegularsRoleID":     "setup roleSlot adoption + dashboard picker (no engine autorole by design)",
	"MemberRoleID":       "modules.autorole join role (welcome.go)",
	"SubsChannelID":      "app/discord/outgress/internal/setup (template tier rooms)",
	"SubsCategoryID":     "app/discord/outgress/internal/setup (template tier rooms)",
	"VIPChannelID":       "app/discord/outgress/internal/setup (template tier rooms)",
	"VIPCategoryID":      "app/discord/outgress/internal/setup (template tier rooms)",
	"LiveEnabled":        "GuildModules.On",
	"ClipsEnabled":       "GuildModules.On",
	"WelcomeEnabled":     "GuildModules.On",
	"GoodbyeEnabled":     "app/discord/engine/modules.Goodbye",
	"VoiceEnabled":       "GuildModules.On",
	"TicketsEnabled":     "GuildModules.On",
	"LogsEnabled":        "GuildModules.On",
	"LevelsEnabled":      "GuildModules.On",
	"LinkGuardEnabled":   "GuildModules.On",
	"SubscribersEnabled": "GuildModules.On",
	"CategoryAllow":      "LiveSettings.CategoryAllowed",
	"CategoryDeny":       "LiveSettings.CategoryAllowed",
	"LinkAllowList":      "LinkGuardSettings.Allowed",
	"TwitchLogin":        "app/discord/engine/modules.Live (watch link)",

	"TicketArchiveCategoryID": "TicketSettings.ArchiveCategory",
	"TicketStaffRoles":        "TicketSettings.StaffRoleIDs (TicketSettings.IsStaff)",
	"TicketOpenLimit":         "TicketSettings.OpenLimitN",
	"TicketTranscriptEnabled": "TicketSettings.TranscriptOn",
	"TicketLogChannelID":      "TicketSettings.LogChannel",
	"TicketPanelTitle":        "Config.TicketPanel",
	"TicketPanelBody":         "Config.TicketPanel",
	"TicketPanelColor":        "Config.TicketPanel",
	"TicketPanelButton":       "Config.TicketPanel",
	"PinnedRoles":             "RolesSettings.PinnedRole",
	"AutoRoleEnabled":         "GuildModules.On",
	"LogMessagesEnabled":      "LogsSettings.CategoryOn",
	"LogMembersEnabled":       "LogsSettings.CategoryOn",
	"LogVoiceEnabled":         "LogsSettings.CategoryOn",
	"LogModerationEnabled":    "LogsSettings.CategoryOn",
	"LogChannelsEnabled":      "LogsSettings.CategoryOn",
	"LogRolesEnabled":         "LogsSettings.CategoryOn",
	"LogServerEnabled":        "LogsSettings.CategoryOn",
	"LogIgnoreBots":           "LogsSettings.IgnoreBotsOn",
	"LogMessagesChannelID":    "LogsSettings.ChannelFor",
	"LogMembersChannelID":     "LogsSettings.ChannelFor",
	"LogVoiceChannelID":       "LogsSettings.ChannelFor",
	"LogModerationChannelID":  "LogsSettings.ChannelFor",
	"LogIgnoredChannels":      "LogsSettings.Ignores",
	"VoiceCategoryID":         "app/discord/engine/modules.VoiceState (clone parent)",
	"VoiceNameTemplate":       "VoiceSettings.Name",
	"VoiceUserLimit":          "VoiceSettings.Limit",
	"VoicePrivacyMode":        "VoiceSettings.Privacy",
}

func configFields() []reflect.StructField {
	typ := reflect.TypeOf(Config{})
	fields := make([]reflect.StructField, typ.NumField())
	for i := range fields {
		fields[i] = typ.Field(i)
	}
	return fields
}

func namesTypeMethod(typeName, method string, isMethod bool) bool {
	return isMethod && !strings.ContainsAny(method, " ") && !strings.ContainsAny(typeName, "/")
}

func TestEveryConfigFieldHasADocumentedReader(t *testing.T) {
	for _, field := range configFields() {
		reader, ok := configReaders[field.Name]
		require.True(t, ok && strings.TrimSpace(reader) != "",
			"Config.%s has no reader listed in configReaders. Add the accessor or consumer that reads it, or delete the field.", field.Name)
		typeName, method, isMethod := strings.Cut(reader, ".")
		if !namesTypeMethod(typeName, method, isMethod) {
			continue
		}
		owner, known := readerTypes[typeName]
		require.True(t, known, "Config.%s names reader %q on unknown type %q", field.Name, reader, typeName)
		_, found := owner.MethodByName(method)
		assert.True(t, found, "Config.%s names reader %q, but %s has no method %q", field.Name, reader, typeName, method)
	}
}

var readerTypes = map[string]reflect.Type{
	"Config":            reflect.TypeFor[Config](),
	"RolesSettings":     reflect.TypeFor[RolesSettings](),
	"LiveSettings":      reflect.TypeFor[LiveSettings](),
	"LinkGuardSettings": reflect.TypeFor[LinkGuardSettings](),
	"LogsSettings":      reflect.TypeFor[LogsSettings](),
	"TicketSettings":    reflect.TypeFor[TicketSettings](),
	"VoiceSettings":     reflect.TypeFor[VoiceSettings](),
	"GuildModules":      reflect.TypeFor[GuildModules](),
}

func moduleOwners() map[string][]string {
	owners := map[string][]string{}
	for _, spec := range Modules {
		keys := spec.Fields
		if spec.EnabledKey != "" {
			keys = append(slices.Clone(keys), spec.EnabledKey)
		}
		for _, tag := range keys {
			owners[tag] = append(owners[tag], spec.Name)
		}
	}
	return owners
}

func assertOwnedOnce(t *testing.T, tag string, owned []string) {
	if tag == "guildId" {
		assert.Empty(t, owned, "guildId is stamped from the binding, never stored in a module")
		return
	}
	assert.Len(t, owned, 1, "Config json tag %q must be owned by exactly one module, got %v", tag, owned)
}

func TestEveryConfigFieldIsOwnedByExactlyOneModule(t *testing.T) {
	owners := moduleOwners()
	tags := configJSONTags(t)
	for _, tag := range tags {
		assertOwnedOnce(t, tag, owners[tag])
	}
	for tag := range owners {
		assert.Contains(t, tags, tag, "module table names %q, which is not a Config json tag", tag)
	}
}

func TestConfigReadersHasNoStaleEntries(t *testing.T) {
	known := map[string]bool{}
	for _, field := range configFields() {
		known[field.Name] = true
	}
	for field := range configReaders {
		assert.True(t, known[field], "configReaders lists %q, which is no longer a Config field", field)
	}
}

func jsonName(field reflect.StructField) string {
	name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
	return name
}

func TestEveryConfigFieldHasACamelCaseJSONTag(t *testing.T) {
	for _, field := range configFields() {
		name := jsonName(field)
		require.NotEmpty(t, name, "Config.%s has no json tag", field.Name)
		assert.Regexp(t, `^[a-z][A-Za-z0-9]*$`, name, "Config.%s json tag is not camelCase", field.Name)
	}
}

func configJSONTags(t *testing.T) []string {
	t.Helper()
	var tags []string
	for _, field := range configFields() {
		if !field.IsExported() {
			continue
		}
		name := jsonName(field)
		require.NotEmpty(t, name, "Config.%s is exported with no json tag; it would ship as %q and the console would never see it", field.Name, field.Name)
		if name != "-" {
			tags = append(tags, name)
		}
	}
	slices.Sort(tags)
	return tags
}

func TestConfigFieldsMatchTheSharedFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/config_fields.json")
	require.NoError(t, err)
	var doc struct {
		Fields []string `json:"fields"`
	}
	require.NoError(t, codec.Unmarshal(raw, &doc))

	assert.Equal(t, doc.Fields, configJSONTags(t), "Config json tags drifted from testdata/config_fields.json")
}
