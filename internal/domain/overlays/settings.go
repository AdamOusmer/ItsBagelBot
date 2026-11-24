// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package overlays

import (
	_ "embed"
	"maps"
	"regexp"
	"slices"
	"time"
	"unicode/utf8"

	"ItsBagelBot/pkg/codec"
)

type SettingType string

const (
	SettingText      SettingType = "text"
	SettingFamily    SettingType = "family"
	SettingColor     SettingType = "color"
	SettingMedia     SettingType = "media"
	SettingMediaList SettingType = "media-list"
	SettingList      SettingType = "list"
	SettingDatetime  SettingType = "datetime"
	SettingBoolean   SettingType = "boolean"
	SettingNumber    SettingType = "number"
	SettingSelect    SettingType = "select"
	SettingObject    SettingType = "object"
	SettingMap       SettingType = "map"
	SettingArray     SettingType = "array"
	SettingScalars   SettingType = "scalars"
)

// Setting is one entry of the pinned widget settings table shared with the TypeScript runtime.
// Keys the table does not name are ignored, as the runtime ignores them. Object holds Fields, map holds Fields under each of Keys, array holds Fields per item up to Limit,
// list and scalars hold up to Limit entries when Limit is set.
type Setting struct {
	Key     string      `json:"key"`
	Type    SettingType `json:"type"`
	Min     float64     `json:"min,omitempty"`
	Max     float64     `json:"max,omitempty"`
	Options []string    `json:"options,omitempty"`
	Keys    []string    `json:"keys,omitempty"`
	Limit   int         `json:"limit,omitempty"`
	Fields  []Setting   `json:"fields,omitempty"`
}

type settingsTable struct {
	Kinds map[WidgetKind]struct {
		Fields []Setting `json:"fields"`
	} `json:"kinds"`
}

//go:embed testdata/widget-settings.json
var widgetSettingsJSON []byte

var widgetSettings = loadWidgetSettings()

func loadWidgetSettings() map[WidgetKind][]Setting {
	var table settingsTable
	if err := codec.Unmarshal(widgetSettingsJSON, &table); err != nil {
		panic("overlays: widget settings table: " + err.Error())
	}
	out := make(map[WidgetKind][]Setting, len(table.Kinds))
	for kind, entry := range table.Kinds {
		out[kind] = entry.Fields
	}
	return out
}

const (
	maxSettingText   = 4000
	maxSettingFamily = 120
	maxSettingMedia  = 110
	maxSettingItem   = 100
	maxMediaList     = 100
	maxPlainList     = 500
)

var (
	settingColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	settingMedia = regexp.MustCompile(`^asset:[a-zA-Z0-9_-]{1,96}$`)
	dateLayouts  = []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02"}
)

var settingChecks map[SettingType]func(Setting, any, Path) error

func init() {
	settingChecks = map[SettingType]func(Setting, any, Path) error{
		SettingText:      textSetting(maxSettingText),
		SettingFamily:    textSetting(maxSettingFamily),
		SettingColor:     patternSetting(settingColor, 7),
		SettingMedia:     mediaSetting,
		SettingMediaList: mediaListSetting,
		SettingList:      listSetting,
		SettingDatetime:  datetimeSetting,
		SettingBoolean:   booleanSetting,
		SettingNumber:    numberSetting,
		SettingSelect:    selectSetting,
		SettingObject:    objectSetting,
		SettingMap:       mapSetting,
		SettingArray:     arraySetting,
		SettingScalars:   scalarsSetting,
	}
}

func validateSettings(kind WidgetKind, settings map[string]any, path Path) error {
	return checkFields(widgetSettings[kind], settings, path)
}

func checkFields(fields []Setting, value map[string]any, path Path) error {
	known := make(map[string]Setting, len(fields))
	for _, field := range fields {
		known[field.Key] = field
	}
	for _, key := range slices.Sorted(maps.Keys(value)) {
		field, found := known[key]
		if !found {
			continue
		}
		if err := field.check(value[key], path.Field(key)); err != nil {
			return err
		}
	}
	return nil
}

func (s Setting) check(value any, path Path) error {
	return settingChecks[s.Type](s, value, path)
}

func invalidSetting(path Path) error { return fail(CodeInvalidSetting, path) }

