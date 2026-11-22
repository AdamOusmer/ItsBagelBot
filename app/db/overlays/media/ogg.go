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
	oggFlagBOS = 0x02
	oggFlagEOS = 0x04
	oggHeader  = 27
)

type oggPage struct {
	flags   byte
	granule int64
	serial  uint32
	payload []byte
}

type oggStream struct {
	rate    int64
	preSkip int64
}

func normalizeOGG(data []byte, lim overlays.AssetLimit) (parsed, error) {
	pages, err := parseOgg(data)
	if err != nil {
		return parsed{}, err
	}
	if err := checkOggStream(pages); err != nil {
		return parsed{}, err
	}
	stream, err := oggIdentify(pages[0].payload)
	if err != nil {
		return parsed{}, err
	}
	ms, err := checkDuration(lim, stream.durationMS(pages[len(pages)-1].granule))
	if err != nil {
		return parsed{}, err
	}
	return parsed{data: data, durationMS: ms}, nil
}

func parseOgg(data []byte) ([]oggPage, error) {
	var pages []oggPage
	for len(data) > 0 {
		page, rest, err := oggNext(data)
		if err != nil {
			return nil, err
		}
		pages = append(pages, page)
		data = rest
	}
	return pages, nil
}

func oggNext(data []byte) (oggPage, []byte, error) {
	if len(data) < oggHeader || !bytes.HasPrefix(data, []byte("OggS\x00")) {
		return oggPage{}, nil, fail(CodeCorrupt)
	}
	head := oggHeader + int(data[26])
	if len(data) < head {
		return oggPage{}, nil, fail(CodeCorrupt)
	}
	end := head
	for _, segment := range data[oggHeader:head] {
		end += int(segment)
	}
	if len(data) < end {
		return oggPage{}, nil, fail(CodeCorrupt)
	}
	page := oggPage{
		flags:   data[5],
		granule: int64(binary.LittleEndian.Uint64(data[6:])),
		serial:  binary.LittleEndian.Uint32(data[14:]),
		payload: data[head:end],
	}
	return page, data[end:], nil
}

func checkOggStream(pages []oggPage) error {
	first, last := pages[0], pages[len(pages)-1]
	if first.flags&oggFlagBOS == 0 || last.flags&oggFlagEOS == 0 {
		return fail(CodeCorrupt)
	}
	if slices.ContainsFunc(pages, func(p oggPage) bool { return p.serial != first.serial }) {
		return fail(CodeCorrupt)
	}
	return nil
}

func oggIdentify(packet []byte) (oggStream, error) {
	switch {
	case bytes.HasPrefix(packet, []byte("\x01vorbis")) && len(packet) >= 16:
		return oggStream{rate: int64(binary.LittleEndian.Uint32(packet[12:]))}, nil
	case bytes.HasPrefix(packet, []byte("OpusHead")) && len(packet) >= 19:
		return oggStream{rate: 48000, preSkip: int64(binary.LittleEndian.Uint16(packet[10:]))}, nil
	}
	return oggStream{}, fail(CodeCodec)
}

func (s oggStream) durationMS(granule int64) float64 {
	if s.rate <= 0 {
		return 0
	}
	return float64(granule-s.preSkip) * 1000 / float64(s.rate)
}
