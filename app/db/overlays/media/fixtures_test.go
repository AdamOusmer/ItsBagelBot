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

const pngIHDREnd position = 33

type (
	blob      []byte
	number    int
	position  int
	chunkKind string
	riffForm  string
	marker    byte
	formatTag uint16
)

type picture struct {
	w number
	h number
}

type streamEnd struct {
	granule int64
	flags   byte
}

type oggPageSpec struct {
	flags   byte
	granule int64
	payload blob
}

type webmSpec struct {
	docType    string
	codec      string
	size       picture
	durationMS float64
}

func join(parts ...blob) blob {
	var out blob
	for _, part := range parts {
		out = append(out, part...)
	}
	return out
}

func (b blob) splice(at position, insert ...blob) blob {
	return join(b[:at], join(insert...), b[at:])
}

func (b blob) dropLast(n number) blob {
	return b[:len(b)-int(n)]
}

func (n number) le32() blob {
	return binary.LittleEndian.AppendUint32(nil, uint32(n))
}

func (n number) be32() blob {
	return binary.BigEndian.AppendUint32(nil, uint32(n))
}

func pngChunk(kind chunkKind, body blob) blob {
	out := append(number(len(body)).be32(), string(kind)...)
	out = append(out, body...)
	return binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(out[4:]))
}

func pngFixture(p picture) blob {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, int(p.w), int(p.h)))); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func pngHeaderOnly(p picture) blob {
	header := append(p.w.be32(), p.h.be32()...)
	header = append(header, 8, 0, 0, 0, 0)
	return join(blob("\x89PNG\r\n\x1a\n"), pngChunk("IHDR", header), pngChunk("IEND", nil))
}

func pngWithMetadata() blob {
	meta := []blob{
		pngChunk("tEXt", blob("Comment\x00GPS 51.5,-0.1")),
		pngChunk("iTXt", blob("XML:com.adobe.xmp\x00\x00\x00\x00\x00GPS")),
		pngChunk("eXIf", blob("GPS")),
		pngChunk("tIME", make(blob, 7)),
	}
	return append(pngFixture(picture{4, 3}).splice(pngIHDREnd, meta...), "<script>alert(1)</script>"...)
}

func jpegSegment(id marker, payload blob) blob {
	out := binary.BigEndian.AppendUint16(blob{0xFF, byte(id)}, uint16(len(payload)+2))
	return append(out, payload...)
}

func jpegFixture() blob {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 16, 16)), nil); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func jpegWithMetadata() blob {
	exif := jpegSegment(0xE1, blob("Exif\x00\x00GPSLatitude"))
	comment := jpegSegment(0xFE, blob("secret comment"))
	return append(jpegFixture().splice(2, exif, comment), "<html>"...)
}

func jpegWithSize(p picture) blob {
	src := slices.Clone(jpegFixture())
	sof := bytes.Index(src, []byte{0xFF, 0xC0})
	binary.BigEndian.PutUint16(src[sof+5:], uint16(p.h))
	binary.BigEndian.PutUint16(src[sof+7:], uint16(p.w))
	return src
}

