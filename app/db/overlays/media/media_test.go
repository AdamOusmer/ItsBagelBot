// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package media_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"testing"

	"ItsBagelBot/app/db/overlays/media"
	"ItsBagelBot/internal/domain/overlays"
)

func TestSniff(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want overlays.AssetType
		code overlays.Code
	}{
		{"png", pngFixture(1, 1), overlays.AssetPNG, ""},
		{"jpeg", jpegFixture(), overlays.AssetJPEG, ""},
		{"gif", gifFixture(1, image.Pt(2, 2)), overlays.AssetGIF, ""},
		{"webp", webpFixture(4, 4), overlays.AssetWebP, ""},
		{"wav", wavFixture(100, 1), overlays.AssetWAV, ""},
		{"webm", webmVariant(func(*webmSpec) {}), overlays.AssetWebM, ""},
		{"ogg", oggFixture(opusHead(), 48312, 4), overlays.AssetOGG, ""},
		{"mp3 frames", mp3Frames(2), overlays.AssetMP3, ""},
		{"mp3 id3", mp3WithTags(), overlays.AssetMP3, ""},
		{"woff2", woff2Fixture(64), overlays.AssetWOFF2, ""},
		{"widget", []byte("  \n{\"html\":\"\"}"), overlays.AssetWidget, ""},
		{"svg", []byte("<svg xmlns='http://www.w3.org/2000/svg'/>"), "", media.CodeSVG},
		{"svg with prolog", []byte("\n<?xml version='1.0'?><svg/>"), "", media.CodeSVG},
		{"html", []byte("<!DOCTYPE html><script>1</script>"), "", media.CodeHTML},
		{"mp4", []byte("\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00"), "", media.CodeMP4},
		{"aac adts", []byte{0xFF, 0xF1, 0x50, 0x80, 0x00, 0x1F, 0xFC}, "", media.CodeUnsupported},
		{"executable", []byte("MZ\x90\x00\x03"), "", media.CodeUnsupported},
		{"empty", nil, "", media.CodeCorrupt},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := media.Sniff(bytes.NewReader(tc.in))
			if got != tc.want || media.CodeOf(err) != tc.code {
				t.Fatalf("Sniff = %q, %v; want %q, %q", got, err, tc.want, tc.code)
			}
		})
	}
}

type acceptCase struct {
	name   string
	in     []byte
	kind   overlays.AssetType
	w, h   int
	ms     int
	absent []string
	exact  []byte
	valid  func([]byte) error
}

func decodeImage(decode func(io.Reader) (image.Image, error)) func([]byte) error {
	return func(data []byte) error {
		_, err := decode(bytes.NewReader(data))
		return err
	}
}

func gifThreeFrames(data []byte) error {
	img, err := gif.DecodeAll(bytes.NewReader(data))
	if err == nil && len(img.Image) != 3 {
		return errors.New("frame count changed")
	}
	return err
}

func webpMetadataFlagsCleared(data []byte) error {
	if data[20]&0x0C != 0 {
		return errors.New("metadata flags still set")
	}
	return nil
}

