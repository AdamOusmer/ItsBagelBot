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

func normalizeMP3(data []byte, lim overlays.AssetLimit) (parsed, error) {
	start, err := mp3AudioStart(data)
	if err != nil {
		return parsed{}, err
	}
	end, seconds := scanMP3(data, start)
	if end == start {
		return parsed{}, fail(CodeCorrupt)
	}
	ms, err := checkDuration(lim, seconds*1000)
	if err != nil {
		return parsed{}, err
	}
	return parsed{data: data[start:end], durationMS: ms}, nil
}

func mp3AudioStart(data []byte) (int, error) {
	if !bytes.HasPrefix(data, []byte("ID3")) {
		return 0, nil
	}
	if len(data) < 10 {
		return 0, fail(CodeCorrupt)
	}
	tagSize := int(data[6]&0x7F)<<21 | int(data[7]&0x7F)<<14 | int(data[8]&0x7F)<<7 | int(data[9]&0x7F)
	start := 10 + tagSize
	if data[5]&0x10 != 0 {
		start += 10
	}
	if start >= len(data) {
		return 0, fail(CodeCorrupt)
	}
	return start, nil
}

func scanMP3(data []byte, start int) (int, float64) {
	pos, seconds := start, 0.0
	for {
		frame, ok := mp3Header(data[pos:])
		if !ok || pos+frame.length > len(data) {
			return pos, seconds
		}
		pos += frame.length
		seconds += float64(frame.samples) / float64(frame.rate)
	}
}

func mp3Header(b []byte) (mp3Frame, bool) {
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
