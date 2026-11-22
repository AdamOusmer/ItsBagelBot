// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package media

import (
	"encoding/binary"
	"math"
	"math/bits"

	"ItsBagelBot/internal/domain/overlays"
)

const (
	idEBML          = 0x1A45DFA3
	idSegment       = 0x18538067
	idInfo          = 0x1549A966
	idTracks        = 0x1654AE6B
	idCluster       = 0x1F43B675
	idDocType       = 0x4282
	idTimecodeScale = 0x2AD7B1
	idDuration      = 0x4489
	idTrackEntry    = 0xAE
	idTrackType     = 0x83
	idCodecID       = 0x86
	idVideo         = 0xE0
	idPixelWidth    = 0xB0
	idPixelHeight   = 0xBA

	trackVideo = 1
	trackAudio = 2

	defaultTimecodeScale = 1_000_000
)

var webmCodecs = map[uint64]map[string]bool{
	trackVideo: {"V_VP8": true, "V_VP9": true, "V_AV1": true},
	trackAudio: {"A_OPUS": true, "A_VORBIS": true},
}

type ebmlElement struct {
	id   uint32
	body []byte
}

type webmTrack struct {
	kind  uint64
	codec string
	w     int
	h     int
}

func normalizeWebM(data []byte, lim overlays.AssetLimit) (parsed, error) {
	header, segment, err := webmSections(data)
	if err != nil {
		return parsed{}, err
	}
	if err := checkDocType(header); err != nil {
		return parsed{}, err
	}
	seg, err := ebmlChildren(segment, idCluster)
	if err != nil {
		return parsed{}, err
	}
	if len(seg) == 0 || seg[len(seg)-1].id != idCluster {
		return parsed{}, fail(CodeCorrupt)
	}
	ms, err := webmDuration(seg, lim)
	if err != nil {
		return parsed{}, err
	}
	w, h, err := webmVideo(seg, lim)
	if err != nil {
		return parsed{}, err
	}
	return parsed{data: data, w: w, h: h, durationMS: ms}, nil
}

func webmSections(data []byte) ([]byte, []byte, error) {
	top, err := ebmlChildren(data, 0)
	if err != nil {
		return nil, nil, err
	}
	if len(top) != 2 {
		return nil, nil, fail(CodeCorrupt)
	}
	if top[0].id != idEBML || top[1].id != idSegment {
		return nil, nil, fail(CodeCorrupt)
	}
	return top[0].body, top[1].body, nil
}

func checkDocType(header []byte) error {
	fields, err := ebmlChildren(header, 0)
	if err != nil {
		return err
	}
	docType, _ := ebmlFind(fields, idDocType)
	if string(docType) != "webm" {
		return fail(CodeUnsupported)
	}
	return nil
}

func webmDuration(seg []ebmlElement, lim overlays.AssetLimit) (int, error) {
	body, ok := ebmlFind(seg, idInfo)
	if !ok {
		return 0, fail(CodeCorrupt)
	}
	info, err := ebmlChildren(body, 0)
	if err != nil {
		return 0, err
	}
	scale := uint64(defaultTimecodeScale)
	if raw, ok := ebmlFind(info, idTimecodeScale); ok {
		scale = ebmlUint(raw)
	}
	raw, ok := ebmlFind(info, idDuration)
	if !ok {
		return 0, fail(CodeCorrupt)
	}
	ticks, err := ebmlFloat(raw)
	if err != nil {
		return 0, err
	}
	return checkDuration(lim, ticks*float64(scale)/1e6)
}

func webmVideo(seg []ebmlElement, lim overlays.AssetLimit) (int, int, error) {
	tracks, err := webmTracks(seg)
	if err != nil {
		return 0, 0, err
	}
	w, h := 0, 0
	for _, t := range tracks {
		if err := checkTrack(t, lim); err != nil {
			return 0, 0, err
		}
		if t.kind == trackVideo {
			w, h = t.w, t.h
		}
	}
	if w == 0 {
		return 0, 0, fail(CodeCorrupt)
	}
	return w, h, nil
}

func checkTrack(t webmTrack, lim overlays.AssetLimit) error {
	if !webmCodecs[t.kind][t.codec] {
		return fail(CodeCodec)
	}
	if t.kind == trackVideo {
		return checkFrame(lim, t.w, t.h)
	}
	return nil
}