func TestNormalizeAccepts(t *testing.T) {
	webm := webmVariant(func(*webmSpec) {})
	opus := oggFixture(opusHead(), 96312, 4)
	woff2 := woff2Fixture(256)
	widget := []byte("{ \"html\" : \"hi\",\n \"css\":\"a{}\", \"js\":\"\", \"fields\": [ ] }")
	cases := []acceptCase{
		{name: "png", in: pngFixture(4, 3), kind: overlays.AssetPNG, w: 4, h: 3, valid: decodeImage(png.Decode)},
		{name: "png metadata and trailer", in: pngWithMetadata(), kind: overlays.AssetPNG, w: 4, h: 3,
			absent: []string{"tEXt", "iTXt", "eXIf", "tIME", "GPS", "<script>"}, valid: decodeImage(png.Decode)},
		{name: "jpeg exif gps and comment", in: jpegWithMetadata(), kind: overlays.AssetJPEG, w: 16, h: 16,
			absent: []string{"Exif", "GPS", "secret comment", "<html>"}, valid: decodeImage(jpeg.Decode)},
		{name: "gif trailer", in: append(gifFixture(3, image.Pt(2, 2)), "<html>"...), kind: overlays.AssetGIF,
			w: 2, h: 2, ms: 300, absent: []string{"<html>"}, valid: gifThreeFrames},
		{name: "webp metadata", in: webpFixture(32, 16), kind: overlays.AssetWebP, w: 32, h: 16,
			absent: []string{"EXIF", "XMP ", "GPS", "XMP-DATA"}, valid: webpMetadataFlagsCleared},
		{name: "webm", in: webm, kind: overlays.AssetWebM, w: 640, h: 360, ms: 5000, exact: webm},
		{name: "wav list stripped", in: wavFixture(16000, 1), kind: overlays.AssetWAV, ms: 1000,
			absent: []string{"LIST", "IART"}},
		{name: "ogg opus", in: opus, kind: overlays.AssetOGG, ms: 2000, exact: opus},
		{name: "ogg vorbis", in: oggFixture(vorbisHead(), 44100, 4), kind: overlays.AssetOGG, ms: 1000},
		{name: "mp3 tags and trailer", in: mp3WithTags(), kind: overlays.AssetMP3, ms: 79,
			exact: mp3Frames(3)},
		{name: "woff2", in: woff2, kind: overlays.AssetWOFF2, exact: woff2},
		{name: "widget", in: widget, kind: overlays.AssetWidget,
			exact: []byte("{\"html\":\"hi\",\"css\":\"a{}\",\"js\":\"\",\"fields\":[]}")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, out, err := media.Normalize(context.Background(), bytes.NewReader(tc.in), int64(len(tc.in)))
			if err != nil {
				t.Fatalf("Normalize: %v", err)
			}
			sum := sha256.Sum256(out)
			want := media.Result{Type: tc.kind, Bytes: int64(len(out)), W: tc.w, H: tc.h, DurationMS: tc.ms, SHA256: hex.EncodeToString(sum[:])}
			if res != want {
				t.Fatalf("result = %+v, want %+v", res, want)
			}
			assertOutput(t, tc, out)
		})
	}
}

func assertOutput(t *testing.T, tc acceptCase, out []byte) {
	t.Helper()
	for _, banned := range tc.absent {
		if bytes.Contains(out, []byte(banned)) {
			t.Errorf("output still contains %q", banned)
		}
	}
	if tc.exact != nil && !bytes.Equal(out, tc.exact) {
		t.Errorf("output differs from expected bytes")
	}
	if tc.valid != nil {
		if err := tc.valid(out); err != nil {
			t.Errorf("output invalid: %v", err)
		}
	}
}

