// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package media

import (
	"encoding/binary"

	"ItsBagelBot/internal/domain/overlays"
)

var wavFormats = map[uint16]bool{0x0001: true, 0x0003: true, 0xFFFE: true}

func normalizeWAV(data []byte, lim overlays.AssetLimit) (parsed, error) {
	chunks, err := parseRIFF(data, "WAVE")
	if err != nil {
		return parsed{}, err
	}
	format, err := requireChunk(chunks, "fmt ")
	if err != nil {
		return parsed{}, err
	}
	pcm, err := requireChunk(chunks, "data")
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
	ms, err := checkDuration(lim, float64(len(pcm.body))*1000/float64(byteRate))
	if err != nil {
		return parsed{}, err
	}
	return parsed{data: writeRIFF("WAVE", []riffChunk{format, pcm}), durationMS: ms}, nil
}
