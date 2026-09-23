// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package github

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

// writeGolden regenerates testdata/pins.golden.txt and
// testdata/rewrite.golden.txt instead of asserting against them:
//
//	go test ./app/deployer/internal/stages/github/ -run Golden -github.write-golden
//
// Regeneration is a flag and never a side effect of running the suite: a
// fixture the suite can rewrite documents whatever the code currently does.
// The inputs in testdata/k8s are the real deploy/k8s files, copied; refresh
// them by copying again when a manifest gains a shape the parser must learn.
var writeGolden = flag.Bool("github.write-golden", false, "rewrite testdata/*.golden.txt from testdata/k8s")

// goldenPins is the rewrite corpus: every image the manifests pin except
// warp, at a new tag and a digest derived from the image name. Leaving warp
// out keeps one pin line in a changed file (gossip.yaml) untouched.
func goldenPins(lines []PinLine) map[deploy.ImageName]deploy.ImagePin {
	pins := map[deploy.ImageName]deploy.ImagePin{}
	for img := range pinsByImage(lines) {
		if img != "warp" {
			pins[img] = deploy.ImagePin{Tag: "v9.9.9-beta", Digest: testDigest(string(img))}
		}
	}
	return pins
}

func TestParsePinsGolden(t *testing.T) {
	var b strings.Builder
	for _, l := range ParsePins(loadManifests(t), testRepo) {
		fmt.Fprintf(&b, "%s:%d %s/%s %s %s@%s\n", l.File, l.Line, l.Kind, l.Workload, l.Image, l.Pin.Tag, l.Pin.Digest)
	}
	assertGolden(t, "pins.golden.txt", b.String())
}

// TestRewritePinsGolden pins every byte of the rewrite: each input line
// either appears unchanged or is listed in the golden with its replacement,
// and the line count cannot move.
func TestRewritePinsGolden(t *testing.T) {
	files := loadManifests(t)
	changed, err := RewritePins(files, testRepo, goldenPins(ParsePins(files, testRepo)))
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, p := range sortedPaths(changed) {
		lineDiff(&b, p, files[p], changed[p])
	}
	assertGolden(t, "rewrite.golden.txt", b.String())
}

func lineDiff(b *strings.Builder, p ports.FilePath, before, after []byte) {
	old, next := strings.Split(string(before), "\n"), strings.Split(string(after), "\n")
	if len(old) != len(next) {
		fmt.Fprintf(b, "%s: line count %d -> %d\n", p, len(old), len(next))
		return
	}
	for i := range old {
		if old[i] != next[i] {
			fmt.Fprintf(b, "%s:%d\n-%s\n+%s\n", p, i+1, old[i], next[i])
		}
	}
}

func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *writeGolden {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (regenerate with -github.write-golden)", err)
	}
	if got != string(want) {
		t.Errorf("%s differs from the output; if the change is intended, regenerate with -github.write-golden and review the diff\ngot:\n%s", name, got)
	}
}

// TestRewritePinsProperties: the changed-file set, a second rewrite being a
// no-op, and the rewritten pins reading back as requested.
func TestRewritePinsProperties(t *testing.T) {
	files := loadManifests(t)
	lines := ParsePins(files, testRepo)
	pins := goldenPins(lines)
	changed, err := RewritePins(files, testRepo, pins)
	if err != nil {
		t.Fatal(err)
	}
	after := overlay(files, changed)
	again, err := RewritePins(after, testRepo, pins)
	if err != nil {
		t.Fatal(err)
	}
	readBack := pinsByImage(ParsePins(after, testRepo))
	delete(readBack, "warp")
	got := rewriteFacts{Files: len(changed), Again: len(again), Pins: readBack, Untouched: changed["deploy/k8s/priorityclasses.yaml"] == nil}
	want := rewriteFacts{Files: 15, Again: 0, Pins: pins, Untouched: true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rewrite facts = %+v, want %+v", got, want)
	}
}

