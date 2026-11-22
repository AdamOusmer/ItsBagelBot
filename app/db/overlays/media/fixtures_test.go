// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package media_test

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math"
	"slices"
)

const pngIHDREnd = 33

func splice(src []byte, at int, insert ...[]byte) []byte {
	return bytes.Join([][]byte{src[:at], bytes.Join(insert, nil), src[at:]}, nil)
}

func cut(src []byte, n int) []byte {
	return src[:len(src)-n]
}

func le32(n int) []byte {
	return binary.LittleEndian.AppendUint32(nil, uint32(n))
}

func be32(n int) []byte {
	return binary.BigEndian.AppendUint32(nil, uint32(n))
}

func pngChunk(kind string, body []byte) []byte {
	out := append(be32(len(body)), kind...)
	out = append(out, body...)
	return binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(out[4:]))
}

func pngFixture(w, h int) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, w, h))); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func pngHeaderOnly(w, h int) []byte {
	header := append(be32(w), be32(h)...)
	header = append(header, 8, 0, 0, 0, 0)
	return bytes.Join([][]byte{[]byte("\x89PNG\r\n\x1a\n"), pngChunk("IHDR", header), pngChunk("IEND", nil)}, nil)
}

func pngWithMetadata() []byte {
	meta := [][]byte{
		pngChunk("tEXt", []byte("Comment\x00GPS 51.5,-0.1")),
		pngChunk("iTXt", []byte("XML:com.adobe.xmp\x00\x00\x00\x00\x00GPS")),
		pngChunk("eXIf", []byte("GPS")),
		pngChunk("tIME", make([]byte, 7)),
	}
	return append(splice(pngFixture(4, 3), pngIHDREnd, meta...), "<script>alert(1)</script>"...)
}

func jpegSegment(marker byte, payload []byte) []byte {
	out := binary.BigEndian.AppendUint16([]byte{0xFF, marker}, uint16(len(payload)+2))
	return append(out, payload...)
}

func jpegFixture() []byte {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 16, 16)), nil); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func jpegWithMetadata() []byte {
	exif := jpegSegment(0xE1, []byte("Exif\x00\x00GPSLatitude"))
	comment := jpegSegment(0xFE, []byte("secret comment"))
	return append(splice(jpegFixture(), 2, exif, comment), "<html>"...)
}

func jpegWithSize(w, h int) []byte {
	src := slices.Clone(jpegFixture())
	sof := bytes.Index(src, []byte{0xFF, 0xC0})
	binary.BigEndian.PutUint16(src[sof+5:], uint16(h))
	binary.BigEndian.PutUint16(src[sof+7:], uint16(w))
	return src
}

