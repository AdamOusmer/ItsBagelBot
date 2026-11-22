// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package media

import (
	"bytes"
	"io"

	"ItsBagelBot/internal/domain/overlays"
)

const sniffBytes = 512

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

var magics = []struct {
	prefix string
	kind   overlays.AssetType
}{
	{"\x89PNG\r\n\x1a\n", overlays.AssetPNG},
	{"\xff\xd8\xff", overlays.AssetJPEG},
	{"GIF87a", overlays.AssetGIF},
	{"GIF89a", overlays.AssetGIF},
	{"\x1a\x45\xdf\xa3", overlays.AssetWebM},
	{"OggS", overlays.AssetOGG},
	{"wOF2", overlays.AssetWOFF2},
	{"ID3", overlays.AssetMP3},
}

// Sniff decides the asset type from the first bytes alone. SVG, MP4, HTML and
// every unknown format are errors.
func Sniff(r io.Reader) (overlays.AssetType, error) {
	head, err := readHead(r)
	if err != nil {
		return "", err
	}
	return sniffHead(head)
}

func readHead(r io.Reader) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, sniffBytes))
}

func sniffHead(head blob) (overlays.AssetType, error) {
	if kind, ok := matchBinary(head); ok {
		return kind, nil
	}
	if len(head) >= 8 && string(head[4:8]) == "ftyp" {
		return "", fail(CodeMP4)
	}
	return sniffText(head)
}

func matchBinary(head blob) (overlays.AssetType, bool) {
	for _, m := range magics {
		if bytes.HasPrefix(head, []byte(m.prefix)) {
			return m.kind, true
		}
	}
	if kind, ok := matchRIFF(head); ok {
		return kind, true
	}
	if _, ok := head.mp3Header(); ok {
		return overlays.AssetMP3, true
	}
	return "", false
}

func matchRIFF(head blob) (overlays.AssetType, bool) {
	if len(head) < 12 || string(head[:4]) != "RIFF" {
		return "", false
	}
	switch string(head[8:12]) {
	case "WEBP":
		return overlays.AssetWebP, true
	case "WAVE":
		return overlays.AssetWAV, true
	}
	return "", false
}

func sniffText(head blob) (overlays.AssetType, error) {
	text := bytes.TrimLeft(bytes.TrimPrefix(head, utf8BOM), " \t\r\n")
	if len(text) == 0 {
		return "", fail(CodeCorrupt)
	}
	switch text[0] {
	case '{':
		return overlays.AssetWidget, nil
	case '<':
		return "", fail(markupCode(text))
	}
	return "", fail(CodeUnsupported)
}

func markupCode(text blob) overlays.Code {
	lower := bytes.ToLower(text)
	if bytes.Contains(lower, []byte("<svg")) || bytes.HasPrefix(lower, []byte("<?xml")) {
		return CodeSVG
	}
	return CodeHTML
}
