// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

import (
	"os"
	"path/filepath"
	"testing"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/moderation"
	"ItsBagelBot/pkg/codec"
)

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

	cfg := ParseConfig(codec.RawMessage(`{"profile":"adult","allow_terms":"` + slur + `"}`))
	if v := g.InspectWith(module.RoleEveryone, line, cfg); v.Action != ActionTimeout {
		t.Fatalf("hate floor must be immovable: got %s rule=%s", v.Action, v.Rule)
	}
}

func TestLexiconHateShortLine(t *testing.T) {
	g := New()
	slur := floorTerm(t)
	if v := g.Inspect(module.RoleEveryone, slur); v.Action != ActionTimeout {
		t.Fatalf("bare short slur: got %s rule=%s", v.Action, v.Rule)
	}
	if v := g.Inspect(module.RoleEveryone, "nice clip"); v.Action != ActionNone {
		t.Fatalf("short clean line flagged: %s", v.Action)
	}
}

func TestLexiconHateCatchesObfuscation(t *testing.T) {
	g := New()
	slur := floorTerm(t)
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

	cfg := ParseConfig(codec.RawMessage(`{"allow_terms":"kill yourself"}`))
	if v := g.InspectWith(module.RoleEveryone, line, cfg); v.Action != ActionNone {
		t.Fatalf("allow-term should suppress harassment: got %s", v.Action)
	}
}

func TestLexiconProfileGates(t *testing.T) {
	g := New()
	sexual := "check out this hentai stream it is really something else"
	profane := "that was some absolute bullshit refs are blind i swear"
	profane = "well shit that was a terrible play from the team today"

	pg := ParseConfig(codec.RawMessage(`{"profile":"pg"}`))
	adult := ParseConfig(codec.RawMessage(`{"profile":"adult"}`))

	if v := g.Inspect(module.RoleEveryone, sexual); v.Action != ActionDelete {
		t.Fatalf("sexual under moderate: got %s rule=%s", v.Action, v.Rule)
	}
	if v := g.InspectWith(module.RoleEveryone, sexual, adult); v.Action != ActionNone {
		t.Fatalf("sexual under adult must pass: got %s rule=%s", v.Action, v.Rule)
	}

	if v := g.Inspect(module.RoleEveryone, profane); v.Action != ActionNone {
		t.Fatalf("profanity under moderate must pass: got %s rule=%s", v.Action, v.Rule)
	}
	if v := g.InspectWith(module.RoleEveryone, profane, pg); v.Action != ActionDelete {
		t.Fatalf("profanity under pg must delete: got %s rule=%s", v.Action, v.Rule)
	}
}

func TestLexiconWordBounded(t *testing.T) {
	g := New()
	pg := ParseConfig(codec.RawMessage(`{"profile":"pg"}`))
	line := "the class assignment about cocktail recipes is due tomorrow evening"
	if v := g.InspectWith(module.RoleEveryone, line, pg); v.Action != ActionNone {
		t.Fatalf("scunthorpe: got %s rule=%s", v.Action, v.Rule)
	}
}

func TestLexiconNonLatinGuard(t *testing.T) {
	g := New()
	russian := "Привет всем как дела сегодня отличный стрим спасибо за игру друзья"
	if v := g.Inspect(module.RoleEveryone, russian); v.Action != ActionNone {
		t.Fatalf("non-latin chat must not be judged by english lists: got %s rule=%s", v.Action, v.Rule)
	}
}

func TestLexiconDirOverrideAndFallback(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "harassment.txt"), []byte("touch grass\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	l, err := LoadLexiconDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	g := New()
	g.SetLexicon(l)

	if v := g.Inspect(module.RoleEveryone, "why dont you go touch grass instead of typing here"); v.Action != ActionWarn {
		t.Fatalf("override term should warn: got %s rule=%s", v.Action, v.Rule)
	}
	if v := g.Inspect(module.RoleEveryone, "nobody asked just go kill yourself already dude seriously"); v.Action != ActionNone {
		t.Fatalf("replaced category must drop old terms: got %s rule=%s", v.Action, v.Rule)
	}
	if v := g.Inspect(module.RoleEveryone, "you are such a "+floorTerm(t)+" lol"); v.Action != ActionTimeout {
		t.Fatalf("fallback hate list missing: got %s", v.Action)
	}

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
	v, sigs := g.Assess(module.RoleEveryone, "hey friends come check the new highlight video at https://example.com/watch tonight", nil)
	if v.Action != ActionNone {
		t.Fatalf("clean link line must not be actioned: got %s rule=%s", v.Action, v.Rule)
	}
	if !sigs.Deep || !sigs.Linkish || sigs.SimHash == 0 {
		t.Fatalf("signals wrong: %+v", sigs)
	}

	_, sigs = g.Assess(module.RoleEveryone, "nice play", nil)
	if sigs.Deep || sigs.SimHash != 0 {
		t.Fatalf("clean short line must produce zero signals: %+v", sigs)
	}
}
