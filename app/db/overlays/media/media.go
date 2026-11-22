// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package media turns untrusted upload bytes into a safe stored asset. The
// type is decided by magic bytes only; filenames and client content types are
// never inputs.
package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"

	"ItsBagelBot/internal/domain/overlays"
)

type Result struct {
	Type       overlays.AssetType
	Bytes      int64
	W          int
	H          int
	DurationMS int
	SHA256     string
}

type parsed struct {
	data       blob
	w          int
	h          int
	durationMS int
}

type normalizer func(data blob, lim overlays.AssetLimit) (parsed, error)

var normalizers = map[overlays.AssetType]normalizer{
	overlays.AssetPNG:    normalizePNG,
	overlays.AssetJPEG:   normalizeJPEG,
	overlays.AssetWebP:   normalizeWebP,
	overlays.AssetGIF:    normalizeGIF,
	overlays.AssetWebM:   normalizeWebM,
	overlays.AssetMP3:    normalizeMP3,
	overlays.AssetOGG:    normalizeOGG,
	overlays.AssetWAV:    normalizeWAV,
	overlays.AssetWOFF2:  normalizeWOFF2,
	overlays.AssetWidget: normalizeWidget,
}

var contentTypes = map[overlays.AssetType]string{
	overlays.AssetPNG:    "image/png",
	overlays.AssetJPEG:   "image/jpeg",
	overlays.AssetWebP:   "image/webp",
	overlays.AssetGIF:    "image/gif",
	overlays.AssetWebM:   "video/webm",
	overlays.AssetMP3:    "audio/mpeg",
	overlays.AssetOGG:    "audio/ogg",
	overlays.AssetWAV:    "audio/wav",
	overlays.AssetWOFF2:  "font/woff2",
	overlays.AssetWidget: "application/json",
}

func ContentType(kind overlays.AssetType) string {
	return contentTypes[kind]
}

type plan struct {
	kind  overlays.AssetType
	limit overlays.AssetLimit
	run   normalizer
	head  blob
}

// Normalize reads at most the type's byte limit from in and returns the
// rewritten bytes. size is the declared length, or negative when unknown.
func Normalize(ctx context.Context, in io.Reader, size int64) (Result, []byte, error) {
	p, err := classify(in)
	if err != nil {
		return Result{}, nil, err
	}
	data, err := p.read(ctx, in, size)
	if err != nil {
		return Result{}, nil, err
	}
	out, err := p.run(data, p.limit)
	if err != nil {
		return Result{}, nil, err
	}
	if int64(len(out.data)) > p.limit.MaxBytes {
		return Result{}, nil, fail(CodeTooLarge)
	}
	return resultOf(p.kind, out), out.data, nil
}

func classify(in io.Reader) (plan, error) {
	head, err := readHead(in)
	if err != nil {
		return plan{}, err
	}
	kind, err := sniffHead(head)
	if err != nil {
		return plan{}, err
	}
	limit, ok := overlays.AssetLimitFor(kind)
	run := normalizers[kind]
	if !ok || run == nil {
		return plan{}, fail(CodeUnsupported)
	}
	return plan{kind: kind, limit: limit, run: run, head: head}, nil
}

func (p plan) read(ctx context.Context, in io.Reader, size int64) (blob, error) {
	if size > p.limit.MaxBytes {
		return nil, fail(CodeTooLarge)
	}
	rest, err := io.ReadAll(io.LimitReader(in, p.limit.MaxBytes+1-int64(len(p.head))))
	if err != nil {
		return nil, err
	}
	data := append(p.head, rest...)
	if int64(len(data)) > p.limit.MaxBytes {
		return nil, fail(CodeTooLarge)
	}
	if size >= 0 && int64(len(data)) != size {
		return nil, fail(CodeSizeMismatch)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return data, nil
}

func resultOf(kind overlays.AssetType, p parsed) Result {
	sum := sha256.Sum256(p.data)
	return Result{
		Type:       kind,
		Bytes:      int64(len(p.data)),
		W:          p.w,
		H:          p.h,
		DurationMS: p.durationMS,
		SHA256:     hex.EncodeToString(sum[:]),
	}
}