func textSetting(limit int) func(Setting, any, Path) error {
	return func(_ Setting, value any, path Path) error {
		text, ok := value.(string)
		if !ok || utf8.RuneCountInString(text) > limit {
			return invalidSetting(path)
		}
		return nil
	}
}

func boundedText(value any, limit int, accepts func(string) bool) bool {
	text, ok := value.(string)
	return ok && len(text) <= limit && accepts(text)
}

func orEmpty(accepts func(string) bool) func(string) bool {
	return func(text string) bool { return text == "" || accepts(text) }
}

func patternSetting(pattern *regexp.Regexp, limit int) func(Setting, any, Path) error {
	return func(_ Setting, value any, path Path) error {
		if !boundedText(value, limit, pattern.MatchString) {
			return invalidSetting(path)
		}
		return nil
	}
}

func mediaSetting(_ Setting, value any, path Path) error {
	if !boundedText(value, maxSettingMedia, orEmpty(settingMedia.MatchString)) {
		return invalidSetting(path)
	}
	return nil
}

func itemsOf(value any, limit int) ([]any, bool) {
	items, ok := value.([]any)
	return items, ok && len(items) <= limit
}

func eachItem(value any, limit int, check func(any, Path) error, path Path) error {
	items, ok := itemsOf(value, limit)
	if !ok {
		return invalidSetting(path)
	}
	for i, item := range items {
		if err := check(item, path.Index(i)); err != nil {
			return err
		}
	}
	return nil
}

func mediaListSetting(s Setting, value any, path Path) error {
	return eachItem(value, maxMediaList, func(item any, at Path) error { return mediaSetting(s, item, at) }, path)
}

func listSetting(s Setting, value any, path Path) error {
	return eachItem(value, maxPlainList, func(item any, at Path) error { return textSetting(maxSettingItem)(s, item, at) }, path)
}

func datetimeSetting(_ Setting, value any, path Path) error {
	if !boundedText(value, 64, orEmpty(parsesAsDate)) {
		return invalidSetting(path)
	}
	return nil
}

func parsesAsDate(text string) bool {
	return slices.ContainsFunc(dateLayouts, func(layout string) bool {
		_, err := time.Parse(layout, text)
		return err == nil
	})
}

func booleanSetting(_ Setting, value any, path Path) error {
	if _, ok := value.(bool); !ok {
		return invalidSetting(path)
	}
	return nil
}

func numberOf(value any) (float64, bool) {
	switch number := value.(type) {
	case float64:
		return number, true
	case int:
		return float64(number), true
	case int64:
		return float64(number), true
	case codec.Number:
		parsed, err := number.Float64()
		return parsed, err == nil
	}
	return 0, false
}

func numberSetting(s Setting, value any, path Path) error {
	number, ok := numberOf(value)
	if !ok || !within(number, s.Min, s.Max) {
		return invalidSetting(path)
	}
	return nil
}

func selectSetting(s Setting, value any, path Path) error {
	text, ok := value.(string)
	if !ok || !slices.Contains(s.Options, text) {
		return invalidSetting(path)
	}
	return nil
}

func objectOf(value any) (map[string]any, bool) {
	object, ok := value.(map[string]any)
	return object, ok
}

func objectSetting(s Setting, value any, path Path) error {
	object, ok := objectOf(value)
	if !ok {
		return invalidSetting(path)
	}
	return checkFields(s.Fields, object, path)
}

func mapSetting(s Setting, value any, path Path) error {
	object, ok := objectOf(value)
	if !ok {
		return invalidSetting(path)
	}
	for _, key := range slices.Sorted(maps.Keys(object)) {
		if !slices.Contains(s.Keys, key) {
			continue
		}
		if err := objectSetting(s, object[key], path.Field(key)); err != nil {
			return err
		}
	}
	return nil
}

func arraySetting(s Setting, value any, path Path) error {
	return eachItem(value, s.Limit, func(item any, at Path) error { return objectSetting(s, item, at) }, path)
}

func scalarsSetting(s Setting, value any, path Path) error {
	object, ok := objectOf(value)
	if !ok || len(object) > s.Limit {
		return invalidSetting(path)
	}
	return nil
}
