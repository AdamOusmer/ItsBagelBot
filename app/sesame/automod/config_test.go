package automod

import (
	"encoding/json"
	"testing"

	"ItsBagelBot/app/sesame/module"
)

const ipLoggerLine = "claim your prize at https://grabify.link/abcd right now friends"

// capsMid is caps-ratio ~0.65: flagged under PG (>=0.6) but not moderate (>=0.7).
// No 8-run and short symbols, so caps is the only possible signal.
const capsMid = "ABCDE FGHIJ KLM nopqrst"

func TestParseConfig(t *testing.T) {
	if ParseConfig(nil) != nil {
		t.Fatal("empty blob must yield nil (global default)")
	}
	if ParseConfig(json.RawMessage(`{bad`)) != nil {
		t.Fatal("malformed blob must yield nil, never a fail-closed config")
	}
	// The dashboard form writes flat strings; terms split on commas and newlines.
	c := ParseConfig(json.RawMessage(`{"profile":"18+","block_terms":"BadWord, other thing\nthird","allow_terms":" okThing "}`))
	if c == nil || c.Disabled || c.Profile != ProfileAdult {
		t.Fatalf("parsed config wrong: %+v", c)
	}
	if len(c.blockTerms) != 3 || string(c.blockTerms[0]) != "badword" || string(c.blockTerms[1]) != "other thing" || string(c.blockTerms[2]) != "third" {
		t.Fatalf("block terms not split+normalized: %q", c.blockTerms)
	}
	if len(c.allowTerms) != 1 || string(c.allowTerms[0]) != "okthing" {
		t.Fatalf("allow term not normalized: %q", c.allowTerms)
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

func TestProfileParse(t *testing.T) {
	for in, want := range map[string]Profile{
		"pg": ProfilePG, "family": ProfilePG, "strict": ProfilePG,
		"adult": ProfileAdult, "18+": ProfileAdult, "mature": ProfileAdult,
		"moderate": ProfileModerate, "": ProfileModerate, "garbage": ProfileModerate,
	} {
		if got := parseProfile(in); got != want {
			t.Fatalf("parseProfile(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestConfigDisabledOptsOut(t *testing.T) {
	g := New()
	// A disabled channel gets no action even on a floor hit: the broadcaster opted
	// the whole gate out and human mods carry the load.
	if v := g.InspectWith(module.RoleEveryone, ipLoggerLine, &Config{Disabled: true}); v.Action != ActionNone {
		t.Fatalf("disabled channel should not act, got %s", v.Action)
	}
}

func TestFloorImmovableUnderAdultAndAllow(t *testing.T) {
	g := New()
	cfg := ParseConfig(json.RawMessage(`{"profile":"adult","allow_terms":"grabify.link"}`))
	// Adult profile + an allow-term covering the domain must NOT let the floor
	// through: objectively-abusive infrastructure is enforced under every profile.
	if v := g.InspectWith(module.RoleEveryone, ipLoggerLine, cfg); v.Rule != "ip_logger" {
		t.Fatalf("floor must be immovable, got rule=%s action=%s", v.Rule, v.Action)
	}
}

func TestAdultProfileSuppressesCaps(t *testing.T) {
	g := New()
	shout := "STOP SCREAMING IN CHAT RIGHT NOW PLEASE"
	if v := g.InspectWith(module.RoleEveryone, shout, nil); v.Action != ActionDelete {
		t.Fatalf("moderate caps should flag, got %s", v.Action)
	}
	adult := ParseConfig(json.RawMessage(`{"profile":"adult"}`))
	if v := g.InspectWith(module.RoleEveryone, shout, adult); v.Action != ActionNone {
		t.Fatalf("adult profile drops caps nag, got %s", v.Action)
	}
}

func TestPGProfileTightensCaps(t *testing.T) {
	g := New()
	if v := g.InspectWith(module.RoleEveryone, capsMid, nil); v.Action != ActionNone {
		t.Fatalf("moderate: mid-caps under threshold should pass, got %s", v.Action)
	}
	pg := ParseConfig(json.RawMessage(`{"profile":"pg"}`))
	if v := g.InspectWith(module.RoleEveryone, capsMid, pg); v.Action != ActionDelete {
		t.Fatalf("pg: mid-caps should flag at the tighter threshold, got %s", v.Action)
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
}

func TestAllowTermSuppressesNonFloor(t *testing.T) {
	g := New()
	shout := "SCREAMING LOUDLY HELLO EVERYONE" // caps -> would be a heuristic delete
	if v := g.InspectWith(module.RoleEveryone, shout, nil); v.Action != ActionDelete {
		t.Fatalf("baseline caps should flag, got %s", v.Action)
	}
	// An allow-term present in the line suppresses the non-floor heuristic.
	cfg := ParseConfig(json.RawMessage(`{"allow_terms":"hello"}`))
	if v := g.InspectWith(module.RoleEveryone, shout, cfg); v.Action != ActionNone {
		t.Fatalf("allow term should suppress the heuristic, got %s", v.Action)
	}
	// Allow also cancels a channel block term (broadcaster owns that call).
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
