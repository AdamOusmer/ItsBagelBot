package automod

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/moderation"
)

// A slur from the embedded floor list, written out only via obfuscation in the
// test inputs below so the source stays clean where possible; the plain form is
// exercised through the artifact file itself.
func floorTerm(t *testing.T) string {
	t.Helper()
	l := EmbeddedLexicon()
	if len(l.Terms(moderation.CatHate)) == 0 {
		t.Fatal("embedded hate list is empty")
	}
	return l.Terms(moderation.CatHate)[0]
}

func TestLexiconHateFloorImmovable(t *testing.T) {
	g := New()
	slur := floorTerm(t)
	line := "you are such a " + slur + " lol"

	v := g.Inspect(module.RoleEveryone, line)
	if v.Action != ActionTimeout || v.Seconds != 1800 {
		t.Fatalf("hate floor: got %s/%ds rule=%s", v.Action, v.Seconds, v.Rule)
	}

	// Neither the adult profile nor an allow-term covering the slur moves it.
	cfg := ParseConfig(json.RawMessage(`{"profile":"adult","allow_terms":"` + slur + `"}`))
	if v := g.InspectWith(module.RoleEveryone, line, cfg); v.Action != ActionTimeout {
		t.Fatalf("hate floor must be immovable: got %s rule=%s", v.Action, v.Rule)
	}
}

// The floor must hold on the SHORT clean path too: a bare slur in a line under
// the clean-bail length is routed to the deep path by the folded pre-scan.
func TestLexiconHateShortLine(t *testing.T) {
	g := New()
	slur := floorTerm(t)
	if v := g.Inspect(module.RoleEveryone, slur); v.Action != ActionTimeout {
		t.Fatalf("bare short slur: got %s rule=%s", v.Action, v.Rule)
	}
	// And the word boundary still protects short innocents from the pre-scan.
	if v := g.Inspect(module.RoleEveryone, "nice clip"); v.Action != ActionNone {
		t.Fatalf("short clean line flagged: %s", v.Action)
	}
}

func TestLexiconHateCatchesObfuscation(t *testing.T) {
	g := New()
	slur := floorTerm(t)
	// Leetify: a->4, e->3, i->1, o->0, s->5; the skeleton fold reverses it.
	leet := make([]rune, 0, len(slur))
	for _, r := range slur {
		switch r {
		case 'a':
			r = '4'
		case 'e':
			r = '3'
		case 'i':
			r = '1'
		case 'o':
			r = '0'
		case 's':
			r = '5'
		}
		leet = append(leet, r)
	}
	line := "you are such a " + string(leet) + " lol"
	if v := g.Inspect(module.RoleEveryone, line); v.Action != ActionTimeout {
		t.Fatalf("leet-obfuscated slur must fold and hit: got %s rule=%s", v.Action, v.Rule)
	}
}

func TestLexiconHarassmentWarns(t *testing.T) {
	g := New()
	line := "nobody asked just go kill yourself already dude seriously"
	v := g.Inspect(module.RoleEveryone, line)
	if v.Action != ActionWarn {
		t.Fatalf("harassment: got %s rule=%s, want warn", v.Action, v.Rule)
	}

	// Harassment is NOT floor: a channel allow-term suppresses it.
	cfg := ParseConfig(json.RawMessage(`{"allow_terms":"kill yourself"}`))
	if v := g.InspectWith(module.RoleEveryone, line, cfg); v.Action != ActionNone {
		t.Fatalf("allow-term should suppress harassment: got %s", v.Action)
	}
}

func TestLexiconProfileGates(t *testing.T) {
	g := New()
	sexual := "check out this hentai stream it is really something else"
	profane := "that was some absolute bullshit refs are blind i swear" // "bullshit" is not in the list; use a listed word
	profane = "well shit that was a terrible play from the team today"

	pg := ParseConfig(json.RawMessage(`{"profile":"pg"}`))
	adult := ParseConfig(json.RawMessage(`{"profile":"adult"}`))

	// Sexual: deleted for pg and moderate (default), ignored for adult.
	if v := g.Inspect(module.RoleEveryone, sexual); v.Action != ActionDelete {
		t.Fatalf("sexual under moderate: got %s rule=%s", v.Action, v.Rule)
	}
	if v := g.InspectWith(module.RoleEveryone, sexual, adult); v.Action != ActionNone {
		t.Fatalf("sexual under adult must pass: got %s rule=%s", v.Action, v.Rule)
	}

	// Profanity: deleted only for pg.
	if v := g.Inspect(module.RoleEveryone, profane); v.Action != ActionNone {
		t.Fatalf("profanity under moderate must pass: got %s rule=%s", v.Action, v.Rule)
	}
	if v := g.InspectWith(module.RoleEveryone, profane, pg); v.Action != ActionDelete {
		t.Fatalf("profanity under pg must delete: got %s rule=%s", v.Action, v.Rule)
	}
}

