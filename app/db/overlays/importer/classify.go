// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package importer

import (
	"path"
	"strings"

	"ItsBagelBot/internal/domain/overlays"
)

const bundleSuffix = ".bundle.json"

type fileRule struct {
	kind   overlays.AssetType
	reason Reason
}

var extRules = map[string]fileRule{
	".png":   {kind: overlays.AssetPNG},
	".webp":  {kind: overlays.AssetWebP},
	".jpg":   {kind: overlays.AssetJPEG},
	".jpeg":  {kind: overlays.AssetJPEG},
	".gif":   {kind: overlays.AssetGIF},
	".webm":  {kind: overlays.AssetWebM},
	".mp3":   {kind: overlays.AssetMP3},
	".ogg":   {kind: overlays.AssetOGG},
	".wav":   {kind: overlays.AssetWAV},
	".woff2": {kind: overlays.AssetWOFF2},
	".mp4":   {reason: ReasonConvertToWebM},
	".mov":   {reason: ReasonConvertToWebM},
	".m4v":   {reason: ReasonConvertToWebM},
	".mkv":   {reason: ReasonConvertToWebM},
	".avi":   {reason: ReasonConvertToWebM},
	".ttf":   {reason: ReasonConvertToWOFF2},
	".otf":   {reason: ReasonConvertToWOFF2},
	".woff":  {reason: ReasonConvertToWOFF2},
	".svg":   {reason: ReasonRejectedType},
	".html":  {reason: ReasonCodeOutsideBundle},
	".htm":   {reason: ReasonCodeOutsideBundle},
	".js":    {reason: ReasonCodeOutsideBundle},
	".mjs":   {reason: ReasonCodeOutsideBundle},
	".css":   {reason: ReasonCodeOutsideBundle},
	".pdf":   {reason: ReasonLicenseFile},
	".txt":   {reason: ReasonLicenseFile},
	".md":    {reason: ReasonLicenseFile},
	".url":   {reason: ReasonLicenseFile},
	".rtf":   {reason: ReasonLicenseFile},
}

func ruleFor(name string) fileRule {
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, bundleSuffix) {
		return fileRule{kind: overlays.AssetWidget}
	}
	if rule, ok := extRules[path.Ext(lower)]; ok {
		return rule
	}
	return fileRule{reason: ReasonUnsupportedType}
}

func isJSON(name string) bool {
	return strings.EqualFold(path.Ext(name), ".json")
}
