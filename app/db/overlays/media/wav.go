// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package media

import (
	"encoding/binary"

	"ItsBagelBot/internal/domain/overlays"
)

var wavFormats = map[uint16]bool{0x0001: true, 0x0003: true, 0xFFFE: true}

func normalizeWAV(data blob, lim overlays.AssetLimit) (parsed, error) {
	chunks, err := parseRIFF(data, formWave)
	if err != nil {
		return parsed{}, err
	}
	format, err := chunks.require("fmt ")
	if err != nil {
		return parsed{}, err
	}
	pcm, err := chunks.require("data")
	if err != nil {
		return parsed{}, err
	}
	if len(format.body) < 16 {
		return parsed{}, fail(CodeCorrupt)
	}
	if !wavFormats[binary.LittleEndian.Uint16(format.body)] {
		return parsed{}, fail(CodeCodec)
	}
	byteRate := binary.LittleEndian.Uint32(format.body[8:])
	if byteRate == 0 {
		return parsed{}, fail(CodeCorrupt)
	}
	ms, err := millis(float64(len(pcm.body)) * 1000 / float64(byteRate)).check(lim)
	if err != nil {
		return parsed{}, err
	}
	return parsed{data: writeRIFF(formWave, riffChunks{format, pcm}), durationMS: ms}, nil
}
