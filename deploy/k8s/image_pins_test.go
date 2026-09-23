// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package k8s

import (
	"regexp"
	"testing"
)

// firstPartyImage matches every image line for the fleet's own registry path,
// pinned or not. Group 3 is the digest, empty on a tag-only line.
var firstPartyImage = regexp.MustCompile(`(?m)^\s*(?:-\s+)?image:\s*["']?ghcr\.io/adamousmer/itsbagelbot/` +
	`([a-z0-9][a-z0-9._/-]*):([A-Za-z0-9_][A-Za-z0-9._-]*)(@sha256:[0-9a-f]{64})?`)

// awaitingFirstPin lists first-party images that have never been published,
// so no digest exists to pin. Each entry must carry a TODO beside its image
// line, and the test fails the moment the image is pinned, so an entry
// cannot outlive the reason for it.
var awaitingFirstPin = map[string]string{
	"deployer": "deployer.yaml: the first release that builds the deployer image fills in its tag@digest",
}

type pinnedImage struct {
	file   string
	image  string
	pinned bool
}

// TestFirstPartyImagesArePinnedByDigest: the deployer's pin PR stage rewrites
// only tag@digest lines (app/deployer/internal/stages/github/pins.go). A
// tag-only line is not an error there, it is invisible: the train would never
// move that image, and the tag alone could be re-pushed under the pod. So
// every first-party image in this directory is pinned by digest, bar the
// documented exceptions above.
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
