package automod

import (
	"encoding/json"
	"testing"

	"ItsBagelBot/app/sesame/module"
)

const ipLoggerLine = "claim your prize at https://grabify.link/abcd right now friends"

// capsMid is caps-ratio ~0.65: flagged under the strict caps threshold (>=0.6)
// but not moderate (>=0.7). No 8-run and short symbols, so caps is the only
// possible signal.
const capsMid = "ABCDE FGHIJ KLM nopqrst"

func TestParseConfig(t *testing.T) {
	if ParseConfig(nil) != nil {
		t.Fatal("empty blob must yield nil (global default)")
	}
	if ParseConfig(json.RawMessage(`{bad`)) != nil {
		t.Fatal("malformed blob must yield nil, never a fail-closed config")
	}
	// The dashboard form writes flat strings; terms split on commas and newlines.
	c := ParseConfig(json.RawMessage(`{"level":"all","block_terms":"BadWord, other thing\nthird","allow_terms":" okThing "}`))
	if c == nil || c.Disabled || c.Level != LevelStrict {
		t.Fatalf("parsed config wrong: %+v", c)
	}
	if len(c.blockTerms) != 3 || string(c.blockTerms[0]) != "badword" || string(c.blockTerms[1]) != "other thing" || string(c.blockTerms[2]) != "third" {
		t.Fatalf("block terms not split+normalized: %q", c.blockTerms)
	}
	if len(c.allowTerms) != 1 || string(c.allowTerms[0]) != "okthing" {
		t.Fatalf("allow term not normalized: %q", c.allowTerms)
	}
}

// The legacy "profile" key is still honored as a level alias.
func TestParseConfigLegacyProfileAlias(t *testing.T) {
	c := ParseConfig(json.RawMessage(`{"profile":"adult"}`))
	if c == nil || c.Level != LevelBasic {
		t.Fatalf("legacy profile alias: got %+v", c)
	}
}

func TestSplitTerms(t *testing.T) {
	got := splitTerms("a, b\n c ,,\n")
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
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

// resolved(): the full "none -> all" span, plus per-section override.
func TestResolvedSections(t *testing.T) {
	none := (&Config{Level: LevelNone}).resolved()
	if none.harassment || none.sexual || none.profanity || none.style || none.links {
		t.Fatalf("none must be floor-only: %+v", none)
	}
	all := (&Config{Level: LevelStrict}).resolved()
	if !(all.harassment && all.sexual && all.profanity && all.style && all.links) {
		t.Fatalf("strict must enable every section: %+v", all)
	}
	// A disabled row collapses to floor-only regardless of its level.
	dis := (&Config{Level: LevelStrict, Disabled: true}).resolved()
	if dis.harassment || dis.style {
		t.Fatalf("disabled row must be floor-only: %+v", dis)
	}
	// Per-section override on top of a level: moderate has profanity off; turn it on.
	over := (&Config{Level: LevelModerate, profanity: triOn}).resolved()
	if !over.profanity {
		t.Fatal("section override triOn must force profanity on")
	}
	// And off overrides a level that has it on.
	off := (&Config{Level: LevelStrict, style: triOff}).resolved()
	if off.style {
		t.Fatal("section override triOff must force style off")
	}
}

// The floor holds under EVERY level, a disabled row, AND an allow-term - this is
// the account-safety guarantee.
func TestFloorImmovableAcrossLevels(t *testing.T) {
	g := New()
	for _, raw := range []string{
		`{"level":"none"}`,
		`{"level":"none","allow_terms":"grabify.link"}`,
		`{"level":"all"}`,
	} {
		cfg := ParseConfig(json.RawMessage(raw))
		if v := g.InspectWith(module.RoleEveryone, ipLoggerLine, cfg); v.Rule != "ip_logger" {
			t.Fatalf("floor must hold for %s: got rule=%s action=%s", raw, v.Rule, v.Action)
		}
	}
	// A disabled module row is floor-only, NOT a full opt-out.
	if v := g.InspectWith(module.RoleEveryone, ipLoggerLine, &Config{Disabled: true}); v.Rule != "ip_logger" {
		t.Fatalf("disabled row must still enforce the floor: got %s", v.Rule)
	}
}

// Level none disables the style checks (caps) but the floor still holds.
func TestLevelNoneDropsStyle(t *testing.T) {
	g := New()
	shout := "STOP SCREAMING IN CHAT RIGHT NOW PLEASE"
	if v := g.InspectWith(module.RoleEveryone, shout, nil); v.Action != ActionDelete {
		t.Fatalf("moderate caps should flag, got %s", v.Action)
	}
	none := ParseConfig(json.RawMessage(`{"level":"none"}`))
	if v := g.InspectWith(module.RoleEveryone, shout, none); v.Action != ActionNone {
		t.Fatalf("level none drops the caps check, got %s", v.Action)
	}
}

func TestStrictTightensCaps(t *testing.T) {
	g := New()
	if v := g.InspectWith(module.RoleEveryone, capsMid, nil); v.Action != ActionNone {
		t.Fatalf("moderate: mid-caps under threshold should pass, got %s", v.Action)
	}
	strict := ParseConfig(json.RawMessage(`{"level":"all"}`))
	if v := g.InspectWith(module.RoleEveryone, capsMid, strict); v.Action != ActionDelete {
		t.Fatalf("strict: mid-caps should flag at the tighter threshold, got %s", v.Action)
	}
}

func TestBlockTermFlags(t *testing.T) {
	g := New()
	cfg := ParseConfig(json.RawMessage(`{"block_terms":"badword"}`))
	if v := g.InspectWith(module.RoleEveryone, "this has badword in it", cfg); v.Rule != "block_term" {
		t.Fatalf("channel block term should flag, got rule=%s", v.Rule)
	}
	// Without the config the same line is clean.
	if v := g.InspectWith(module.RoleEveryone, "this has badword in it", nil); v.Action != ActionNone {
		t.Fatalf("no config: line should be clean, got %s", v.Action)
	}
	// A disabled row does not apply channel block terms (floor-only).
	dis := ParseConfig(json.RawMessage(`{"block_terms":"badword"}`))
	dis.Disabled = true
	if v := g.InspectWith(module.RoleEveryone, "this has badword in it", dis); v.Action != ActionNone {
		t.Fatalf("disabled row must ignore block terms, got %s", v.Action)
	}
}

func TestAllowTermSuppressesNonFloor(t *testing.T) {
	g := New()
	shout := "SCREAMING LOUDLY HELLO EVERYONE" // caps -> would be a heuristic delete
	if v := g.InspectWith(module.RoleEveryone, shout, nil); v.Action != ActionDelete {
		t.Fatalf("baseline caps should flag, got %s", v.Action)
	}
	cfg := ParseConfig(json.RawMessage(`{"allow_terms":"hello"}`))
	if v := g.InspectWith(module.RoleEveryone, shout, cfg); v.Action != ActionNone {
		t.Fatalf("allow term should suppress the heuristic, got %s", v.Action)
	}
	both := ParseConfig(json.RawMessage(`{"block_terms":"badword","allow_terms":"badword"}`))
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
