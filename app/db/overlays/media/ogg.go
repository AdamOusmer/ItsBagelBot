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

type granule int64

type oggPage struct {
	flags   byte
	granule granule
	serial  uint32
	payload blob
}

type oggStream struct {
	rate    int64
	preSkip granule
}

func normalizeOGG(data blob, lim overlays.AssetLimit) (parsed, error) {
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
	ms, err := stream.duration(pages[len(pages)-1].granule).check(lim)
	if err != nil {
		return parsed{}, err
	}
	return parsed{data: data, durationMS: ms}, nil
}

func parseOgg(data blob) ([]oggPage, error) {
	var pages []oggPage
	for len(data) > 0 {
		page, rest, err := data.oggNext()
		if err != nil {
			return nil, err
		}
		pages = append(pages, page)
		data = rest
	}
	return pages, nil
}

func (d blob) oggNext() (oggPage, blob, error) {
	if len(d) < oggHeader || !bytes.HasPrefix(d, []byte("OggS\x00")) {
		return oggPage{}, nil, fail(CodeCorrupt)
	}
	head := oggHeader + offset(d[26])
	if d.end() < head {
		return oggPage{}, nil, fail(CodeCorrupt)
	}
	end := head
	for _, segment := range d[oggHeader:head] {
		end += offset(segment)
	}
	if d.end() < end {
		return oggPage{}, nil, fail(CodeCorrupt)
	}
	page := oggPage{
		flags:   d[5],
		granule: granule(binary.LittleEndian.Uint64(d[6:])),
		serial:  binary.LittleEndian.Uint32(d[14:]),
		payload: d[head:end],
	}
	return page, d[end:], nil
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

func oggIdentify(packet blob) (oggStream, error) {
	switch {
	case bytes.HasPrefix(packet, []byte("\x01vorbis")) && len(packet) >= 16:
		return oggStream{rate: int64(binary.LittleEndian.Uint32(packet[12:]))}, nil
	case bytes.HasPrefix(packet, []byte("OpusHead")) && len(packet) >= 19:
		return oggStream{rate: 48000, preSkip: granule(binary.LittleEndian.Uint16(packet[10:]))}, nil
	}
	return oggStream{}, fail(CodeCodec)
}

func (s oggStream) duration(end granule) millis {
	if s.rate <= 0 {
		return 0
	}
	return millis(float64(end-s.preSkip) * 1000 / float64(s.rate))
}
