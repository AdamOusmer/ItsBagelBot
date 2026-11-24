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

var webpSizers = map[chunkID]func(blob) (int, int, bool){
	"VP8X": vp8xSize,
	"VP8L": vp8lSize,
	"VP8 ": vp8Size,
}

var webpImageIDs = map[chunkID]bool{"VP8 ": true, "VP8L": true, "ANMF": true}

func normalizeWebP(data blob, lim overlays.AssetLimit) (parsed, error) {
	chunks, err := parseRIFF(data, formWebP)
	if err != nil {
		return parsed{}, err
	}
	kept := keptWebPChunks(chunks)
	w, h, err := webpSize(kept)
	if err != nil {
		return parsed{}, err
	}
	if err := (dims{w: extent(w), h: extent(h)}).check(lim); err != nil {
		return parsed{}, err
	}
	if !slices.ContainsFunc(kept, func(c riffChunk) bool { return webpImageIDs[c.id] }) {
		return parsed{}, fail(CodeCorrupt)
	}
	return parsed{data: writeRIFF(formWebP, kept), w: w, h: h}, nil
}

func keptWebPChunks(chunks riffChunks) riffChunks {
	kept := make(riffChunks, 0, len(chunks))
	for _, c := range chunks {
		switch c.id {
		case "EXIF", "XMP ":
			continue
		case "VP8X":
			c.body = c.body.withoutMetadataFlags()
		}
		kept = append(kept, c)
	}
	return kept
}

func (b blob) withoutMetadataFlags() blob {
	b = slices.Clone(b)
	if len(b) > 0 {
		b[0] &^= webpFlagEXIF | webpFlagXMP
	}
	return b
}

func webpSize(chunks riffChunks) (int, int, error) {
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

func (b blob) le24() int {
	return int(b[0]) | int(b[1])<<8 | int(b[2])<<16
}

func vp8xSize(body blob) (int, int, bool) {
	if len(body) < 10 {
		return 0, 0, false
	}
	return 1 + body[4:].le24(), 1 + body[7:].le24(), true
}

func vp8lSize(body blob) (int, int, bool) {
	if len(body) < 5 || body[0] != 0x2F {
		return 0, 0, false
	}
	packed := binary.LittleEndian.Uint32(body[1:])
	return 1 + int(packed&0x3FFF), 1 + int(packed>>14&0x3FFF), true
}

func vp8Size(body blob) (int, int, bool) {
	if len(body) < 10 || !bytes.Equal(body[3:6], []byte{0x9D, 0x01, 0x2A}) {
		return 0, 0, false
	}
	w := int(binary.LittleEndian.Uint16(body[6:]) & 0x3FFF)
	h := int(binary.LittleEndian.Uint16(body[8:]) & 0x3FFF)
	return w, h, true
}
