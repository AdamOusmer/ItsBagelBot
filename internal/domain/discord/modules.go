// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import (
	"reflect"
	"strings"

	"ItsBagelBot/pkg/codec"
)

type ModuleKind int

const (
	ModuleCore ModuleKind = iota
	ModuleDefault
	ModuleOptIn
)

const (
	ModuleRoles       = "roles"
	ModuleLive        = "live"
	ModuleClips       = "clips"
	ModuleWelcome     = "welcome"
	ModuleGoodbye     = "goodbye"
	ModuleAutoRole    = "autorole"
	ModuleSubscribers = "subscribers"
	ModuleVoice       = "voice"
	ModuleTickets     = "tickets"
	ModuleLogs        = "logs"
	ModuleLevels      = "levels"
	ModuleLinkGuard   = "linkguard"
	ModuleModeration  = "moderation"
	ModuleIdentity    = "identity"
	ModuleVoiceState  = "voicestate"
)

type ModuleSpec struct {
	Name       string
	Kind       ModuleKind
	EnabledKey string
	Fields     []string
}

var moduleSettings = map[string]reflect.Type{
	ModuleRoles:       reflect.TypeFor[RolesSettings](),
	ModuleLive:        reflect.TypeFor[LiveSettings](),
	ModuleClips:       reflect.TypeFor[ClipsSettings](),
	ModuleWelcome:     reflect.TypeFor[WelcomeSettings](),
	ModuleGoodbye:     reflect.TypeFor[GoodbyeSettings](),
	ModuleAutoRole:    reflect.TypeFor[AutoRoleSettings](),
	ModuleSubscribers: reflect.TypeFor[SubscribersSettings](),
	ModuleVoice:       reflect.TypeFor[VoiceSettings](),
	ModuleTickets:     reflect.TypeFor[TicketSettings](),
	ModuleLogs:        reflect.TypeFor[LogsSettings](),
	ModuleLevels:      reflect.TypeFor[LevelsSettings](),
	ModuleLinkGuard:   reflect.TypeFor[LinkGuardSettings](),
	ModuleModeration:  reflect.TypeFor[ModerationSettings](),
}

var Modules = []ModuleSpec{
	moduleSpec(ModuleRoles, ModuleCore, ""),
	moduleSpec(ModuleLive, ModuleDefault, "liveEnabled"),
	moduleSpec(ModuleClips, ModuleDefault, "clipsEnabled"),
	moduleSpec(ModuleWelcome, ModuleDefault, "welcomeEnabled"),
	moduleSpec(ModuleGoodbye, ModuleOptIn, "goodbyeEnabled"),
	moduleSpec(ModuleAutoRole, ModuleDefault, "autoRoleEnabled"),
	moduleSpec(ModuleSubscribers, ModuleOptIn, "subscribersEnabled"),
	moduleSpec(ModuleVoice, ModuleDefault, "voiceEnabled"),
	moduleSpec(ModuleTickets, ModuleDefault, "ticketsEnabled"),
	moduleSpec(ModuleLogs, ModuleDefault, "logsEnabled"),
	moduleSpec(ModuleLevels, ModuleDefault, "levelsEnabled"),
	moduleSpec(ModuleLinkGuard, ModuleOptIn, "linkGuardEnabled"),
	moduleSpec(ModuleModeration, ModuleDefault, ""),
	{Name: ModuleIdentity, Kind: ModuleCore},
	{Name: ModuleVoiceState, Kind: ModuleCore},
}

var configIndex = configFieldsByTag(reflect.TypeFor[Config]())

type moduleKey string

type fieldKey string

func moduleSpec(name moduleKey, kind ModuleKind, enabledKey fieldKey) ModuleSpec {
	return ModuleSpec{Name: string(name), Kind: kind, EnabledKey: string(enabledKey), Fields: settingsTags(moduleSettings[string(name)])}
}

func jsonTag(field reflect.StructField) string {
	tag, _, _ := strings.Cut(field.Tag.Get("json"), ",")
	return tag
}

func settingsTags(t reflect.Type) []string {
	tags := make([]string, 0, t.NumField())
	for i := range t.NumField() {
		tags = append(tags, jsonTag(t.Field(i)))
	}
	return tags
}

func Spec(name string) (ModuleSpec, bool) {
	for _, spec := range Modules {
		if spec.Name == name {
			return spec, true
		}
	}
	return ModuleSpec{}, false
}

type ModuleRow struct {
	Name     string           `json:"name"`
	Enabled  bool             `json:"enabled"`
	Settings codec.RawMessage `json:"settings,omitempty"`
	Version  int              `json:"version"`
}

type GuildModules struct {
	GuildID string
	Rows    map[string]ModuleRow
}

