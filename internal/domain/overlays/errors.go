// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package overlays

import "strconv"

type Code string

const (
	CodeInvalidDocument       Code = "invalid_document"
	CodeUnsupportedVersion    Code = "unsupported_version"
	CodeUnknownKind           Code = "unknown_kind"
	CodeInvalidID             Code = "invalid_id"
	CodeInvalidName           Code = "invalid_name"
	CodeInvalidRevision       Code = "invalid_revision"
	CodeInvalidSize           Code = "invalid_size"
	CodeUnknownTemplate       Code = "unknown_template"
	CodeInvalidConfig         Code = "invalid_config"
	CodeSystemHasContent      Code = "system_has_content"
	CodeTooManyWidgets        Code = "too_many_widgets"
	CodeDuplicateWidgetID     Code = "duplicate_widget_id"
	CodeUnknownWidgetKind     Code = "unknown_widget_kind"
	CodeInvalidWidget         Code = "invalid_widget"
	CodeInvalidBundle         Code = "invalid_bundle"
	CodeUnknownAsset          Code = "unknown_asset"
	CodeLegacyMediaRef        Code = "legacy_media_ref"
	CodeUnsupportedMedia      Code = "unsupported_media"
	CodeInvalidAsset          Code = "invalid_asset"
	CodeAssetTooLarge         Code = "asset_too_large"
	CodeAssetDimensions       Code = "asset_dimensions"
	CodeAssetDuration         Code = "asset_duration"
	CodeTooManyAssets         Code = "too_many_assets"
	CodeTooManyOfType         Code = "too_many_of_type"
	CodeOverlayAssetsTooLarge Code = "overlay_assets_too_large"
	CodeDocumentTooLarge      Code = "document_too_large"
)

type Path string

func (p Path) Field(name string) Path {
	if p == "" {
		return Path(name)
	}
	return p + "." + Path(name)
}

func (p Path) Index(i int) Path {
	return p + "[" + Path(strconv.Itoa(i)) + "]"
}

type Error struct {
	Code Code
	Path Path
}

func (e *Error) Error() string {
	if e.Path == "" {
		return string(e.Code)
	}
	return string(e.Code) + " at " + string(e.Path)
}

func fail(code Code, path Path) *Error {
	return &Error{Code: code, Path: path}
}
