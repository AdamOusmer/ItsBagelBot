// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package media

import (
	"bytes"
	"encoding/binary"
	"image/gif"

	"ItsBagelBot/internal/domain/overlays"
)

const gifMaxTotalPixels = 1 << 26

type gifScan struct {
	w      int
	h      int
	frames int
	pixels int64
}

func normalizeGIF(data []byte, lim overlays.AssetLimit) (parsed, error) {
	info, err := scanGIF(data)
	if err != nil {
		return parsed{}, err
	}
	if err := checkGIF(info, lim); err != nil {
		return parsed{}, err
	}
	img, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		return parsed{}, fail(CodeCorrupt)
	}
	var out bytes.Buffer
	if err := gif.EncodeAll(&out, img); err != nil {
		return parsed{}, fail(CodeCorrupt)
	}
	return parsed{data: out.Bytes(), w: info.w, h: info.h, durationMS: gifDurationMS(img.Delay)}, nil
}

func checkGIF(info gifScan, lim overlays.AssetLimit) error {
	if info.frames == 0 {
		return fail(CodeCorrupt)
	}
	if info.frames > lim.MaxFrames {
		return fail(CodeFrames)
	}
	if info.pixels > gifMaxTotalPixels {
		return fail(CodeDimensions)
	}
	return checkFrame(lim, info.w, info.h)
}

func gifDurationMS(delays []int) int {
	total := 0
	for _, d := range delays {
		total += d * 10
	}
	return total
}

func gifTableSize(packed byte) int {
	if packed&0x80 == 0 {
		return 0
	}
	return 3 << (packed&7 + 1)
}

func scanGIF(data []byte) (gifScan, error) {
	if len(data) < 13 {
		return gifScan{}, fail(CodeCorrupt)
	}
	info := gifScan{
		w: int(binary.LittleEndian.Uint16(data[6:])),
		h: int(binary.LittleEndian.Uint16(data[8:])),
	}
	pos := 13 + gifTableSize(data[10])
	for {
		next, done, err := info.block(data, pos)
		if err != nil {
			return gifScan{}, err
		}
		if done {
			return info, nil
		}
		pos = next
	}
}

func (g *gifScan) block(data []byte, pos int) (int, bool, error) {
	if pos >= len(data) {
		return 0, false, fail(CodeCorrupt)
	}
	switch data[pos] {
	case 0x3B:
		return pos, true, nil
	case 0x21:
		next, err := skipSubBlocks(data, pos+2)
		return next, false, err
	case 0x2C:
		return g.frame(data, pos)
	}
	return 0, false, fail(CodeCorrupt)
}

func (g *gifScan) frame(data []byte, pos int) (int, bool, error) {
	if pos+10 > len(data) {
		return 0, false, fail(CodeCorrupt)
	}
	w := int(binary.LittleEndian.Uint16(data[pos+5:]))
	h := int(binary.LittleEndian.Uint16(data[pos+7:]))
	g.frames++
	g.pixels += int64(w) * int64(h)
	next, err := skipSubBlocks(data, pos+11+gifTableSize(data[pos+9]))
	return next, false, err
}

func skipSubBlocks(data []byte, pos int) (int, error) {
	for pos < len(data) {
		size := int(data[pos])
		pos += 1 + size
		if size == 0 {
			return pos, nil
		}
	}
	return 0, fail(CodeCorrupt)
}