func (g GuildModules) On(name string) bool {
	spec, ok := Spec(name)
	if !ok {
		return false
	}
	if spec.Kind == ModuleCore {
		return true
	}
	row, ok := g.Rows[name]
	if !ok {
		return spec.Kind == ModuleDefault
	}
	return row.Enabled
}

func (g GuildModules) Decode(name string, out any) error {
	row, ok := g.Rows[name]
	if !ok || len(row.Settings) == 0 {
		return nil
	}
	return codec.Unmarshal(row.Settings, out)
}

func (g GuildModules) Roles() RolesSettings {
	var roles RolesSettings
	_ = g.Decode(ModuleRoles, &roles)
	return roles
}

func (g GuildModules) Ordered() []ModuleRow {
	out := make([]ModuleRow, 0, len(g.Rows))
	for _, spec := range Modules {
		if row, ok := g.Rows[spec.Name]; ok {
			out = append(out, row)
		}
	}
	return out
}

func SplitConfig(cfg Config) []ModuleRow {
	src := reflect.ValueOf(cfg)
	rows := make([]ModuleRow, 0, len(Modules))
	for _, spec := range Modules {
		if !Stored(spec.Name) {
			continue
		}
		raw, _ := codec.Marshal(settingsOf(spec.Name, src).Interface())
		rows = append(rows, ModuleRow{Name: spec.Name, Enabled: legacyEnabled(spec, src), Settings: raw})
	}
	return rows
}

func Stored(name string) bool {
	_, ok := moduleSettings[name]
	return ok
}

func settingsOf(name string, cfg reflect.Value) reflect.Value {
	out := reflect.New(moduleSettings[name]).Elem()
	for i := range out.NumField() {
		out.Field(i).SetString(cfg.Field(configIndex[jsonTag(out.Type().Field(i))]).String())
	}
	return out
}

func legacyEnabled(spec ModuleSpec, cfg reflect.Value) bool {
	if spec.EnabledKey == "" {
		return true
	}
	flag := cfg.Field(configIndex[spec.EnabledKey]).String()
	if spec.Kind == ModuleOptIn {
		return flag == "on"
	}
	return flag != "off"
}

func JoinModules(guildID string, rows []ModuleRow) Config {
	cfg := Config{GuildID: guildID}
	dst := reflect.ValueOf(&cfg).Elem()
	for _, row := range rows {
		applyRow(dst, row)
	}
	return cfg
}

func applyRow(cfg reflect.Value, row ModuleRow) {
	spec, ok := Spec(row.Name)
	if !ok || !Stored(spec.Name) {
		return
	}
	if set, err := decodeSettings(spec.Name, row.Settings); err == nil {
		writeSettings(cfg, set)
	}
	if spec.EnabledKey != "" {
		cfg.Field(configIndex[spec.EnabledKey]).SetString(onOff(row.Enabled))
	}
}

func decodeSettings(name string, raw codec.RawMessage) (reflect.Value, error) {
	set := reflect.New(moduleSettings[name])
	if len(raw) == 0 {
		return set.Elem(), nil
	}
	err := codec.Unmarshal(raw, set.Interface())
	return set.Elem(), err
}

func writeSettings(cfg, set reflect.Value) {
	for i := range set.NumField() {
		cfg.Field(configIndex[jsonTag(set.Type().Field(i))]).SetString(set.Field(i).String())
	}
}

func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

func moduleConfig(name string, settings codec.RawMessage) (Config, []FieldError) {
	if _, ok := Spec(name); !ok || !Stored(name) {
		return Config{}, []FieldError{{Field: "name", Code: CodeInvalidChoice}}
	}
	if _, err := decodeSettings(name, settings); err != nil {
		return Config{}, []FieldError{{Field: "settings", Code: CodeInvalidChoice}}
	}
	return JoinModules("", []ModuleRow{{Name: name, Enabled: true, Settings: settings}}), nil
}

func ValidateModule(name string, settings codec.RawMessage) []FieldError {
	cfg, bad := moduleConfig(name, settings)
	if bad != nil {
		return bad
	}
	return ValidateConfig(cfg)
}

func ValidateModuleFields(name string, settings codec.RawMessage) []string {
	var out []string
	seen := map[string]bool{}
	for _, fe := range ValidateModule(name, settings) {
		if !seen[fe.Field] {
			seen[fe.Field] = true
			out = append(out, fe.Field)
		}
	}
	return out
}

func SanitizeModule(name string, settings codec.RawMessage) (codec.RawMessage, []FieldError) {
	cfg, bad := moduleConfig(name, settings)
	if bad != nil {
		return nil, bad
	}
	clean, bad := SanitizeConfig(cfg)
	raw, _ := codec.Marshal(settingsOf(name, reflect.ValueOf(clean)).Interface())
	return raw, bad
}
