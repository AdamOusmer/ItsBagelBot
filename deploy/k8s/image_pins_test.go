// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package k8s

import (
	"regexp"
	"testing"
)

var firstPartyImage = regexp.MustCompile(`(?m)^\s*(?:-\s+)?image:\s*["']?ghcr\.io/adamousmer/itsbagelbot/` +
	`([a-z0-9][a-z0-9._/-]*):([A-Za-z0-9_][A-Za-z0-9._-]*)(@sha256:[0-9a-f]{64})?`)

var awaitingFirstPin = map[string]string{
	"deployer": "deployer.yaml: the first release that builds the deployer image fills in its tag@digest",
}

type pinnedImage struct {
	file   string
	image  string
	pinned bool
}

func TestFirstPartyImagesArePinnedByDigest(t *testing.T) {
	for _, filename := range manifestFilenames(t) {
		body := sourceFile{name: filename}.read(t)
		for _, match := range firstPartyImage.FindAllStringSubmatch(body, -1) {
			checkPin(t, pinnedImage{file: filename, image: match[1], pinned: match[3] != ""})
		}
	}
}

func checkPin(t *testing.T, p pinnedImage) {
	t.Helper()
	_, awaiting := awaitingFirstPin[p.image]
	switch {
	case p.pinned && awaiting:
		t.Errorf("%s now pins %s by digest; drop it from awaitingFirstPin and remove its TODO", p.file, p.image)
	case !p.pinned && !awaiting:
		t.Errorf("%s runs %s without a digest; the pin PR stage only rewrites tag@digest lines, so it would never move", p.file, p.image)
	}
}
