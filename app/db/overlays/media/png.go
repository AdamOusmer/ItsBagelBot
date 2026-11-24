// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package media

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image/png"

	"ItsBagelBot/internal/domain/overlays"
)

const (
	pngSignature     = "\x89PNG\r\n\x1a\n"
	pngChunkOverhead = 12
)

var pngKeep = map[string]bool{
	"IHDR": true, "PLTE": true, "IDAT": true, "IEND": true, "tRNS": true,
	"acTL": true, "fcTL": true, "fdAT": true,
	"gAMA": true, "cHRM": true, "sRGB": true, "iCCP": true,
}

var pngCodec = imageCodec{config: png.DecodeConfig, decode: png.Decode}

func normalizePNG(data blob, lim overlays.AssetLimit) (parsed, error) {
	out, err := rewritePNG(data)
	if err != nil {
		return parsed{}, err
	}
	return verifyImage(out, lim, pngCodec)
}

func rewritePNG(data blob) (blob, error) {
	if !bytes.HasPrefix(data, []byte(pngSignature)) {
		return nil, fail(CodeCorrupt)
	}
	out := append(make(blob, 0, len(data)), pngSignature...)
	for pos := offset(len(pngSignature)); pos < data.end(); {
		kind, end, err := data.pngChunk(pos)
		if err != nil {
			return nil, err
		}
		if pngKeep[kind] {
			out = append(out, data[pos:end]...)
		}
		if kind == "IEND" {
			return out, nil
		}
		pos = end
	}
	return nil, fail(CodeCorrupt)
}

func (d blob) pngChunk(pos offset) (string, offset, error) {
	if pos+pngChunkOverhead > d.end() {
		return "", 0, fail(CodeCorrupt)
	}
	end := pos + pngChunkOverhead + offset(binary.BigEndian.Uint32(d[pos:]))
	if end > d.end() {
		return "", 0, fail(CodeCorrupt)
	}
	if crc32.ChecksumIEEE(d[pos+4:end-4]) != binary.BigEndian.Uint32(d[end-4:]) {
		return "", 0, fail(CodeCorrupt)
	}
	return string(d[pos+4 : pos+8]), end, nil
}
