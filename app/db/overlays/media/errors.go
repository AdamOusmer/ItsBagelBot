// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package media

import (
	"errors"

	"ItsBagelBot/internal/domain/overlays"
)

const (
	CodeUnsupported                = overlays.CodeUnsupportedMedia
	CodeCorrupt                    = overlays.CodeInvalidAsset
	CodeTooLarge                   = overlays.CodeAssetTooLarge
	CodeDimensions                 = overlays.CodeAssetDimensions
	CodeDuration                   = overlays.CodeAssetDuration
	CodeSVG          overlays.Code = "unsupported_svg"
	CodeMP4          overlays.Code = "unsupported_mp4"
	CodeHTML         overlays.Code = "unsupported_html"
	CodeCodec        overlays.Code = "unsupported_codec"
	CodeFrames       overlays.Code = "asset_frames"
	CodeSizeMismatch overlays.Code = "size_mismatch"
)

type Error struct {
	Code overlays.Code
}

func (e *Error) Error() string {
	return string(e.Code)
}

func fail(code overlays.Code) error {
	return &Error{Code: code}
}

func CodeOf(err error) overlays.Code {
	var typed *Error
	if errors.As(err, &typed) {
		return typed.Code
	}
	return ""
}
