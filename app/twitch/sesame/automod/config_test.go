// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

import (
	"slices"
	"strings"
	"testing"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/pkg/codec"
)

func termStrings(terms [][]byte) []string {
	out := make([]string, len(terms))
	for i, term := range terms {
		out[i] = string(term)
	}
	return out
}

func sectionsOn(s sections) string {
	var on []string
	for _, f := range []struct {
		name    string
		enabled bool
	}{
		{"harassment", s.harassment},
		{"sexual", s.sexual},
		{"profanity", s.profanity},
		{"style", s.style},
		{"links", s.links},
	} {
		if f.enabled {
			on = append(on, f.name)
		}
	}
	return strings.Join(on, " ")
}

const allSections = "harassment sexual profanity style links"

const ipLoggerLine = "claim your prize at https://grabify.link/abcd right now friends"

const capsMid = "ABCDE FGHIJ KLM nopqrst"

func TestParseConfig(t *testing.T) {
	if ParseConfig(nil) != nil {
		t.Fatal("empty blob must yield nil (global default)")
	}
	if ParseConfig(codec.RawMessage(`{bad`)) != nil {
		t.Fatal("malformed blob must yield nil, never a fail-closed config")
	}
	c := ParseConfig(codec.RawMessage(`{"level":"all","block_terms":"BadWord, other thing\nthird","allow_terms":" okThing "}`))
	if c == nil {
		t.Fatal("config must parse")
	}
	if c.Disabled || c.Level != LevelStrict {
		t.Fatalf("parsed config wrong: %+v", c)
	}
	if got := termStrings(c.blockTerms); !slices.Equal(got, []string{"badword", "other thing", "third"}) {
		t.Fatalf("block terms not split+normalized: %q", got)
	}
	if got := termStrings(c.allowTerms); !slices.Equal(got, []string{"okthing"}) {
		t.Fatalf("allow term not normalized: %q", got)
	}
}

func TestParseConfigLegacyProfileAlias(t *testing.T) {
	c := ParseConfig(codec.RawMessage(`{"profile":"adult"}`))
	if c == nil || c.Level != LevelBasic {
		t.Fatalf("legacy profile alias: got %+v", c)
	}
}

func TestSplitTerms(t *testing.T) {
	if got := splitTerms("a, b\n c ,,\n"); !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Fatalf("splitTerms = %q", got)
	}
	if splitTerms("") != nil {
		t.Fatal("empty input yields nil")
	}
}

