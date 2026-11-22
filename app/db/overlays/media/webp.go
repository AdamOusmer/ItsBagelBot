// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package media

import (
	"bytes"
	"encoding/binary"
	"slices"

	"ItsBagelBot/internal/domain/overlays"
)

const (
	webpFlagEXIF = 0x08
	webpFlagXMP  = 0x04
)

var webpSizers = map[string]func([]byte) (int, int, bool){
	"VP8X": vp8xSize,
	"VP8L": vp8lSize,
	"VP8 ": vp8Size,
}

var webpImageIDs = map[string]bool{"VP8 ": true, "VP8L": true, "ANMF": true}

func normalizeWebP(data []byte, lim overlays.AssetLimit) (parsed, error) {
	chunks, err := parseRIFF(data, "WEBP")
	if err != nil {
		return parsed{}, err
	}
	kept := keptWebPChunks(chunks)
	w, h, err := webpSize(kept)
	if err != nil {
		return parsed{}, err
	}
	if err := checkFrame(lim, w, h); err != nil {
		return parsed{}, err
	}
	if !slices.ContainsFunc(kept, func(c riffChunk) bool { return webpImageIDs[c.id] }) {
		return parsed{}, fail(CodeCorrupt)
	}
	return parsed{data: writeRIFF("WEBP", kept), w: w, h: h}, nil
}

func keptWebPChunks(chunks []riffChunk) []riffChunk {
	kept := make([]riffChunk, 0, len(chunks))
	for _, c := range chunks {
		switch c.id {
		case "EXIF", "XMP ":
			continue
		case "VP8X":
			c.body = withoutMetadataFlags(c.body)
		}
		kept = append(kept, c)
	}
	return kept
}

func withoutMetadataFlags(body []byte) []byte {
	body = slices.Clone(body)
	if len(body) > 0 {
		body[0] &^= webpFlagEXIF | webpFlagXMP
	}
	return body
}

func webpSize(chunks []riffChunk) (int, int, error) {
	if len(chunks) == 0 {
		return 0, 0, fail(CodeCorrupt)
	}
	sizer, ok := webpSizers[chunks[0].id]
	if !ok {
		return 0, 0, fail(CodeCorrupt)
	}
	w, h, ok := sizer(chunks[0].body)
	if !ok {
		return 0, 0, fail(CodeCorrupt)
	}
	return w, h, nil
}

func le24(b []byte) int {
	return int(b[0]) | int(b[1])<<8 | int(b[2])<<16
}

func vp8xSize(body []byte) (int, int, bool) {
	if len(body) < 10 {
		return 0, 0, false
	}
	return 1 + le24(body[4:]), 1 + le24(body[7:]), true
}

func vp8lSize(body []byte) (int, int, bool) {
	if len(body) < 5 || body[0] != 0x2F {
		return 0, 0, false
	}
	bits := binary.LittleEndian.Uint32(body[1:])
	return 1 + int(bits&0x3FFF), 1 + int(bits>>14&0x3FFF), true
}

func vp8Size(body []byte) (int, int, bool) {
	if len(body) < 10 || !bytes.Equal(body[3:6], []byte{0x9D, 0x01, 0x2A}) {
		return 0, 0, false
	}
	w := int(binary.LittleEndian.Uint16(body[6:]) & 0x3FFF)
	h := int(binary.LittleEndian.Uint16(body[8:]) & 0x3FFF)
	return w, h, true
}