func TestNormalizeRejects(t *testing.T) {
	validPNG := pngFixture(4, 3)
	corruptCRC := append([]byte(nil), validPNG...)
	corruptCRC[len(corruptCRC)-20] ^= 0xFF
	cases := []struct {
		name string
		in   []byte
		size int64
		code overlays.Code
	}{
		{"png oversized dimensions", pngHeaderOnly(5000, 5000), -1, media.CodeDimensions},
		{"png truncated", cut(validPNG, 5), -1, media.CodeCorrupt},
		{"png bad crc", corruptCRC, -1, media.CodeCorrupt},
		{"jpeg oversized dimensions", jpegWithSize(5000, 5000), -1, media.CodeDimensions},
		{"jpeg missing end marker", cut(jpegFixture(), 2), -1, media.CodeCorrupt},
		{"jpeg truncated", cut(jpegFixture(), 40), -1, media.CodeCorrupt},
		{"gif oversized screen", gifFixture(1, image.Pt(2000, 2000)), -1, media.CodeDimensions},
		{"gif too many frames", gifFixture(601, image.Pt(2, 2)), -1, media.CodeFrames},
		{"gif truncated", cut(gifFixture(2, image.Pt(2, 2)), 3), -1, media.CodeCorrupt},
		{"webp oversized canvas", webpFixture(5000, 5000), -1, media.CodeDimensions},
		{"webp truncated", cut(webpFixture(8, 8), 6), -1, media.CodeCorrupt},
		{"webm matroska", webmVariant(func(s *webmSpec) { s.docType = "matroska" }), -1, media.CodeUnsupported},
		{"webm h264", webmVariant(func(s *webmSpec) { s.codec = "V_MPEG4/ISO/AVC" }), -1, media.CodeCodec},
		{"webm too long", webmVariant(func(s *webmSpec) { s.durationMS = 121000 }), -1, media.CodeDuration},
		{"webm too wide", webmVariant(func(s *webmSpec) { s.w = 4000 }), -1, media.CodeDimensions},
		{"webm truncated", cut(webmVariant(func(*webmSpec) {}), 10), -1, media.CodeCorrupt},
		{"webm trailing html", append(webmVariant(func(*webmSpec) {}), "<html>"...), -1, media.CodeCorrupt},
		{"wav too long", wavFixture(496000, 1), -1, media.CodeDuration},
		{"wav truncated", cut(wavFixture(16000, 1), 10), -1, media.CodeCorrupt},
		{"wav adpcm", wavFixture(1600, 2), -1, media.CodeCodec},
		{"ogg truncated", cut(oggFixture(opusHead(), 48312, 4), 3), -1, media.CodeCorrupt},
		{"ogg unknown codec", oggFixture([]byte("\x80theora-ident"), 1000, 4), -1, media.CodeCodec},
		{"ogg too long", oggFixture(opusHead(), 48000*31+312, 4), -1, media.CodeDuration},
		{"ogg missing end of stream", oggFixture(opusHead(), 48312, 0), -1, media.CodeCorrupt},
		{"mp3 tag without frames", []byte("ID3\x03\x00\x00\x00\x00\x00\x04abcdjunk"), -1, media.CodeCorrupt},
		{"woff2 truncated", cut(woff2Fixture(256), 1), -1, media.CodeCorrupt},
		{"woff2 declared too large", woff2Fixture(256), 2 << 20, media.CodeTooLarge},
		{"woff2 undeclared overflow", woff2Fixture(2 << 20), -1, media.CodeTooLarge},
		{"widget unknown key", []byte("{\"html\":\"\",\"extra\":1}"), -1, media.CodeCorrupt},
		{"widget trailing data", []byte("{\"html\":\"\"}{}"), -1, media.CodeCorrupt},
		{"widget invalid utf8", []byte("{\"html\":\"\xff\"}"), -1, media.CodeCorrupt},
		{"svg", []byte("<svg/>"), -1, media.CodeSVG},
		{"mp4", []byte("\x00\x00\x00\x18ftypisom\x00\x00\x00\x00"), -1, media.CodeMP4},
		{"html", []byte("<html></html>"), -1, media.CodeHTML},
		{"empty", nil, -1, media.CodeCorrupt},
		{"declared size smaller", validPNG, int64(len(validPNG)) - 1, media.CodeSizeMismatch},
		{"declared size larger", validPNG, int64(len(validPNG)) + 1, media.CodeSizeMismatch},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, out, err := media.Normalize(context.Background(), bytes.NewReader(tc.in), tc.size)
			if media.CodeOf(err) != tc.code || out != nil {
				t.Fatalf("Normalize = %v (%d bytes); want code %q", err, len(out), tc.code)
			}
		})
	}
}

func TestNormalizeCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := media.Normalize(ctx, bytes.NewReader(pngFixture(2, 2)), -1)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestContentTypeCoversEveryAssetType(t *testing.T) {
	for _, kind := range overlays.AssetTypes() {
		if media.ContentType(kind) == "" {
			t.Errorf("no content type for %q", kind)
		}
	}
}
