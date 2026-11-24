// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package overlays_test

import (
	"testing"

	"ItsBagelBot/internal/domain/overlays"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWidgetSettingsValidation(t *testing.T) {
	cases := []struct {
		name     string
		kind     overlays.WidgetKind
		settings map[string]any
		want     overlays.Code
	}{
		{"nothing set", overlays.WidgetText, map[string]any{}, ""},
		{"known text settings", overlays.WidgetText, map[string]any{"text": "hi", "align": "left", "style": map[string]any{"size": 40, "color": "#fff000"}}, ""},
		{"keys outside the table are ignored", overlays.WidgetAlertbox, map[string]any{"delay": 3}, ""},
		{"font size below range", overlays.WidgetText, map[string]any{"style": map[string]any{"size": 1}}, overlays.CodeInvalidSetting},
		{"color that is not hex", overlays.WidgetText, map[string]any{"style": map[string]any{"color": "red"}}, overlays.CodeInvalidSetting},
		{"option outside the list", overlays.WidgetGoal, map[string]any{"metric": "yearly_gifts"}, overlays.CodeInvalidSetting},
		{"alert duration below range", overlays.WidgetAlertbox, map[string]any{"events": map[string]any{"follow": map[string]any{"duration": 0}}}, overlays.CodeInvalidSetting},
		{"alert variation condition", overlays.WidgetAlertbox, map[string]any{"events": map[string]any{"follow": map[string]any{"variations": []any{map[string]any{"condition": "nope"}}}}}, overlays.CodeInvalidSetting},
		{"valid alert variation", overlays.WidgetAlertbox, map[string]any{"events": map[string]any{"sub": map[string]any{"variations": []any{map[string]any{"condition": "tier", "value": "2000", "overrides": map[string]any{"title": "x"}}}}}}, ""},
		{"negative subathon rule", overlays.WidgetTimer, map[string]any{"rules": map[string]any{"base_seconds": -1}}, overlays.CodeInvalidSetting},
		{"media must be an asset reference", overlays.WidgetImage, map[string]any{"media": "https://example.com/a.png"}, overlays.CodeInvalidSetting},
		{"flag that is not boolean", overlays.WidgetVideo, map[string]any{"loop": "yes"}, overlays.CodeInvalidSetting},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := customDocument()
			doc.Widgets = []overlays.Widget{widgetWith("w1", tc.kind, tc.settings)}

			assert.Equal(t, tc.want, codeOf(overlays.Validate(doc)))
		})
	}
}

func TestWidgetSettingsTableCoversEveryKind(t *testing.T) {
	var pinned struct {
		Kinds map[string]any `json:"kinds"`
	}
	require.NoError(t, codec.Unmarshal(fixture(t, "widget-settings.json"), &pinned))

	var kinds []string
	for _, kind := range overlays.WidgetKinds {
		kinds = append(kinds, string(kind))
	}

	require.ElementsMatch(t, kinds, keysOf(pinned.Kinds))
}

func keysOf(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}
