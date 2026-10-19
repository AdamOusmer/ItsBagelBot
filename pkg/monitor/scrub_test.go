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

func TestNoIdentifyingNewRelicAttributeKeys(t *testing.T) {
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "node_modules" || d.Name() == ".git" || d.Name() == "ent") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if loc := identifyingKey.Find(src); loc != nil {
			t.Errorf("%s sends an identifying attribute to New Relic: %s", path, loc)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