func webmTracks(seg []ebmlElement) ([]webmTrack, error) {
	body, ok := ebmlFind(seg, idTracks)
	if !ok {
		return nil, fail(CodeCorrupt)
	}
	entries, err := ebmlChildren(body, 0)
	if err != nil {
		return nil, err
	}
	var tracks []webmTrack
	for _, entry := range entries {
		if entry.id != idTrackEntry {
			continue
		}
		track, err := webmTrackOf(entry.body)
		if err != nil {
			return nil, err
		}
		tracks = append(tracks, track)
	}
	return tracks, nil
}

func webmTrackOf(body []byte) (webmTrack, error) {
	fields, err := ebmlChildren(body, 0)
	if err != nil {
		return webmTrack{}, err
	}
	kind, _ := ebmlFind(fields, idTrackType)
	codec, _ := ebmlFind(fields, idCodecID)
	track := webmTrack{kind: ebmlUint(kind), codec: string(codec)}
	video, ok := ebmlFind(fields, idVideo)
	if !ok {
		return track, nil
	}
	dims, err := ebmlChildren(video, 0)
	if err != nil {
		return webmTrack{}, err
	}
	w, _ := ebmlFind(dims, idPixelWidth)
	h, _ := ebmlFind(dims, idPixelHeight)
	track.w, track.h = int(ebmlUint(w)), int(ebmlUint(h))
	return track, nil
}

func ebmlChildren(data []byte, stop uint32) ([]ebmlElement, error) {
	var out []ebmlElement
	for len(data) > 0 {
		if id, _, err := ebmlID(data); err == nil && id == stop {
			return append(out, ebmlElement{id: id}), nil
		}
		elem, rest, err := ebmlNext(data)
		if err != nil {
			return nil, err
		}
		out = append(out, elem)
		data = rest
	}
	return out, nil
}

func ebmlNext(data []byte) (ebmlElement, []byte, error) {
	id, idLen, err := ebmlID(data)
	if err != nil {
		return ebmlElement{}, nil, err
	}
	size, sizeLen, err := ebmlSize(data[idLen:])
	if err != nil {
		return ebmlElement{}, nil, err
	}
	rest := data[idLen+sizeLen:]
	if size > uint64(len(rest)) {
		return ebmlElement{}, nil, fail(CodeCorrupt)
	}
	return ebmlElement{id: id, body: rest[:size]}, rest[size:], nil
}

func ebmlID(data []byte) (uint32, int, error) {
	if len(data) == 0 || data[0] == 0 {
		return 0, 0, fail(CodeCorrupt)
	}
	n := bits.LeadingZeros8(data[0]) + 1
	if n > 4 || len(data) < n {
		return 0, 0, fail(CodeCorrupt)
	}
	var id uint32
	for _, b := range data[:n] {
		id = id<<8 | uint32(b)
	}
	return id, n, nil
}

func ebmlSize(data []byte) (uint64, int, error) {
	if len(data) == 0 || data[0] == 0 {
		return 0, 0, fail(CodeCorrupt)
	}
	n := bits.LeadingZeros8(data[0]) + 1
	if len(data) < n {
		return 0, 0, fail(CodeCorrupt)
	}
	marker := uint64(0xFF) >> n
	value := uint64(data[0]) & marker
	unknown := value == marker
	for _, b := range data[1:n] {
		value = value<<8 | uint64(b)
		unknown = unknown && b == 0xFF
	}
	if unknown {
		return uint64(len(data) - n), n, nil
	}
	return value, n, nil
}

func ebmlFind(elems []ebmlElement, id uint32) ([]byte, bool) {
	for _, e := range elems {
		if e.id == id {
			return e.body, true
		}
	}
	return nil, false
}

func ebmlUint(body []byte) uint64 {
	var value uint64
	for _, b := range body[:min(len(body), 8)] {
		value = value<<8 | uint64(b)
	}
	return value
}

func ebmlFloat(body []byte) (float64, error) {
	switch len(body) {
	case 4:
		return float64(math.Float32frombits(binary.BigEndian.Uint32(body))), nil
	case 8:
		return math.Float64frombits(binary.BigEndian.Uint64(body)), nil
	}
	return 0, fail(CodeCorrupt)
}
