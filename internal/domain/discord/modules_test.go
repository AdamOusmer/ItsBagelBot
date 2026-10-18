// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import (
	"reflect"
	"strings"
	"testing"

	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func populatedConfig(enabled string) Config {
	cfg := Config{}
	v := reflect.ValueOf(&cfg).Elem()
	for tag, i := range configFieldsByTag(v.Type()) {
		v.Field(i).SetString("v-" + tag)
	}
	for _, spec := range Modules {
		if spec.EnabledKey != "" {
			v.Field(configIndex[spec.EnabledKey]).SetString(enabled)
		}
	}
	return cfg
}

func TestSplitJoinRoundTripsEveryConfigField(t *testing.T) {
	for _, enabled := range []string{"on", "off"} {
		cfg := populatedConfig(enabled)
		rows := SplitConfig(cfg)
		assert.Len(t, rows, len(Modules)-1, "one row per module except identity")
		assert.Equal(t, cfg, JoinModules(cfg.GuildID, rows), "enabled=%s", enabled)
	}
}

func TestSplitRowsCarryOnlyTheirOwnFields(t *testing.T) {
	rows := SplitConfig(populatedConfig("on"))
	for _, row := range rows {
		spec, ok := Spec(row.Name)
		require.True(t, ok)
		var got map[string]string
		require.NoError(t, codec.Unmarshal(row.Settings, &got))
		keys := make([]string, 0, len(got))
		for k := range got {
			keys = append(keys, k)
		}
		assert.ElementsMatch(t, spec.Fields, keys, row.Name)
	}
}

func TestAbsentRowSemanticsFollowKind(t *testing.T) {
	var none GuildModules
	for _, spec := range Modules {
		want := spec.Kind != ModuleOptIn
		assert.Equal(t, want, none.On(spec.Name), spec.Name)
	}
	assert.False(t, none.On("nope"))
	off := GuildModules{Rows: map[string]ModuleRow{ModuleRoles: {Name: ModuleRoles}, ModuleLive: {Name: ModuleLive}}}
	assert.True(t, off.On(ModuleRoles), "core ignores the row flag")
	assert.False(t, off.On(ModuleLive))
}

func TestEnableSemanticsMatchTheLegacyFlags(t *testing.T) {
	legacy := map[string]func(Config) bool{
		ModuleLive: Config.LiveOn, ModuleClips: Config.ClipsOn, ModuleWelcome: Config.WelcomeOn,
		ModuleAutoRole: Config.AutoRoleOn, ModuleVoice: Config.VoiceOn, ModuleTickets: Config.TicketsOn,
		ModuleLevels: Config.LevelsOn, ModuleLinkGuard: Config.LinkGuardOn,
		ModuleLogs:        func(c Config) bool { return wantToggle(true, c.LogsEnabled) },
		ModuleSubscribers: func(c Config) bool { return wantToggle(false, c.SubscribersEnabled) },
	}
	for name, read := range legacy {
		spec, _ := Spec(name)
		for _, value := range []string{"", "on", "off", "yes"} {
			cfg := Config{}
			reflect.ValueOf(&cfg).Elem().Field(configIndex[spec.EnabledKey]).SetString(value)
			rows := map[string]ModuleRow{}
			for _, row := range SplitConfig(cfg) {
				rows[row.Name] = row
			}
			mods := GuildModules{Rows: rows}
			assert.Equal(t, read(cfg), mods.On(name), "%s=%q", name, value)
		}
	}
	for _, value := range []string{"", "on", "off"} {
		welcome := WelcomeSettings{GoodbyeEnabled: value}
		assert.Equal(t, Config{GoodbyeEnabled: value}.GoodbyeOn(), welcome.GoodbyeOn())
	}
}

func TestGuildModulesDecodeAndRoles(t *testing.T) {
	mods := GuildModules{Rows: map[string]ModuleRow{}}
	for _, row := range SplitConfig(Config{ModsRoleID: "m", LiveChannelID: "c"}) {
		mods.Rows[row.Name] = row
	}
	var live LiveSettings
	require.NoError(t, mods.Decode(ModuleLive, &live))
	assert.Equal(t, "c", live.LiveChannelID)
	assert.Equal(t, "m", mods.Roles().ModsRoleID)
	require.NoError(t, GuildModules{}.Decode(ModuleLive, &live), "absent row decodes to nothing")
}

func TestValidateModuleReportsOnlyItsOwnFields(t *testing.T) {
	raw := codec.RawMessage(`{"ticketPanelColor":"nope","logChannelId":"bad","ticketOpenLimit":"9"}`)
	got := ValidateModule(ModuleTickets, raw)
	fields := []string{}
	for _, fe := range got {
		fields = append(fields, fe.Field)
	}
	assert.ElementsMatch(t, []string{"ticketPanelColor", "ticketOpenLimit"}, fields)

	assert.Empty(t, ValidateModule(ModuleLive, codec.RawMessage(`{"liveChannelId":"123456789012345678"}`)))
	assert.NotEmpty(t, ValidateModule("nope", nil))
	assert.NotEmpty(t, ValidateModule(ModuleIdentity, nil))
	assert.NotEmpty(t, ValidateModule(ModuleLive, codec.RawMessage(`[1]`)))
}

func TestSanitizeModuleBlanksInvalidFields(t *testing.T) {
	raw := codec.RawMessage(`{"liveChannelId":"x","twitchLogin":"keep"}`)
	clean, bad := SanitizeModule(ModuleLive, raw)
	require.Len(t, bad, 1)
	assert.Equal(t, "liveChannelId", bad[0].Field)
	var live LiveSettings
	require.NoError(t, codec.Unmarshal(clean, &live))
	assert.Equal(t, LiveSettings{TwitchLogin: "keep"}, live)

	_, bad = SanitizeModule("nope", raw)
	assert.NotEmpty(t, bad)
}

func TestModuleTableCoversTheDesignNames(t *testing.T) {
	names := []string{}
	for _, spec := range Modules {
		names = append(names, spec.Name)
	}
	assert.Equal(t, "roles,live,clips,welcome,autorole,subscribers,voice,tickets,logs,levels,linkguard,moderation,identity", strings.Join(names, ","))
}
