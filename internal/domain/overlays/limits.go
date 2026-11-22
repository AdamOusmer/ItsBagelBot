// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package overlays

import "slices"

const (
	DocumentMaxBytes       = 256 << 10
	MaxWidgets             = 100
	OverlayMaxAssetBytes   = 250 << 20
	OverlayMaxAssets       = 200
	AccountMaxBytes        = 1 << 30
	AccountMaxOverlays     = 20
	AccountMaxAssets       = 500
	AccountUploadsPerHour  = 60
	ImportMaxUnpackedBytes = 500 << 20
	ImportMaxEntries       = 300
	ImportMaxRatio         = 50
	LapseRetentionDays     = 60
)

type AssetType string

const (
	AssetPNG    AssetType = "png"
	AssetWebP   AssetType = "webp"
	AssetJPEG   AssetType = "jpeg"
	AssetGIF    AssetType = "gif"
	AssetWebM   AssetType = "webm"
	AssetMP3    AssetType = "mp3"
	AssetOGG    AssetType = "ogg"
	AssetWAV    AssetType = "wav"
	AssetWOFF2  AssetType = "woff2"
	AssetWidget AssetType = "widget"
)

type AssetLimit struct {
	MaxBytes      int64
	MaxWidth      int
	MaxHeight     int
	MaxPixels     int64
	MaxDurationMS int
	MaxFrames     int
	MaxPerOverlay int
}

var (
	imageLimit = AssetLimit{MaxBytes: 10 << 20, MaxWidth: 4096, MaxHeight: 4096, MaxPixels: 1 << 24}
	audioLimit = AssetLimit{MaxBytes: 5 << 20, MaxDurationMS: 30_000}
)

var assetTypes = []AssetType{AssetPNG, AssetWebP, AssetJPEG, AssetGIF, AssetWebM, AssetMP3, AssetOGG, AssetWAV, AssetWOFF2, AssetWidget}

var assetLimits = map[AssetType]AssetLimit{
	AssetPNG:    imageLimit,
	AssetWebP:   imageLimit,
	AssetJPEG:   imageLimit,
	AssetGIF:    {MaxBytes: 10 << 20, MaxWidth: 1920, MaxHeight: 1080, MaxFrames: 600},
	AssetWebM:   {MaxBytes: 50 << 20, MaxWidth: 1920, MaxHeight: 1920, MaxDurationMS: 120_000},
	AssetMP3:    audioLimit,
	AssetOGG:    audioLimit,
	AssetWAV:    audioLimit,
	AssetWOFF2:  {MaxBytes: 1 << 20, MaxPerOverlay: 4},
	AssetWidget: {MaxBytes: 512 << 10},
}

func AssetTypes() []AssetType {
	return slices.Clone(assetTypes)
}

func AssetLimitFor(kind AssetType) (AssetLimit, bool) {
	limit, ok := assetLimits[kind]
	return limit, ok
}

func Allowed(kind Kind, premium bool) bool {
	return kind == KindSystem || premium
}

func (l AssetLimit) fitsFrame(w, h int) bool {
	if l.MaxWidth == 0 {
		return true
	}
	inFrame := within(w, 1, l.MaxWidth) && within(h, 1, l.MaxHeight)
	return inFrame && l.fitsPixels(w, h)
}

func (l AssetLimit) fitsPixels(w, h int) bool {
	return l.MaxPixels == 0 || int64(w)*int64(h) <= l.MaxPixels
}

func (l AssetLimit) fitsDuration(ms int) bool {
	return l.MaxDurationMS == 0 || ms <= l.MaxDurationMS
}
