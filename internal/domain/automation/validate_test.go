// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"ItsBagelBot/internal/domain/automation"
)

var update = flag.Bool("update", false, "rewrite golden files")

func fixtures(t *testing.T, dir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("testdata", dir, "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixtures in %s: %v", dir, err)
	}
	var out []string
	for _, f := range files {
		if !strings.HasSuffix(f, ".issues.json") {
			out = append(out, f)
		}
	}
	return out
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func indented(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(raw, '\n')
}

func checkGolden(t *testing.T, path string, got []byte) {
	t.Helper()
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	if want := readFile(t, path); !bytes.Equal(want, got) {
		t.Fatalf("%s mismatch\nwant:\n%s\ngot:\n%s", path, want, got)
	}
}

func TestValidFixturesHaveNoIssues(t *testing.T) {
	for _, f := range fixtures(t, "valid") {
		t.Run(filepath.Base(f), func(t *testing.T) {
			if _, issues := automation.Parse(readFile(t, f)); issues != nil {
				t.Fatalf("unexpected issues: %v", issues)
			}
		})
	}
}

func TestInvalidFixturesMatchGoldens(t *testing.T) {
	for _, f := range fixtures(t, "invalid") {
		t.Run(filepath.Base(f), func(t *testing.T) {
			_, issues := automation.Parse(readFile(t, f))
			if len(issues) == 0 {
				t.Fatal("expected issues")
			}
			checkGolden(t, strings.TrimSuffix(f, ".json")+".issues.json", indented(t, issues))
		})
	}
}

func TestValidFixturesRoundTrip(t *testing.T) {
	for _, f := range fixtures(t, "valid") {
		t.Run(filepath.Base(f), func(t *testing.T) {
			def, _ := automation.Parse(readFile(t, f))
			first, err := json.Marshal(def)
			if err != nil {
				t.Fatal(err)
			}
			again, issues := automation.Parse(first)
			if issues != nil {
				t.Fatalf("reparse issues: %v", issues)
			}
			second, _ := json.Marshal(again)
			if !bytes.Equal(first, second) {
				t.Fatalf("round trip drifted:\n%s\n%s", first, second)
			}
		})
	}
}

func TestParseRejectsOversizedAndMalformed(t *testing.T) {
	cases := map[string]struct {
		raw  string
		code string
	}{
		"trailing":  {`{"version":1,"trigger":{"type":"manual"}} {}`, "invalid_json"},
		"unknown":   {`{"version":1,"trigger":{"type":"manual"},"x":1}`, "unknown_field"},
		"oversized": {strings.Repeat(" ", automation.MaxDefinitionBytes+1), "too_long"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, issues := automation.Parse([]byte(tc.raw))
			if !singleIssue(issues, "", tc.code) {
				t.Fatalf("got %v want %s at root", issues, tc.code)
			}
		})
	}
}

func singleIssue(issues []automation.Issue, path, code string) bool {
	if len(issues) != 1 {
		return false
	}
	return issues[0].Path == path && issues[0].Code == code
}

func hasCommandDefaults(p automation.CommandTrigger) bool {
	if p.Match != "start" {
		return false
	}
	return len(p.Sources) == 1 && p.Sources[0] == "chat"
}

func TestValidateName(t *testing.T) {
	cases := map[string]string{
		"Welcome message": "",
		"   ":             "required",
		"bad\x07name":     "invalid_format",
		strings.Repeat("n", automation.MaxNameLength+1): "too_long",
	}
	for name, code := range cases {
		issues := automation.ValidateName(name)
		if code == "" && issues != nil {
			t.Errorf("%q: unexpected %v", name, issues)
		}
		if code != "" && !singleIssue(issues, "name", code) {
			t.Errorf("%q: got %v want %s", name, issues, code)
		}
	}
}

func TestFlattenWalksNestedBranches(t *testing.T) {
	def, issues := automation.Parse(readFile(t, filepath.Join("testdata", "valid", "nested_and_disabled.json")))
	if issues != nil {
		t.Fatal(issues)
	}
	if got := len(def.Flatten()); got != 8 {
		t.Fatalf("flatten = %d, want 8", got)
	}
	if def.Actions[0].Nested() == nil || def.Actions[2].Nested() != nil {
		t.Fatal("nested accessor mismatch")
	}
}

func TestDecodeParamsAppliesDefaults(t *testing.T) {
	p, err := automation.DecodeParams[automation.CommandTrigger](json.RawMessage(`{"name":"!hi"}`))
	if err != nil || !hasCommandDefaults(p) {
		t.Fatalf("got %+v, %v", p, err)
	}
	if _, err := automation.DecodeParams[automation.CommandTrigger](json.RawMessage(`{"nope":1}`)); err == nil {
		t.Fatal("expected unknown field error")
	}
}

func TestEventKindsGolden(t *testing.T) {
	checkGolden(t, filepath.Join("testdata", "event_kinds.golden.json"), indented(t, automation.EventKinds()))
}

func TestEventKindExtract(t *testing.T) {
	cases := []struct {
		kind  string
		event string
		want  map[string]string
	}{
		{"channel.cheer", `{"user_name":"Ann","user_login":"ann","user_id":"1","bits":250,"message":"gg","is_anonymous":false}`,
			map[string]string{"user": "Ann", "login": "ann", "user_id": "1", "amount": "250", "message": "gg", "anonymous": "false"}},
		{"channel.raid", `{"from_broadcaster_user_name":"Bo","from_broadcaster_user_login":"bo","from_broadcaster_user_id":"2","viewers":42}`,
			map[string]string{"user": "Bo", "login": "bo", "user_id": "2", "viewers": "42"}},
		{"channel.subscription.message", `{"user_name":"Cy","tier":"1000","cumulative_months":7,"streak_months":3.0,"message":{"text":"hi","emotes":[]}}`,
			map[string]string{"user": "Cy", "tier": "1000", "months": "7", "streak": "3", "message": "hi"}},
		{"channel.channel_points_custom_reward_redemption.add", `{"user_login":"di","reward":{"id":"r1","title":"Hydrate","cost":300},"user_input":"water"}`,
			map[string]string{"login": "di", "reward": "Hydrate", "reward_id": "r1", "cost": "300", "input": "water"}},
		{"channel.cheer", `not json`, map[string]string{}},
	}
	for _, tc := range cases {
		k, ok := automation.LookupEventKind(tc.kind)
		if !ok {
			t.Fatalf("kind %s missing", tc.kind)
		}
		if got := k.Extract([]byte(tc.event)); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %v want %v", tc.kind, got, tc.want)
		}
	}
}