func gifFixture(frames int, screen image.Point) []byte {
	palette := color.Palette{color.Black, color.White}
	out := gif.GIF{Config: image.Config{ColorModel: palette, Width: screen.X, Height: screen.Y}}
	for range frames {
		out.Image = append(out.Image, image.NewPaletted(image.Rect(0, 0, 2, 2), palette))
		out.Delay = append(out.Delay, 10)
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, &out); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func riffChunkBytes(id string, body []byte) []byte {
	out := append(append([]byte(id), le32(len(body))...), body...)
	if len(body)%2 == 1 {
		out = append(out, 0)
	}
	return out
}

func riffFile(form string, chunks ...[]byte) []byte {
	payload := bytes.Join(chunks, nil)
	out := append(append([]byte("RIFF"), le32(len(payload)+4)...), form...)
	return append(out, payload...)
}

func webpFixture(w, h int) []byte {
	canvas := make([]byte, 10)
	canvas[0] = 0x1C
	copy(canvas[4:], le32(w - 1)[:3])
	copy(canvas[7:], le32(h - 1)[:3])
	lossless := append([]byte{0x2F}, le32((w-1)|(h-1)<<14)...)
	return riffFile("WEBP",
		riffChunkBytes("VP8X", canvas),
		riffChunkBytes("VP8L", append(lossless, 0, 0, 0, 0)),
		riffChunkBytes("EXIF", []byte("GPS")),
		riffChunkBytes("XMP ", []byte("XMP-DATA")),
	)
}

func wavFixture(dataBytes int, formatTag uint16) []byte {
	format := binary.LittleEndian.AppendUint16(nil, formatTag)
	format = binary.LittleEndian.AppendUint16(format, 1)
	format = append(format, le32(8000)...)
	format = append(format, le32(16000)...)
	format = append(format, 2, 0, 16, 0)
	return riffFile("WAVE",
		riffChunkBytes("fmt ", format),
		riffChunkBytes("LIST", []byte("INFOIART\x04\x00\x00\x00name")),
		riffChunkBytes("data", make([]byte, dataBytes)),
	)
}

func oggPage(flags byte, granule int64, payload []byte) []byte {
	out := binary.LittleEndian.AppendUint64([]byte{'O', 'g', 'g', 'S', 0, flags}, uint64(granule))
	out = append(out, le32(1)...)
	out = append(out, le32(0)...)
	out = append(out, le32(0)...)
	return append(append(out, 1, byte(len(payload))), payload...)
}

func opusHead() []byte {
	head := append([]byte("OpusHead"), 1, 1)
	head = binary.LittleEndian.AppendUint16(head, 312)
	head = append(head, le32(48000)...)
	return append(head, 0, 0, 0)
}

func vorbisHead() []byte {
	head := append([]byte("\x01vorbis"), 0, 0, 0, 0, 1)
	return append(head, le32(44100)...)
}

func oggFixture(head []byte, granule int64, lastFlags byte) []byte {
	return bytes.Join([][]byte{
		oggPage(0x02, 0, head),
		oggPage(0x00, granule/2, []byte("packet-data")),
		oggPage(lastFlags, granule, []byte("packet-data")),
	}, nil)
}

func ebml(id []byte, body ...[]byte) []byte {
	payload := bytes.Join(body, nil)
	out := append(slices.Clone(id), 0x10|byte(len(payload)>>24), byte(len(payload)>>16), byte(len(payload)>>8), byte(len(payload)))
	return append(out, payload...)
}

type webmSpec struct {
	docType    string
	codec      string
	w          int
	h          int
	durationMS float64
}

func webmVariant(edit func(*webmSpec)) []byte {
	spec := webmSpec{docType: "webm", codec: "V_VP9", w: 640, h: 360, durationMS: 5000}
	edit(&spec)
	video := ebml([]byte{0xAE},
		ebml([]byte{0x83}, []byte{1}),
		ebml([]byte{0x86}, []byte(spec.codec)),
		ebml([]byte{0xE0}, ebml([]byte{0xB0}, be32(spec.w)), ebml([]byte{0xBA}, be32(spec.h))),
	)
	audio := ebml([]byte{0xAE}, ebml([]byte{0x83}, []byte{2}), ebml([]byte{0x86}, []byte("A_OPUS")))
	duration := binary.BigEndian.AppendUint64(nil, math.Float64bits(spec.durationMS))
	return bytes.Join([][]byte{
		ebml([]byte{0x1A, 0x45, 0xDF, 0xA3}, ebml([]byte{0x42, 0x82}, []byte(spec.docType))),
		ebml([]byte{0x18, 0x53, 0x80, 0x67},
			ebml([]byte{0x15, 0x49, 0xA9, 0x66}, ebml([]byte{0x44, 0x89}, duration)),
			ebml([]byte{0x16, 0x54, 0xAE, 0x6B}, video, audio),
			ebml([]byte{0x1F, 0x43, 0xB6, 0x75}, ebml([]byte{0xE7}, []byte{0})),
		),
	}, nil)
}

func mp3Frames(n int) []byte {
	frame := append([]byte{0xFF, 0xFB, 0x90, 0x00}, make([]byte, 413)...)
	return bytes.Repeat(frame, n)
}

func mp3WithTags() []byte {
	tag := append([]byte("ID3\x03\x00\x00\x00\x00\x00\x14"), []byte("APIC-picture-payload")...)
	return append(append(tag, mp3Frames(3)...), "<html>"...)
}

func woff2Fixture(total int) []byte {
	out := append([]byte("wOF2"), 0, 1, 0, 0)
	out = append(out, be32(total)...)
	out = append(out, 0, 1)
	return append(out, make([]byte, total-len(out))...)
}