func TestParseLevel(t *testing.T) {
	for in, want := range map[string]Level{
		"none": LevelNone, "off": LevelNone, "floor": LevelNone,
		"basic": LevelBasic, "adult": LevelBasic, "18+": LevelBasic,
		"strict": LevelStrict, "all": LevelStrict, "pg": LevelStrict, "family": LevelStrict,
		"moderate": LevelModerate, "": LevelModerate, "garbage": LevelModerate,
	} {
		if got := parseLevel(in); got != want {
			t.Fatalf("parseLevel(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestResolvedSections(t *testing.T) {
	if got := sectionsOn((&Config{Level: LevelNone}).resolved()); got != "" {
		t.Fatalf("none must be floor-only, enabled: %s", got)
	}
	if got := sectionsOn((&Config{Level: LevelStrict}).resolved()); got != allSections {
		t.Fatalf("strict must enable every section, got: %s", got)
	}
	if got := sectionsOn((&Config{Level: LevelStrict, Disabled: true}).resolved()); got != "" {
		t.Fatalf("disabled row must be floor-only, enabled: %s", got)
	}
	over := (&Config{Level: LevelModerate, profanity: triOn}).resolved()
	if !over.profanity {
		t.Fatal("section override triOn must force profanity on")
	}
	off := (&Config{Level: LevelStrict, style: triOff}).resolved()
	if off.style {
		t.Fatal("section override triOff must force style off")
	}
}

func TestFloorImmovableAcrossLevels(t *testing.T) {
	g := New()
	for _, raw := range []string{
		`{"level":"none"}`,
		`{"level":"none","allow_terms":"grabify.link"}`,
		`{"level":"all"}`,
	} {
		cfg := ParseConfig(codec.RawMessage(raw))
		if v := g.InspectWith(module.RoleEveryone, ipLoggerLine, cfg); v.Rule != "ip_logger" {
			t.Fatalf("floor must hold for %s: got rule=%s action=%s", raw, v.Rule, v.Action)
		}
	}
	if v := g.InspectWith(module.RoleEveryone, ipLoggerLine, &Config{Disabled: true}); v.Rule != "ip_logger" {
		t.Fatalf("disabled row must still enforce the floor: got %s", v.Rule)
	}
}

func TestLevelNoneDropsStyle(t *testing.T) {
	g := newGateWithEmotes()
	shout := "STOP SCREAMING IN CHAT RIGHT NOW PLEASE"
	if v := g.InspectWith(module.RoleEveryone, shout, nil); v.Action != ActionDelete {
		t.Fatalf("moderate caps should flag, got %s", v.Action)
	}
	none := ParseConfig(codec.RawMessage(`{"level":"none"}`))
	if v := g.InspectWith(module.RoleEveryone, shout, none); v.Action != ActionNone {
		t.Fatalf("level none drops the caps check, got %s", v.Action)
	}
}

func TestStrictTightensCaps(t *testing.T) {
	g := newGateWithEmotes()
	if v := g.InspectWith(module.RoleEveryone, capsMid, nil); v.Action != ActionNone {
		t.Fatalf("moderate: mid-caps under threshold should pass, got %s", v.Action)
	}
	strict := ParseConfig(codec.RawMessage(`{"level":"all"}`))
	if v := g.InspectWith(module.RoleEveryone, capsMid, strict); v.Action != ActionDelete {
		t.Fatalf("strict: mid-caps should flag at the tighter threshold, got %s", v.Action)
	}
}

func TestBlockTermFlags(t *testing.T) {
	g := New()
	cfg := ParseConfig(codec.RawMessage(`{"block_terms":"badword"}`))
	if v := g.InspectWith(module.RoleEveryone, "this has badword in it", cfg); v.Rule != "block_term" {
		t.Fatalf("channel block term should flag, got rule=%s", v.Rule)
	}
	if v := g.InspectWith(module.RoleEveryone, "this has badword in it", nil); v.Action != ActionNone {
		t.Fatalf("no config: line should be clean, got %s", v.Action)
	}
	dis := ParseConfig(codec.RawMessage(`{"block_terms":"badword"}`))
	dis.Disabled = true
	if v := g.InspectWith(module.RoleEveryone, "this has badword in it", dis); v.Action != ActionNone {
		t.Fatalf("disabled row must ignore block terms, got %s", v.Action)
	}
}

func TestAllowTermSuppressesNonFloor(t *testing.T) {
	g := newGateWithEmotes()
	shout := "SCREAMING LOUDLY HELLO EVERYONE"
	if v := g.InspectWith(module.RoleEveryone, shout, nil); v.Action != ActionDelete {
		t.Fatalf("baseline caps should flag, got %s", v.Action)
	}
	cfg := ParseConfig(codec.RawMessage(`{"allow_terms":"hello"}`))
	if v := g.InspectWith(module.RoleEveryone, shout, cfg); v.Action != ActionNone {
		t.Fatalf("allow term should suppress the heuristic, got %s", v.Action)
	}
	both := ParseConfig(codec.RawMessage(`{"block_terms":"badword","allow_terms":"badword"}`))
	if v := g.InspectWith(module.RoleEveryone, "look a badword here", both); v.Action != ActionNone {
		t.Fatalf("allow should cancel its own block term, got %s", v.Action)
	}
}

func TestNilConfigMatchesDefault(t *testing.T) {
	g := New()
	if g.InspectWith(module.RoleEveryone, ipLoggerLine, nil) != g.Inspect(module.RoleEveryone, ipLoggerLine) {
		t.Fatal("nil config must equal the default Inspect")
	}
}
