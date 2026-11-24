// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package importer

import (
	"cmp"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"ItsBagelBot/internal/domain/overlays"
	"ItsBagelBot/pkg/codec"
)

const (
	maxName     = 120
	maxCoord    = 7680
	maxRotation = 360
	minCanvas   = 320
	boxWidth    = 400
	boxHeight   = 200
)

type widgetRule struct {
	kind   overlays.WidgetKind
	reason Reason
}

var widgetRules = map[string]widgetRule{
	"alertbox":      {kind: overlays.WidgetAlertbox},
	"eventlist":     {kind: overlays.WidgetEventlist},
	"goal":          {kind: overlays.WidgetGoal},
	"label":         {kind: overlays.WidgetLabel},
	"chatbox":       {kind: overlays.WidgetChatbox},
	"emotewall":     {kind: overlays.WidgetEmotewall},
	"countdown":     {kind: overlays.WidgetTimer},
	"credits":       {kind: overlays.WidgetCredits},
	"image":         {kind: overlays.WidgetImage},
	"video":         {kind: overlays.WidgetVideo},
	"text":          {kind: overlays.WidgetText},
	"slideshow":     {kind: overlays.WidgetSlideshow},
	"customwidget":  {kind: overlays.WidgetCustom},
	"custom":        {kind: overlays.WidgetCustom},
	"tips":          {reason: ReasonTips},
	"tip":           {reason: ReasonTips},
	"merch":         {reason: ReasonTips},
	"points":        {reason: ReasonStore},
	"store":         {reason: ReasonStore},
	"loyaltystore":  {reason: ReasonStore},
	"contests":      {reason: ReasonContests},
	"contest":       {reason: ReasonContests},
	"bets":          {reason: ReasonContests},
	"bet":           {reason: ReasonContests},
	"mediashare":    {reason: ReasonMediaShare},
	"keyvalue":      {reason: ReasonKeyValue},
	"keyvaluestore": {reason: ReasonKeyValue},
}

var (
	remoteMedia = regexp.MustCompile(`(?i)^(https?:)?//\S+\.(png|jpe?g|gif|webp|webm|mp4|mp3|ogg|wav|woff2?|ttf|otf|svg)(\?\S*)?$`)
	remoteFont  = regexp.MustCompile(`(?i)fonts\.(googleapis|gstatic)\.com|\.(woff2?|ttf|otf)(\?|$)`)
)

type exportWidget struct {
	Type     string         `json:"type"`
	Name     string         `json:"name"`
	X        float64        `json:"x"`
	Y        float64        `json:"y"`
	Width    float64        `json:"width"`
	Height   float64        `json:"height"`
	Rotation float64        `json:"rotation"`
	Opacity  *float64       `json:"opacity"`
	Visible  *bool          `json:"visible"`
	Settings map[string]any `json:"settings"`
}

type exportDoc struct {
	Name    string         `json:"name"`
	Width   int            `json:"width"`
	Height  int            `json:"height"`
	Widgets []exportWidget `json:"widgets"`
}

func within[T cmp.Ordered](value, low, high T) bool {
	return value >= low && value <= high
}

func clamp(value, low, high float64) float64 {
	return min(max(value, low), high)
}

func truncate(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}

func normalizeType(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, text)
}

func mapStrings(value any, fn func(string) string) any {
	switch node := value.(type) {
	case string:
		return fn(node)
	case map[string]any:
		out := make(map[string]any, len(node))
		for key, item := range node {
			out[key] = mapStrings(item, fn)
		}
		return out
	case []any:
		out := make([]any, len(node))
		for i, item := range node {
			out[i] = mapStrings(item, fn)
		}
		return out
	}
	return value
}

func (b *builder) bind(subject Subject, value any) any {
	return mapStrings(value, func(text string) string { return b.bindString(subject, text) })
}

func (b *builder) bindString(subject Subject, text string) string {
	if remoteMedia.MatchString(text) {
		b.report.unsupported(subject, remoteReason(text))
		return ""
	}
	if id, ok := b.lookup(text); ok {
		return assetPrefix + id
	}
	return text
}

