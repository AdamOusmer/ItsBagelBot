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

func normalizePNG(data []byte, lim overlays.AssetLimit) (parsed, error) {
	out, err := rewritePNG(data)
	if err != nil {
		return parsed{}, err
	}
	return verifyImage(out, lim, pngCodec)
}

func rewritePNG(data []byte) ([]byte, error) {
	if !bytes.HasPrefix(data, []byte(pngSignature)) {
		return nil, fail(CodeCorrupt)
	}
	out := append(make([]byte, 0, len(data)), pngSignature...)
	for pos := len(pngSignature); pos < len(data); {
		kind, end, err := pngChunk(data, pos)
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

func pngChunk(data []byte, pos int) (string, int, error) {
	if pos+pngChunkOverhead > len(data) {
		return "", 0, fail(CodeCorrupt)
	}
	end := pos + pngChunkOverhead + int(binary.BigEndian.Uint32(data[pos:]))
	if end > len(data) {
		return "", 0, fail(CodeCorrupt)
	}
	if crc32.ChecksumIEEE(data[pos+4:end-4]) != binary.BigEndian.Uint32(data[end-4:]) {
		return "", 0, fail(CodeCorrupt)
	}
	return string(data[pos+4 : pos+8]), end, nil
}
