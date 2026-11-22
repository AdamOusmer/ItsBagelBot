// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package overlays

import (
	"bytes"
	"cmp"
	"maps"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"ItsBagelBot/pkg/codec"
)

const (
	assetPrefix = "asset:"
	maxName     = 120
	minCanvas   = 320
	maxCanvas   = 7680
	maxCoord    = 7680
	maxRotation = 360
)

var (
	idPattern   = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,96}$`)
	hashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
	legacyMedia = regexp.MustCompile(`^/media/([a-f0-9]{64})\.(png|jpe?g|gif|webp|mp4|webm|mp3|ogg|wav)$`)
)

func within[T cmp.Ordered](value, low, high T) bool {
	return value >= low && value <= high
}

func Validate(doc Document) error {
	checks := []func(Document) error{validateSize, validateVersion, validateKind, validateIdentity, validateBody}
	for _, check := range checks {
		if err := check(doc); err != nil {
			return err
		}
	}
	return nil
}

func validateSize(doc Document) error {
	var buf bytes.Buffer
	encoder := codec.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(doc); err != nil {
		return fail(CodeInvalidDocument, "")
	}
	if buf.Len() > DocumentMaxBytes {
		return fail(CodeDocumentTooLarge, "")
	}
	return nil
}

func validateVersion(doc Document) error {
	if doc.Version != Version {
		return fail(CodeUnsupportedVersion, "version")
	}
	return nil
}

func validateKind(doc Document) error {
	if doc.Kind != KindSystem && doc.Kind != KindCustom {
		return fail(CodeUnknownKind, "kind")
	}
	return nil
}

func validateIdentity(doc Document) error {
	if !idPattern.MatchString(doc.ID) {
		return fail(CodeInvalidID, "id")
	}
	if !validName(doc.Name) {
		return fail(CodeInvalidName, "name")
	}
	if doc.Rev < 0 {
		return fail(CodeInvalidRevision, "rev")
	}
	return nil
}

func validName(name string) bool {
	return within(utf8.RuneCountInString(strings.TrimSpace(name)), 1, maxName)
}

func validateBody(doc Document) error {
	if doc.Kind == KindSystem {
		return validateSystem(doc)
	}
	return validateCustom(doc)
}

func validateSystem(doc Document) error {
	if systemHasContent(doc) {
		return fail(CodeSystemHasContent, "")
	}
	if doc.Template != TemplatePowered {
		return fail(CodeUnknownTemplate, "template")
	}
	if doc.Config == nil || doc.Config.Validate() != nil {
		return fail(CodeInvalidConfig, "config")
	}
	return nil
}

func systemHasContent(doc Document) bool {
	return slices.Contains([]bool{doc.Width != 0, doc.Height != 0, len(doc.Widgets) > 0, len(doc.Assets) > 0}, true)
}

func validateCustom(doc Document) error {
	if doc.Template != "" || doc.Config != nil {
		return fail(CodeInvalidDocument, "template")
	}
	if !within(doc.Width, minCanvas, maxCanvas) || !within(doc.Height, minCanvas, maxCanvas) {
		return fail(CodeInvalidSize, "width")
	}
	if err := validateWidgets(doc.Widgets); err != nil {
		return err
	}
	if err := validateAssets(doc.Assets); err != nil {
		return err
	}
	return validateRefs(doc)
}

func validateWidgets(widgets []Widget) error {
	if len(widgets) > MaxWidgets {
		return fail(CodeTooManyWidgets, "widgets")
	}
	seen := make(map[string]struct{}, len(widgets))
	for i, widget := range widgets {
		path := Path("widgets").Index(i)
		if err := validateWidget(widget, path); err != nil {
			return err
		}
		if _, duplicate := seen[widget.ID]; duplicate {
			return fail(CodeDuplicateWidgetID, path.Field("id"))
		}
		seen[widget.ID] = struct{}{}
	}
	return nil
}

func validateWidget(widget Widget, path Path) error {
	checks := []func(Widget, Path) error{validateWidgetID, validateWidgetKind, validateWidgetBox, validateWidgetSettings}
	for _, check := range checks {
		if err := check(widget, path); err != nil {
			return err
		}
	}
	return nil
}

func validateWidgetID(widget Widget, path Path) error {
	if !idPattern.MatchString(widget.ID) {
		return fail(CodeInvalidID, path.Field("id"))
	}
	return nil
}

func validateWidgetKind(widget Widget, path Path) error {
	if !slices.Contains(WidgetKinds, widget.Kind) {
		return fail(CodeUnknownWidgetKind, path.Field("kind"))
	}
	return nil
}

type bound struct {
	name            string
	value, low, max float64
}

func validateWidgetBox(widget Widget, path Path) error {
	bounds := []bound{
		{"x", widget.X, 0, maxCoord}, {"y", widget.Y, 0, maxCoord},
		{"w", widget.W, 1, maxCoord}, {"h", widget.H, 1, maxCoord},
		{"rotation", widget.Rotation, -maxRotation, maxRotation}, {"opacity", widget.Opacity, 0, 1},
	}
	for _, b := range bounds {
		if !within(b.value, b.low, b.max) {
			return fail(CodeInvalidWidget, path.Field(b.name))
		}
	}
	if !within(utf8.RuneCountInString(widget.Name), 1, maxName) {
		return fail(CodeInvalidWidget, path.Field("name"))
	}
	return nil
}

func validateWidgetSettings(widget Widget, path Path) error {
	if widget.Settings == nil {
		return fail(CodeInvalidWidget, path.Field("settings"))
	}
	return nil
}

func validateBundle(widget Widget, assets map[string]Asset, path Path) error {
	if widget.Kind != WidgetCustom {
		return nil
	}
	ref, _ := widget.Settings["bundle"].(string)
	id, isRef := strings.CutPrefix(ref, assetPrefix)
	if !isRef {
		return fail(CodeInvalidBundle, path)
	}
	asset, found := assets[id]
	if !found {
		return fail(CodeUnknownAsset, path)
	}
	if asset.Type != AssetWidget {
		return fail(CodeInvalidBundle, path)
	}
	return nil
}

func validateAssets(assets map[string]Asset) error {
	if len(assets) > OverlayMaxAssets {
		return fail(CodeTooManyAssets, "assets")
	}
	var total int64
	perType := make(map[AssetType]int)
	for _, id := range slices.Sorted(maps.Keys(assets)) {
		path := Path("assets").Field(id)
		if !idPattern.MatchString(id) {
			return fail(CodeInvalidID, path)
		}
		if err := validateAsset(assets[id], path); err != nil {
			return err
		}
		total += assets[id].Bytes
		perType[assets[id].Type]++
	}
	return validateTotals(total, perType)
}

func validateTotals(total int64, perType map[AssetType]int) error {
	if total > OverlayMaxAssetBytes {
		return fail(CodeOverlayAssetsTooLarge, "assets")
	}
	for kind, count := range perType {
		if limit, _ := AssetLimitFor(kind); limit.MaxPerOverlay > 0 && count > limit.MaxPerOverlay {
			return fail(CodeTooManyOfType, "assets")
		}
	}
	return nil
}

func validateAsset(asset Asset, path Path) error {
	limit, known := AssetLimitFor(asset.Type)
	if !known || !asset.wellFormed() {
		return fail(CodeInvalidAsset, path)
	}
	if asset.Bytes > limit.MaxBytes {
		return fail(CodeAssetTooLarge, path.Field("bytes"))
	}
	if !limit.fitsFrame(asset.W, asset.H) {
		return fail(CodeAssetDimensions, path)
	}
	if !limit.fitsDuration(asset.DurationMS) {
		return fail(CodeAssetDuration, path.Field("duration_ms"))
	}
	return nil
}

func (a Asset) wellFormed() bool {
	return hashPattern.MatchString(a.SHA256) && a.Bytes > 0 && min(a.W, a.H, a.DurationMS) >= 0
}

func validateRefs(doc Document) error {
	for i, widget := range doc.Widgets {
		path := Path("widgets").Index(i).Field("settings")
		if err := validateBundle(widget, doc.Assets, path); err != nil {
			return err
		}
		if _, code := mapStrings(refRoot(widget), checkRef(doc.Assets)); code != "" {
			return fail(code, path)
		}
	}
	return nil
}

func refRoot(widget Widget) any {
	if widget.Kind == WidgetCustom {
		return widget.Settings["values"]
	}
	return widget.Settings
}

func checkRef(assets map[string]Asset) func(string) (string, Code) {
	return func(text string) (string, Code) {
		if legacyMedia.MatchString(text) {
			return text, CodeLegacyMediaRef
		}
		if id, isRef := strings.CutPrefix(text, assetPrefix); isRef {
			if _, found := assets[id]; !found {
				return text, CodeUnknownAsset
			}
		}
		return text, ""
	}
}

func mapStrings(value any, fn func(string) (string, Code)) (any, Code) {
	switch node := value.(type) {
	case string:
		return fn(node)
	case map[string]any:
		return mapEntries(node, fn)
	case []any:
		return mapItems(node, fn)
	}
	return value, ""
}

func mapEntries(entries map[string]any, fn func(string) (string, Code)) (any, Code) {
	out := make(map[string]any, len(entries))
	for key, item := range entries {
		next, code := mapStrings(item, fn)
		if code != "" {
			return nil, code
		}
		out[key] = next
	}
	return out, ""
}

func mapItems(items []any, fn func(string) (string, Code)) (any, Code) {
	out := make([]any, len(items))
	for i, item := range items {
		next, code := mapStrings(item, fn)
		if code != "" {
			return nil, code
		}
		out[i] = next
	}
	return out, ""
}
