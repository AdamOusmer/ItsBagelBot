// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package media

import (
	"bytes"
	"encoding/binary"
	"image/jpeg"

	"ItsBagelBot/internal/domain/overlays"
)

const (
	jpegSOS = 0xDA
	jpegEOI = 0xD9
	jpegTEM = 0x01
)

var jpegCodec = imageCodec{config: jpeg.DecodeConfig, decode: jpeg.Decode}

var jpegDropped = func() (dropped [256]bool) {
	for marker := 0xE1; marker <= 0xEF; marker++ {
		dropped[marker] = true
	}
	dropped[0xFE] = true
	return dropped
}()

func normalizeJPEG(data []byte, lim overlays.AssetLimit) (parsed, error) {
	out, err := rewriteJPEG(data)
	if err != nil {
		return parsed{}, err
	}
	return verifyImage(out, lim, jpegCodec)
}

func rewriteJPEG(data []byte) ([]byte, error) {
	if !bytes.HasPrefix(data, []byte{0xFF, 0xD8}) {
		return nil, fail(CodeCorrupt)
	}
	out := []byte{0xFF, 0xD8}
	for pos := 2; pos < len(data); {
		marker, at, err := jpegMarker(data, pos)
		if err != nil {
			return nil, err
		}
		if marker == jpegEOI {
			return append(out, 0xFF, jpegEOI), nil
		}
		kept, next, err := jpegSegment(data, at, marker)
		if err != nil {
			return nil, err
		}
		out = append(out, kept...)
		pos = next
	}
	return nil, fail(CodeCorrupt)
}

func jpegMarker(data []byte, pos int) (byte, int, error) {
	for bytes.HasPrefix(data[pos:], []byte{0xFF, 0xFF}) {
		pos++
	}
	if len(data[pos:]) < 2 || data[pos] != 0xFF {
		return 0, 0, fail(CodeCorrupt)
	}
	return data[pos+1], pos, nil
}

func jpegSegment(data []byte, at int, marker byte) ([]byte, int, error) {
	end, err := jpegSegmentEnd(data, at, marker)
	if err != nil {
		return nil, 0, err
	}
	if marker == jpegSOS {
		return jpegScan(data, at, end)
	}
	if jpegDropped[marker] {
		return nil, end, nil
	}
	return data[at:end], end, nil
}

func jpegScan(data []byte, at, headerEnd int) ([]byte, int, error) {
	scanEnd, err := jpegScanEnd(data, headerEnd)
	if err != nil {
		return nil, 0, err
	}
	return data[at:scanEnd], scanEnd, nil
}

func jpegSegmentEnd(data []byte, at int, marker byte) (int, error) {
	if marker == jpegTEM {
		return at + 2, nil
	}
	if len(data) < at+4 {
		return 0, fail(CodeCorrupt)
	}
	length := int(binary.BigEndian.Uint16(data[at+2:]))
	if length < 2 || at+2+length > len(data) {
		return 0, fail(CodeCorrupt)
	}
	return at + 2 + length, nil
}

func jpegScanEnd(data []byte, from int) (int, error) {
	for i := from; i+1 < len(data); i++ {
		if data[i] == 0xFF && endsScan(data[i+1]) {
			return i, nil
		}
	}
	return 0, fail(CodeCorrupt)
}

func endsScan(next byte) bool {
	return next != 0x00 && (next < 0xD0 || next > 0xD7)
}
