// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package media

import (
	"encoding/binary"

	"ItsBagelBot/internal/domain/overlays"
)

const woff2HeaderBytes = 48

var woff2Flavors = map[uint32]bool{0x00010000: true, 0x4F54544F: true}

func normalizeWOFF2(data []byte, _ overlays.AssetLimit) (parsed, error) {
	if len(data) < woff2HeaderBytes {
		return parsed{}, fail(CodeCorrupt)
	}
	if int(binary.BigEndian.Uint32(data[8:])) != len(data) {
		return parsed{}, fail(CodeCorrupt)
	}
	if !woff2Flavors[binary.BigEndian.Uint32(data[4:])] || binary.BigEndian.Uint16(data[12:]) == 0 {
		return parsed{}, fail(CodeCorrupt)
	}
	return parsed{data: data}, nil
}