func TestLexiconWordBounded(t *testing.T) {
	g := New()
	pg := ParseConfig(json.RawMessage(`{"profile":"pg"}`))
	// "class"/"assignment" contain "ass"; "cocktail" contains "cock". Word
	// bounding keeps them clean even under the strictest profile.
	line := "the class assignment about cocktail recipes is due tomorrow evening"
	if v := g.InspectWith(module.RoleEveryone, line, pg); v.Action != ActionNone {
		t.Fatalf("scunthorpe: got %s rule=%s", v.Action, v.Rule)
	}
}

func TestLexiconNonLatinGuard(t *testing.T) {
	g := New()
	// A genuine Russian sentence (long enough to force the deep path). Its
	// skeleton partially folds to latin, but the language juror restricts the
	// scan to the floor, so no word-list category can fire.
	russian := "Привет всем как дела сегодня отличный стрим спасибо за игру друзья"
	if v := g.Inspect(module.RoleEveryone, russian); v.Action != ActionNone {
		t.Fatalf("non-latin chat must not be judged by english lists: got %s rule=%s", v.Action, v.Rule)
	}
}

func TestLexiconDirOverrideAndFallback(t *testing.T) {
	dir := t.TempDir()
	// Override only harassment; the other categories fall back to embedded.
	if err := os.WriteFile(filepath.Join(dir, "harassment.txt"), []byte("touch grass\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	l, err := LoadLexiconDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	g := New()
	g.SetLexicon(l)

	// New term active, old term gone.
	if v := g.Inspect(module.RoleEveryone, "why dont you go touch grass instead of typing here"); v.Action != ActionWarn {
		t.Fatalf("override term should warn: got %s rule=%s", v.Action, v.Rule)
	}
	if v := g.Inspect(module.RoleEveryone, "nobody asked just go kill yourself already dude seriously"); v.Action != ActionNone {
		t.Fatalf("replaced category must drop old terms: got %s rule=%s", v.Action, v.Rule)
	}
	// Fallback category (hate) still present.
	if v := g.Inspect(module.RoleEveryone, "you are such a "+floorTerm(t)+" lol"); v.Action != ActionTimeout {
		t.Fatalf("fallback hate list missing: got %s", v.Action)
	}

	// Missing dir errors; SetLexicon(nil) restores the embedded artifact.
	if _, err := LoadLexiconDir(filepath.Join(dir, "nope")); err == nil {
		t.Fatal("missing dir must error")
	}
	g.SetLexicon(nil)
	if v := g.Inspect(module.RoleEveryone, "nobody asked just go kill yourself already dude seriously"); v.Action != ActionWarn {
		t.Fatalf("embedded restore failed: got %s", v.Action)
	}
}

func TestLinkishSignal(t *testing.T) {
	g := New()
	// Long clean line with a link: no verdict, but Deep+Linkish signals with a
	// SimHash for the campaign juror.
	v, sigs := g.Assess(module.RoleEveryone, "hey friends come check the new highlight video at https://example.com/watch tonight", nil)
	if v.Action != ActionNone {
		t.Fatalf("clean link line must not be actioned: got %s rule=%s", v.Action, v.Rule)
	}
	if !sigs.Deep || !sigs.Linkish || sigs.SimHash == 0 {
		t.Fatalf("signals wrong: %+v", sigs)
	}

	// Clean short line: zero signals (clean path).
	_, sigs = g.Assess(module.RoleEveryone, "nice play", nil)
	if sigs.Deep || sigs.SimHash != 0 {
		t.Fatalf("clean short line must produce zero signals: %+v", sigs)
	}
}
