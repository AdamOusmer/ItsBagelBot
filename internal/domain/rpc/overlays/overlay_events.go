// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package overlaysrpc

import "strconv"

const (
	OverlayRevisedPrefix = "controls.overlay.revised."
	OverlayRevokedPrefix = "controls.overlay.revoked."
)

func OverlayRevisedSubject(overlayID uint64) string {
	return OverlayRevisedPrefix + strconv.FormatUint(overlayID, 10)
}

func OverlayRevokedSubject(overlayID uint64) string {
	return OverlayRevokedPrefix + strconv.FormatUint(overlayID, 10)
}