type rewriteFacts struct {
	Files     int
	Again     int
	Pins      map[deploy.ImageName]deploy.ImagePin
	Untouched bool
}

func TestRewritePinsRefusesMalformedPins(t *testing.T) {
	good := testDigest("x")
	cases := []struct {
		name string
		pin  deploy.ImagePin
	}{
		{"empty tag", deploy.ImagePin{Tag: "", Digest: good}},
		{"tag with a colon", deploy.ImagePin{Tag: "v1:2", Digest: good}},
		{"tag with a space", deploy.ImagePin{Tag: "v1 2", Digest: good}},
		{"digest without algorithm", deploy.ImagePin{Tag: "v1", Digest: deploy.Digest(strings.TrimPrefix(string(good), "sha256:"))}},
		{"short digest", deploy.ImagePin{Tag: "v1", Digest: "sha256:abc"}},
		{"uppercase digest", deploy.ImagePin{Tag: "v1", Digest: deploy.Digest(strings.ToUpper(string(good)))}},
	}
	files := loadManifests(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := RewritePins(files, testRepo, map[deploy.ImageName]deploy.ImagePin{"users": tc.pin})
			if !errors.Is(err, ports.ErrInvalid) {
				t.Errorf("err = %v, want ErrInvalid", err)
			}
		})
	}
}

// TestParsePinsLineShapes covers what the real files do not: a list item, a
// quoted value, a tag-only line, another repo, a comment.
func TestParsePinsLineShapes(t *testing.T) {
	d := string(testDigest("a"))
	body := strings.Join([]string{
		"kind: Deployment",
		"metadata:",
		"  namespace: app",
		"  name: web",
		"spec:",
		"  - image: " + testRepo + "/web:v1@" + d,
		`    image: "` + testRepo + `/web-sidecar:v1@` + d + `"`,
		"    image: " + testRepo + "/web:v1",
		"    image: ghcr.io/other/web:v1@" + d,
		"    # image: " + testRepo + "/web:v1@" + d,
		"",
	}, "\n")
	got := ParsePins(ports.Files{"deploy/k8s/web.yaml": []byte(body)}, testRepo)
	want := []PinLine{
		{File: "deploy/k8s/web.yaml", Line: 6, Image: "web", Pin: deploy.ImagePin{Tag: "v1", Digest: deploy.Digest(d)}, Kind: "Deployment", Workload: "web"},
		{File: "deploy/k8s/web.yaml", Line: 7, Image: "web-sidecar", Pin: deploy.ImagePin{Tag: "v1", Digest: deploy.Digest(d)}, Kind: "Deployment", Workload: "web"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParsePins =\n%+v\nwant\n%+v", got, want)
	}
}

// TestServicesForManifests: the rollout units come from the manifests, a
// sidecar image maps to the Deployment that runs it and the notifications
// CronJob is not a unit of its own.
func TestServicesForManifests(t *testing.T) {
	lines := ParsePins(loadManifests(t), testRepo)
	cases := []struct {
		name   string
		images []deploy.ImageName
		want   []string
	}{
		{"sidecar maps to its deployment", []deploy.ImageName{"warp"}, []string{"gossip"}},
		{"cronjob image counts once", []deploy.ImageName{"notifications"}, []string{"notifications"}},
		{"three deployments in one file", []deploy.ImageName{"discord-engine", "discord-ingress"}, []string{"discord-ingress", "discord-engine"}},
		{"every image", nil, allServices},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			images := pinsByImage(lines)
			if tc.images != nil {
				images = pinSet(tc.images...)
			}
			if got := servicesFor(lines, images); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("servicesFor = %v, want %v", got, tc.want)
			}
		})
	}
}

func pinSet(images ...deploy.ImageName) map[deploy.ImageName]deploy.ImagePin {
	out := map[deploy.ImageName]deploy.ImagePin{}
	for _, img := range images {
		out[img] = deploy.ImagePin{Tag: "v1", Digest: testDigest(string(img))}
	}
	return out
}
