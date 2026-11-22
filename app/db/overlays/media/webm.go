// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package media

import (
	"encoding/binary"
	"math"
	"math/bits"

	"ItsBagelBot/internal/domain/overlays"
)

type ebmlID uint32

const (
	idEBML          ebmlID = 0x1A45DFA3
	idSegment       ebmlID = 0x18538067
	idInfo          ebmlID = 0x1549A966
	idTracks        ebmlID = 0x1654AE6B
	idCluster       ebmlID = 0x1F43B675
	idDocType       ebmlID = 0x4282
	idTimecodeScale ebmlID = 0x2AD7B1
	idDuration      ebmlID = 0x4489
	idTrackEntry    ebmlID = 0xAE
	idTrackType     ebmlID = 0x83
	idCodecID       ebmlID = 0x86
	idVideo         ebmlID = 0xE0
	idPixelWidth    ebmlID = 0xB0
	idPixelHeight   ebmlID = 0xBA

	trackVideo = 1
	trackAudio = 2

	defaultTimecodeScale = 1_000_000
)

var webmCodecs = map[uint64]map[string]bool{
	trackVideo: {"V_VP8": true, "V_VP9": true, "V_AV1": true},
	trackAudio: {"A_OPUS": true, "A_VORBIS": true},
}

type ebmlElement struct {
	id   ebmlID
	body blob
}

type ebmlElements []ebmlElement

type webmTrack struct {
	kind  uint64
	codec string
	w     int
	h     int
}

func normalizeWebM(data blob, lim overlays.AssetLimit) (parsed, error) {
	header, segment, err := webmSections(data)
	if err != nil {
		return parsed{}, err
	}
	if err := checkDocType(header); err != nil {
		return parsed{}, err
	}
	seg, err := segment.ebmlChildren(idCluster)
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

func webmSections(data blob) (blob, blob, error) {
	top, err := data.ebmlChildren(0)
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

func checkDocType(header blob) error {
	fields, err := header.ebmlChildren(0)
	if err != nil {
		return err
	}
	docType, _ := fields.find(idDocType)
	if string(docType) != "webm" {
		return fail(CodeUnsupported)
	}
	return nil
}

func webmDuration(seg ebmlElements, lim overlays.AssetLimit) (int, error) {
	body, ok := seg.find(idInfo)
	if !ok {
		return 0, fail(CodeCorrupt)
	}
	info, err := body.ebmlChildren(0)
	if err != nil {
		return 0, err
	}
	scale := uint64(defaultTimecodeScale)
	if raw, ok := info.find(idTimecodeScale); ok {
		scale = raw.ebmlUint()
	}
	raw, ok := info.find(idDuration)
	if !ok {
		return 0, fail(CodeCorrupt)
	}
	ticks, err := raw.ebmlFloat()
	if err != nil {
		return 0, err
	}
	return millis(ticks * float64(scale) / 1e6).check(lim)
}

func webmVideo(seg ebmlElements, lim overlays.AssetLimit) (int, int, error) {
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
		return dims{w: extent(t.w), h: extent(t.h)}.check(lim)
	}
	return nil
}

func webmTracks(seg ebmlElements) ([]webmTrack, error) {
	body, ok := seg.find(idTracks)
	if !ok {
		return nil, fail(CodeCorrupt)
	}
	entries, err := body.ebmlChildren(0)
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

func webmTrackOf(body blob) (webmTrack, error) {
	fields, err := body.ebmlChildren(0)
	if err != nil {
		return webmTrack{}, err
	}
	kind, _ := fields.find(idTrackType)
	codec, _ := fields.find(idCodecID)
	track := webmTrack{kind: kind.ebmlUint(), codec: string(codec)}
	video, ok := fields.find(idVideo)
	if !ok {
		return track, nil
	}
	pixels, err := video.ebmlChildren(0)
	if err != nil {
		return webmTrack{}, err
	}
	w, _ := pixels.find(idPixelWidth)
	h, _ := pixels.find(idPixelHeight)
	track.w, track.h = int(w.ebmlUint()), int(h.ebmlUint())
	return track, nil
}

func (d blob) ebmlChildren(stop ebmlID) (ebmlElements, error) {
	var out ebmlElements
	for len(d) > 0 {
		if id, _, err := d.readID(); err == nil && id == stop {
			return append(out, ebmlElement{id: id}), nil
		}
		elem, rest, err := d.ebmlNext()
		if err != nil {
			return nil, err
		}
		out = append(out, elem)
		d = rest
	}
	return out, nil
}

func (d blob) ebmlNext() (ebmlElement, blob, error) {
	id, idLen, err := d.readID()
	if err != nil {
		return ebmlElement{}, nil, err
	}
	size, sizeLen, err := d[idLen:].readSize()
	if err != nil {
		return ebmlElement{}, nil, err
	}
	rest := d[idLen+sizeLen:]
	if size > uint64(len(rest)) {
		return ebmlElement{}, nil, fail(CodeCorrupt)
	}
	return ebmlElement{id: id, body: rest[:size]}, rest[size:], nil
}

func (d blob) readID() (ebmlID, int, error) {
	if len(d) == 0 || d[0] == 0 {
		return 0, 0, fail(CodeCorrupt)
	}
	n := bits.LeadingZeros8(d[0]) + 1
	if n > 4 || len(d) < n {
		return 0, 0, fail(CodeCorrupt)
	}
	var id ebmlID
	for _, b := range d[:n] {
		id = id<<8 | ebmlID(b)
	}
	return id, n, nil
}

func (d blob) readSize() (uint64, int, error) {
	if len(d) == 0 || d[0] == 0 {
		return 0, 0, fail(CodeCorrupt)
	}
	n := bits.LeadingZeros8(d[0]) + 1
	if len(d) < n {
		return 0, 0, fail(CodeCorrupt)
	}
	marker := uint64(0xFF) >> n
	value := uint64(d[0]) & marker
	unknown := value == marker
	for _, b := range d[1:n] {
		value = value<<8 | uint64(b)
		unknown = unknown && b == 0xFF
	}
	if unknown {
		return uint64(len(d) - n), n, nil
	}
	return value, n, nil
}

func (e ebmlElements) find(id ebmlID) (blob, bool) {
	for _, elem := range e {
		if elem.id == id {
			return elem.body, true
		}
	}
	return nil, false
}

func (d blob) ebmlUint() uint64 {
	var value uint64
	for _, b := range d[:min(len(d), 8)] {
		value = value<<8 | uint64(b)
	}
	return value
}

func (d blob) ebmlFloat() (float64, error) {
	switch len(d) {
	case 4:
		return float64(math.Float32frombits(binary.BigEndian.Uint32(d))), nil
	case 8:
		return math.Float64frombits(binary.BigEndian.Uint64(d)), nil
	}
	return 0, fail(CodeCorrupt)
}