func remoteReason(text string) Reason {
	if remoteFont.MatchString(text) {
		return ReasonRemoteFont
	}
	return ReasonNetwork
}

func (r widgetRule) refusal(known bool) Reason {
	if !known {
		return ReasonUnknownWidget
	}
	return r.reason
}

func (w exportWidget) subject(index int) Subject {
	name := strings.TrimSpace(w.Name)
	if name == "" {
		name = strings.TrimSpace(w.Type)
	}
	if name == "" {
		name = "Widget " + strconv.Itoa(index+1)
	}
	return Subject(truncate(name, maxName))
}

func (w exportWidget) opacity() float64 {
	if w.Opacity == nil {
		return 1
	}
	return clamp(*w.Opacity, 0, 1)
}

func sizeOr(value, fallback float64) float64 {
	if value < 1 {
		return fallback
	}
	return clamp(value, 1, maxCoord)
}

func (w exportWidget) place(kind overlays.WidgetKind, name Subject, settings map[string]any) overlays.Widget {
	return overlays.Widget{
		Name: string(name), Kind: kind,
		X: clamp(w.X, 0, maxCoord), Y: clamp(w.Y, 0, maxCoord),
		W: sizeOr(w.Width, boxWidth), H: sizeOr(w.Height, boxHeight),
		Rotation: clamp(w.Rotation, -maxRotation, maxRotation), Opacity: w.opacity(),
		Hidden: w.Visible != nil && !*w.Visible, Settings: settings,
	}
}

func (b *builder) adoptCanvas(doc exportDoc) {
	if strings.TrimSpace(doc.Name) != "" {
		b.name = truncate(strings.TrimSpace(doc.Name), maxName)
	}
	if within(doc.Width, minCanvas, maxCoord) && within(doc.Height, minCanvas, maxCoord) {
		b.width, b.height = doc.Width, doc.Height
	}
}

func (b *builder) addExport(doc exportDoc) error {
	b.adoptCanvas(doc)
	for i, w := range doc.Widgets {
		if err := b.exportWidget(i, w); err != nil {
			return err
		}
	}
	return nil
}

func (b *builder) exportWidget(index int, w exportWidget) error {
	name := w.subject(index)
	rule, known := widgetRules[normalizeType(w.Type)]
	if reason := rule.refusal(known); reason != "" {
		b.report.unsupported(name, reason)
		return nil
	}
	settings, ok, err := b.exportSettings(name, rule.kind, w.Settings)
	if err != nil || !ok {
		return err
	}
	b.addWidget(w.place(rule.kind, name, settings))
	return nil
}

func (b *builder) exportSettings(name Subject, kind overlays.WidgetKind, raw map[string]any) (map[string]any, bool, error) {
	if kind != overlays.WidgetCustom {
		return b.bind(name, raw).(map[string]any), true, nil
	}
	src, ok := sourceFromSettings(raw)
	if !ok {
		b.report.unsupported(name, ReasonMissingCode)
		return nil, false, nil
	}
	src.Name = string(name)
	return b.customSettings(src)
}

func sourceFromSettings(raw map[string]any) (widgetSource, bool) {
	var src widgetSource
	data, err := codec.Marshal(raw)
	if err != nil || codec.Unmarshal(data, &src) != nil {
		return src, false
	}
	return src, src.HTML != "" || src.JS != ""
}

func (b *builder) addFolderWidget(src widgetSource) error {
	settings, ok, err := b.customSettings(src)
	if err != nil || !ok {
		return err
	}
	b.addWidget(overlays.Widget{
		Name: src.Name, Kind: overlays.WidgetCustom, W: float64(b.width), H: float64(b.height),
		Opacity: 1, Settings: settings,
	})
	return nil
}

func (b *builder) addWidget(w overlays.Widget) {
	if len(b.widgets) >= overlays.MaxWidgets {
		b.report.unsupported(Subject(w.Name), ReasonTooManyWidgets)
		return
	}
	w.ID = "w" + strconv.Itoa(len(b.widgets)+1)
	b.widgets = append(b.widgets, w)
	b.report.mapped(Subject(w.Name))
}
