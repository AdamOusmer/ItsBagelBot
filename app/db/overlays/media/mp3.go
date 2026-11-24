// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package media

import (
	"bytes"
	"encoding/binary"

	"ItsBagelBot/internal/domain/overlays"
)

type mp3Frame struct {
	length  int
	samples int
	rate    int
}

var mp3Rates = [4][4]int{
	0: {11025, 12000, 8000},
	2: {22050, 24000, 16000},
	3: {44100, 48000, 32000},
}

var mpeg2Bitrates = [16]int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0}

var mp3Bitrates = [4][16]int{
	0: mpeg2Bitrates,
	2: mpeg2Bitrates,
	3: {0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0},
}

func normalizeMP3(data blob, lim overlays.AssetLimit) (parsed, error) {
	start, err := data.mp3AudioStart()
	if err != nil {
		return parsed{}, err
	}
	end, seconds := data.scanMP3(start)
	if end == start {
		return parsed{}, fail(CodeCorrupt)
	}
	ms, err := millis(seconds * 1000).check(lim)
	if err != nil {
		return parsed{}, err
	}
	return parsed{data: data[start:end], durationMS: ms}, nil
}

func (d blob) mp3AudioStart() (offset, error) {
	if !bytes.HasPrefix(d, []byte("ID3")) {
		return 0, nil
	}
	if len(d) < 10 {
		return 0, fail(CodeCorrupt)
	}
	tagSize := int(d[6]&0x7F)<<21 | int(d[7]&0x7F)<<14 | int(d[8]&0x7F)<<7 | int(d[9]&0x7F)
	start := offset(10 + tagSize)
	if d[5]&0x10 != 0 {
		start += 10
	}
	if start >= d.end() {
		return 0, fail(CodeCorrupt)
	}
	return start, nil
}

func (d blob) scanMP3(start offset) (offset, float64) {
	pos, seconds := start, 0.0
	for {
		next, ok := d[pos:].mp3Header()
		if !ok || pos+offset(next.length) > d.end() {
			return pos, seconds
		}
		pos += offset(next.length)
		seconds += float64(next.samples) / float64(next.rate)
	}
}

func (b blob) mp3Header() (mp3Frame, bool) {
	if len(b) < 4 || binary.BigEndian.Uint16(b)&0xFFE6 != 0xFFE2 {
		return mp3Frame{}, false
	}
	version := int(b[1] >> 3 & 3)
	rate := mp3Rates[version][b[2]>>2&3]
	bitrate := mp3Bitrates[version][b[2]>>4] * 1000
	if rate == 0 || bitrate == 0 {
		return mp3Frame{}, false
	}
	samples := 576
	if version == 3 {
		samples = 1152
	}
	return mp3Frame{
		length:  samples/8*bitrate/rate + int(b[2]>>1&1),
		samples: samples,
		rate:    rate,
	}, true
}
