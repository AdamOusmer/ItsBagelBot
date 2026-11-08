// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package overlay

import (
	"errors"

	"ItsBagelBot/pkg/codec"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type failingStore struct{ sceneStore }

func (f failingStore) save(saved) error { return errors.New("disk unavailable") }

func TestFailedSaveDoesNotPublishOrRevoke(t *testing.T) {
	source, err := OpenSource(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	before := source.current.Load()
	source.store = failingStore{source.store}
	if err = source.Publish(validScene); err == nil {
		t.Fatal("expected save error")
	}
	if _, err = source.ResetSourceURL(); err == nil {
		t.Fatal("expected reset save error")
	}
	if source.current.Load() != before {
		t.Fatal("failed save changed the published scene or credentials")
	}
	select {
	case <-before.changed:
		t.Fatal("failed save woke readers")
	default:
	}
}

func TestEscapedSceneSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	source, err := OpenSource(dir)
	if err != nil {
		t.Fatal(err)
	}
	content := strings.Repeat("<", maxSceneBytes/2)
	raw := strings.Replace(validScene, "<script>alert(1)</script>", content, 1)
	if err = source.Publish(raw); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "overlay.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() <= maxSceneBytes {
		t.Fatal("fixture did not exercise escaped persistence expansion")
	}
	restarted, err := OpenSource(dir)
	if err != nil {
		t.Fatal(err)
	}
	scene := restarted.Current()
	if scene.Layers[0].Content != content {
		t.Fatal("restart changed text")
	}
	scene.Layers[0].Content = "mutated copy"
	if restarted.Current().Layers[0].Content != content {
		t.Fatal("desktop caller mutated published scene")
	}
}

func TestStrictSceneInput(t *testing.T) {
	for _, raw := range []string{
		strings.Replace(validScene, `"resolution":`, `"extra":true,"resolution":`, 1),
		strings.Replace(validScene, `"kind":"text"`, `"kind":"text","html":"<script>"`, 1),
		validScene + `{}`,
	} {
		if _, err := parseScene(raw); err == nil {
			t.Fatal("accepted unknown field or trailing data")
		}
	}
}

func TestEmptyChatWireShape(t *testing.T) {
	source, err := OpenSource(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	raw := `{"resolution":"1080p","layers":[{"id":"chat","kind":"chat","x":0,"y":0,"w":50,"h":50,"opacity":100,"rows":3}]}`
	if err = source.Publish(raw); err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Scene struct {
			Layers []struct {
				Messages codec.RawMessage `json:"messages"`
			} `json:"layers"`
		} `json:"scene"`
	}
	if err = codec.Unmarshal(source.current.Load().wire, &wire); err != nil {
		t.Fatal(err)
	}
	if string(wire.Scene.Layers[0].Messages) != "[]" {
		t.Fatal("empty chat messages must be an array, never null or missing")
	}
}
