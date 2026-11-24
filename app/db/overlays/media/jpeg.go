// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package media

import (
	"bytes"
	"encoding/binary"
	"image/jpeg"

	"ItsBagelBot/internal/domain/overlays"
)

type marker byte

type jpegMark struct {
	id marker
	at offset
}

const (
	jpegSOS marker = 0xDA
	jpegEOI marker = 0xD9
	jpegTEM marker = 0x01
)

var jpegCodec = imageCodec{config: jpeg.DecodeConfig, decode: jpeg.Decode}

var jpegDropped = func() (dropped [256]bool) {
	for id := 0xE1; id <= 0xEF; id++ {
		dropped[id] = true
	}
	dropped[0xFE] = true
	return dropped
}()

func normalizeJPEG(data blob, lim overlays.AssetLimit) (parsed, error) {
	out, err := rewriteJPEG(data)
	if err != nil {
		return parsed{}, err
	}
	return verifyImage(out, lim, jpegCodec)
}

func rewriteJPEG(data blob) (blob, error) {
	if !bytes.HasPrefix(data, []byte{0xFF, 0xD8}) {
		return nil, fail(CodeCorrupt)
	}
	out := blob{0xFF, 0xD8}
	for pos := offset(2); pos < data.end(); {
		mark, err := data.jpegMarker(pos)
		if err != nil {
			return nil, err
		}
		if mark.id == jpegEOI {
			return append(out, 0xFF, byte(jpegEOI)), nil
		}
		kept, next, err := data.jpegSegment(mark)
		if err != nil {
			return nil, err
		}
		out = append(out, kept...)
		pos = next
	}
	return nil, fail(CodeCorrupt)
}

func (d blob) jpegMarker(pos offset) (jpegMark, error) {
	for bytes.HasPrefix(d[pos:], []byte{0xFF, 0xFF}) {
		pos++
	}
	if len(d[pos:]) < 2 || d[pos] != 0xFF {
		return jpegMark{}, fail(CodeCorrupt)
	}
	return jpegMark{id: marker(d[pos+1]), at: pos}, nil
}

func (d blob) jpegSegment(mark jpegMark) (blob, offset, error) {
	end, err := d.jpegSegmentEnd(mark)
	if err != nil {
		return nil, 0, err
	}
	if mark.id == jpegSOS {
		return d.jpegScan(mark, end)
	}
	if jpegDropped[mark.id] {
		return nil, end, nil
	}
	return d[mark.at:end], end, nil
}

func (d blob) jpegScan(mark jpegMark, headerEnd offset) (blob, offset, error) {
	scanEnd, err := d.jpegScanEnd(headerEnd)
	if err != nil {
		return nil, 0, err
	}
	return d[mark.at:scanEnd], scanEnd, nil
}

func (d blob) jpegSegmentEnd(mark jpegMark) (offset, error) {
	if mark.id == jpegTEM {
		return mark.at + 2, nil
	}
	if d.end() < mark.at+4 {
		return 0, fail(CodeCorrupt)
	}
	length := offset(binary.BigEndian.Uint16(d[mark.at+2:]))
	if length < 2 || mark.at+2+length > d.end() {
		return 0, fail(CodeCorrupt)
	}
	return mark.at + 2 + length, nil
}

func (d blob) jpegScanEnd(from offset) (offset, error) {
	for i := from; i+1 < d.end(); i++ {
		if d[i] == 0xFF && marker(d[i+1]).endsScan() {
			return i, nil
		}
	}
	return 0, fail(CodeCorrupt)
}

func (m marker) endsScan() bool {
	return m != 0x00 && (m < 0xD0 || m > 0xD7)
}
