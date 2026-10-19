// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package monitor

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/newrelic/go-agent/v3/newrelic"
)

func TestScrubErrorDropsDetailAfterFirstColon(t *testing.T) {
	got := ScrubError(errors.New("twitch rejected request: 400 {\"broadcaster_id\":\"123456789\"}"))

	nrErr, ok := got.(newrelic.Error)
	if !ok {
		t.Fatalf("want newrelic.Error, got %T", got)
	}
	if strings.Contains(nrErr.Message, "400") || strings.Contains(nrErr.Message, "123456789") {
		t.Fatalf("message leaks detail: %q", nrErr.Message)
	}
}

func TestScrubErrorRedactsLongDigitRuns(t *testing.T) {
	nrErr := ScrubError(errors.New("no channel 987654321")).(newrelic.Error)
	if strings.Contains(nrErr.Message, "987654321") {
		t.Fatalf("message leaks id: %q", nrErr.Message)
	}
}

var identifyingKey = regexp.MustCompile(`(?:Add(?:Custom)?Attribute|RecordCustomEvent)\(\s*"[^"]*(?:broadcaster_id|user_id|userid|login)[^"]*"`)

func prunedDir(d fs.DirEntry) bool {
	switch d.Name() {
	case "node_modules", ".git", "ent":
		return d.IsDir()
	}
	return false
}

func productionGoFile(d fs.DirEntry) bool {
	name := d.Name()
	return !d.IsDir() && strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go")
}

func productionGoFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if prunedDir(d) {
			return filepath.SkipDir
		}
		if productionGoFile(d) {
			files = append(files, path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestNoIdentifyingNewRelicAttributeKeys(t *testing.T) {
	for _, path := range productionGoFiles(t, filepath.Join("..", "..")) {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if hit := identifyingKey.Find(src); hit != nil {
			t.Errorf("%s sends an identifying attribute to New Relic: %s", path, hit)
		}
	}
}
