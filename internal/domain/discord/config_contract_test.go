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
	"LogChannelID":       "Config.TicketLogChannel",
	"TicketChannelID":    "app/discord/engine/modules.Ticket (EnsureDesk)",
	"TicketCategoryID":   "app/discord/engine/modules.Ticket (channel parent)",
	"OwnerRoleID":        "Config.StaffRoleIDs (discord.IsModStaff)",
	"LeadModRoleID":      "Config.StaffRoleIDs (discord.IsModStaff)",
	"ModsRoleID":         "Config.StaffRoleIDs (discord.IsModStaff)",
	"VIPRoleID":          "setup roleSlot adoption + dashboard picker (no engine autorole by design)",
	"SubscriberRoleID":   "setup roleSlot adoption + dashboard picker (no engine autorole by design)",
	"RegularsRoleID":     "setup roleSlot adoption + dashboard picker (no engine autorole by design)",
	"MemberRoleID":       "modules.autorole join role (welcome.go)",
	"SubsChannelID":      "SubscribersSettings.TierRooms",
	"SubsCategoryID":     "SubscribersSettings.TierRooms",
	"VIPChannelID":       "SubscribersSettings.TierRooms",
	"VIPCategoryID":      "SubscribersSettings.TierRooms",
	"LiveEnabled":        "Config.LiveOn",
	"ClipsEnabled":       "Config.ClipsOn",
	"WelcomeEnabled":     "Config.WelcomeOn",
	"GoodbyeEnabled":     "app/discord/engine/modules.Goodbye",
	"VoiceEnabled":       "Config.VoiceOn",
	"TicketsEnabled":     "Config.TicketsOn",
	"LogsEnabled":        "Config.LogCategoryOn",
	"LevelsEnabled":      "Config.LevelsOn",
	"LinkGuardEnabled":   "Config.LinkGuardOn",
	"SubscribersEnabled": "GuildModules.On",
	"CategoryAllow":      "Config.CategoryAllowed",
	"CategoryDeny":       "Config.CategoryAllowed",
	"LinkAllowList":      "Config.LinkAllowed",
	"TwitchLogin":        "app/discord/engine/modules.Live (watch link)",

	"TicketArchiveCategoryID": "Config.TicketArchiveCategory",
	"TicketStaffRoles":        "Config.TicketStaffRoleIDs (discord.IsTicketStaff)",
	"TicketOpenLimit":         "Config.TicketOpenLimitN",
	"TicketTranscriptEnabled": "Config.TicketTranscriptOn",
	"TicketLogChannelID":      "Config.TicketLogChannel",
	"TicketPanelTitle":        "Config.TicketPanel",
	"TicketPanelBody":         "Config.TicketPanel",
	"TicketPanelColor":        "Config.TicketPanel",
	"TicketPanelButton":       "Config.TicketPanel",
	"PinnedRoles":             "RolesSettings.PinnedRole",
	"AutoRoleEnabled":         "Config.AutoRoleOn",
	"LogMessagesEnabled":      "Config.LogCategoryOn",
	"LogMembersEnabled":       "Config.LogCategoryOn",
	"LogVoiceEnabled":         "Config.LogCategoryOn",
	"LogModerationEnabled":    "Config.LogCategoryOn",
	"LogChannelsEnabled":      "Config.LogCategoryOn",
	"LogRolesEnabled":         "Config.LogCategoryOn",
	"LogServerEnabled":        "Config.LogCategoryOn",
	"LogIgnoreBots":           "Config.LogIgnoreBotsOn",
	"LogMessagesChannelID":    "Config.LogChannelFor",
	"LogMembersChannelID":     "Config.LogChannelFor",
	"LogVoiceChannelID":       "Config.LogChannelFor",
	"LogModerationChannelID":  "Config.LogChannelFor",
	"LogIgnoredChannels":      "Config.LogIgnores",
	"VoiceCategoryID":         "app/discord/engine/modules.VoiceState (clone parent)",
	"VoiceNameTemplate":       "Config.VoiceName",
	"VoiceUserLimit":          "Config.VoiceLimit",
	"VoicePrivacyMode":        "Config.VoicePrivacy",
}

func configFields() []reflect.StructField {
	typ := reflect.TypeOf(Config{})
	fields := make([]reflect.StructField, typ.NumField())
	for i := range fields {
		fields[i] = typ.Field(i)
	}
	return fields
}

func TestEveryConfigFieldHasADocumentedReader(t *testing.T) {
	for _, field := range configFields() {
		reader, ok := configReaders[field.Name]
		require.True(t, ok && strings.TrimSpace(reader) != "",
			"Config.%s has no reader listed in configReaders. Add the accessor or consumer that reads it, or delete the field.", field.Name)
		typeName, method, isMethod := strings.Cut(reader, ".")
		if !isMethod || strings.Contains(method, " ") || strings.Contains(typeName, "/") {
			continue
		}
		owner, known := readerTypes[typeName]
		require.True(t, known, "Config.%s names reader %q on unknown type %q", field.Name, reader, typeName)
		_, found := owner.MethodByName(method)
		assert.True(t, found, "Config.%s names reader %q, but %s has no method %q", field.Name, reader, typeName, method)
	}
}

var readerTypes = map[string]reflect.Type{
	"Config":              reflect.TypeFor[Config](),
	"RolesSettings":       reflect.TypeFor[RolesSettings](),
	"SubscribersSettings": reflect.TypeFor[SubscribersSettings](),
	"GuildModules":        reflect.TypeFor[GuildModules](),
}

func TestEveryConfigFieldIsOwnedByExactlyOneModule(t *testing.T) {
	owners := map[string][]string{}
	for _, spec := range Modules {
		for _, tag := range spec.Fields {
			owners[tag] = append(owners[tag], spec.Name)
		}
		if spec.EnabledKey != "" {
			owners[spec.EnabledKey] = append(owners[spec.EnabledKey], spec.Name)
		}
	}
	for _, tag := range configJSONTags(t) {
		if tag == "guildId" {
			assert.Empty(t, owners[tag], "guildId is stamped from the binding, never stored in a module")
			continue
		}
		assert.Len(t, owners[tag], 1, "Config json tag %q must be owned by exactly one module, got %v", tag, owners[tag])
	}
	for tag := range owners {
		assert.Contains(t, configJSONTags(t), tag, "module table names %q, which is not a Config json tag", tag)
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
