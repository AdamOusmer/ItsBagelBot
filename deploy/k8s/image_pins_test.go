// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package k8s

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
)

var firstPartyImage = regexp.MustCompile(`(?m)^\s*(?:-\s+)?image:\s*["']?ghcr\.io/adamousmer/itsbagelbot/` +
	`([a-z0-9][a-z0-9._/-]*):([A-Za-z0-9_][A-Za-z0-9._-]*)(@sha256:[0-9a-f]{64})?`)

var awaitingFirstPin = map[string]string{}

func TestFirstPartyImagesArePinnedByDigest(t *testing.T) {
	for _, filename := range manifestFilenames(t) {
		for _, match := range firstPartyImage.FindAllStringSubmatch(readText(t, filename), -1) {
			image, pinned := match[1], match[3] != ""
			_, awaiting := awaitingFirstPin[image]

			if pinned {
				assert.False(t, awaiting, "%s now pins %s by digest; drop it from awaitingFirstPin and remove its TODO", filename, image)
			} else {
				assert.True(t, awaiting, "%s runs %s without a digest; the pin PR stage only rewrites tag@digest lines, so it would never move", filename, image)
			}
		}
	}
}