func gifFixture(frames number, screen image.Point) blob {
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

func riffChunkBytes(id chunkKind, body blob) blob {
	out := join(blob(id), number(len(body)).le32(), body)
	if len(body)%2 == 1 {
		out = append(out, 0)
	}
	return out
}

func riffFile(form riffForm, chunks ...blob) blob {
	payload := join(chunks...)
	return join(blob("RIFF"), number(len(payload)+4).le32(), blob(form), payload)
}

func webpFixture(p picture) blob {
	canvas := make(blob, 10)
	canvas[0] = 0x1C
	copy(canvas[4:], (p.w - 1).le32()[:3])
	copy(canvas[7:], (p.h - 1).le32()[:3])
	lossless := join(blob{0x2F}, ((p.w - 1) | ((p.h - 1) << 14)).le32(), make(blob, 4))
	return riffFile("WEBP",
		riffChunkBytes("VP8X", canvas),
		riffChunkBytes("VP8L", lossless),
		riffChunkBytes("EXIF", blob("GPS")),
		riffChunkBytes("XMP ", blob("XMP-DATA")),
	)
}

func wavFixture(dataBytes number, tag formatTag) blob {
	format := binary.LittleEndian.AppendUint16(nil, uint16(tag))
	format = binary.LittleEndian.AppendUint16(format, 1)
	format = join(format, number(8000).le32(), number(16000).le32(), blob{2, 0, 16, 0})
	return riffFile("WAVE",
		riffChunkBytes("fmt ", format),
		riffChunkBytes("LIST", blob("INFOIART\x04\x00\x00\x00name")),
		riffChunkBytes("data", make(blob, int(dataBytes))),
	)
}

func (s oggPageSpec) encode() blob {
	out := binary.LittleEndian.AppendUint64(blob{'O', 'g', 'g', 'S', 0, s.flags}, uint64(s.granule))
	out = join(out, number(1).le32(), number(0).le32(), number(0).le32())
	return join(out, blob{1, byte(len(s.payload))}, s.payload)
}

func opusHead() blob {
	head := join(blob("OpusHead"), blob{1, 1})
	head = binary.LittleEndian.AppendUint16(head, 312)
	return join(head, number(48000).le32(), blob{0, 0, 0})
}

func vorbisHead() blob {
	return join(blob("\x01vorbis"), blob{0, 0, 0, 0, 1}, number(44100).le32())
}

func oggFixture(head blob, end streamEnd) blob {
	return join(
		oggPageSpec{flags: 0x02, payload: head}.encode(),
		oggPageSpec{granule: end.granule / 2, payload: blob("packet-data")}.encode(),
		oggPageSpec{flags: end.flags, granule: end.granule, payload: blob("packet-data")}.encode(),
	)
}

func ebml(id blob, body ...blob) blob {
	payload := join(body...)
	size := number(len(payload)).be32()
	size[0] |= 0x10
	return join(id, size, payload)
}

func webmVariant(edit func(*webmSpec)) blob {
	spec := webmSpec{docType: "webm", codec: "V_VP9", size: picture{640, 360}, durationMS: 5000}
	edit(&spec)
	video := ebml(blob{0xAE},
		ebml(blob{0x83}, blob{1}),
		ebml(blob{0x86}, blob(spec.codec)),
		ebml(blob{0xE0}, ebml(blob{0xB0}, spec.size.w.be32()), ebml(blob{0xBA}, spec.size.h.be32())),
	)
	audio := ebml(blob{0xAE}, ebml(blob{0x83}, blob{2}), ebml(blob{0x86}, blob("A_OPUS")))
	duration := binary.BigEndian.AppendUint64(nil, math.Float64bits(spec.durationMS))
	return join(
		ebml(blob{0x1A, 0x45, 0xDF, 0xA3}, ebml(blob{0x42, 0x82}, blob(spec.docType))),
		ebml(blob{0x18, 0x53, 0x80, 0x67},
			ebml(blob{0x15, 0x49, 0xA9, 0x66}, ebml(blob{0x44, 0x89}, duration)),
			ebml(blob{0x16, 0x54, 0xAE, 0x6B}, video, audio),
			ebml(blob{0x1F, 0x43, 0xB6, 0x75}, ebml(blob{0xE7}, blob{0})),
		),
	)
}

func mp3Frames(n number) blob {
	frame := join(blob{0xFF, 0xFB, 0x90, 0x00}, make(blob, 413))
	return bytes.Repeat(frame, int(n))
}

func mp3WithTags() blob {
	tag := join(blob("ID3\x03\x00\x00\x00\x00\x00\x14"), blob("APIC-picture-payload"))
	return join(tag, mp3Frames(3), blob("<html>"))
}

func woff2Fixture(total number) blob {
	out := join(blob("wOF2"), blob{0, 1, 0, 0}, total.be32(), blob{0, 1})
	return join(out, make(blob, int(total)-len(out)))
}
