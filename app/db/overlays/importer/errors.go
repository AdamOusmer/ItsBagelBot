// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package importer

type Code string

const (
	CodeInvalidArchive  Code = "invalid_archive"
	CodeUnsafePath      Code = "unsafe_path"
	CodeSymlink         Code = "symlink"
	CodeNestedArchive   Code = "nested_archive"
	CodeTooManyEntries  Code = "too_many_entries"
	CodeTooLarge        Code = "too_large"
	CodeBombRatio       Code = "bomb_ratio"
	CodeInvalidManifest Code = "invalid_manifest"
	CodeInvalidDocument Code = "invalid_document"
	CodeInvalidBundle   Code = "invalid_bundle"
	CodeBadHash         Code = "bad_hash"
	CodeMissingAsset    Code = "missing_asset"
)

type Error struct {
	Code Code
	Path string
}

func (e *Error) Error() string {
	if e.Path == "" {
		return string(e.Code)
	}
	return string(e.Code) + " at " + e.Path
}

type Reason string

const (
	ReasonLicenseFile       Reason = "license_file"
	ReasonConvertToWebM     Reason = "convert_to_webm"
	ReasonConvertToWOFF2    Reason = "convert_to_woff2"
	ReasonRejectedType      Reason = "rejected_type"
	ReasonUnsupportedType   Reason = "unsupported_type"
	ReasonCodeOutsideBundle Reason = "code_outside_bundle"
	ReasonEmpty             Reason = "empty_file"
	ReasonTooLarge          Reason = "file_too_large"
	ReasonTooManyAssets     Reason = "too_many_assets"
	ReasonTooManyOfType     Reason = "too_many_of_type"
	ReasonTooManyWidgets    Reason = "too_many_widgets"
	ReasonNotInManifest     Reason = "not_in_manifest"
	ReasonUnknownWidget     Reason = "unknown_widget"
	ReasonMissingCode       Reason = "missing_widget_code"
	ReasonNotText           Reason = "widget_not_text"
	ReasonTips              Reason = "tips_merch"
	ReasonStore             Reason = "points_store"
	ReasonContests          Reason = "contests_bets"
	ReasonMediaShare        Reason = "media_share"
	ReasonKeyValue          Reason = "key_value_store"
	ReasonRemoteFont        Reason = "remote_font"
	ReasonNetwork           Reason = "network_call"
	ReasonApproximatedFont  Reason = "approximated_font"
	ReasonUnknownField      Reason = "unknown_field_type"
)
