// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package media

import (
	"bytes"
	"image"
	"io"

	"ItsBagelBot/internal/domain/overlays"
)

type imageCodec struct {
	config func(io.Reader) (image.Config, error)
	decode func(io.Reader) (image.Image, error)
}

func verifyImage(data []byte, lim overlays.AssetLimit, codec imageCodec) (parsed, error) {
	cfg, err := codec.config(bytes.NewReader(data))
	if err != nil {
		return parsed{}, fail(CodeCorrupt)
	}
	if err := checkFrame(lim, cfg.Width, cfg.Height); err != nil {
		return parsed{}, err
	}
	if _, err := codec.decode(bytes.NewReader(data)); err != nil {
		return parsed{}, fail(CodeCorrupt)
	}
	return parsed{data: data, w: cfg.Width, h: cfg.Height}, nil
}
