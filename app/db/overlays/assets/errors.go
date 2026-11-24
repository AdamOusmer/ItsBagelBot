// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package assets

import "errors"

var (
	ErrAlphaRequired = errors.New("alpha_required")
	ErrQuota         = errors.New("quota_exceeded")
	ErrRateLimited   = errors.New("rate_limited")
	ErrInvalidSize   = errors.New("invalid_size")
	ErrInvalidPart   = errors.New("invalid_part")
	ErrIncomplete    = errors.New("incomplete_upload")
)
