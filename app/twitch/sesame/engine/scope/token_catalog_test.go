// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"fmt"
	"testing"

	"ItsBagelBot/pkg/tmpl"
)

func TestCommandTokenFamiliesUseTheSharedLexer(t *testing.T) {
	seen := make(map[string]string)
	familyIDs := make(map[string]struct{})
	for _, family := range CommandTokenFamilies() {
		validateTokenFamily(t, family, familyIDs, seen)
	}
}

func validateTokenFamily(t *testing.T, family TokenFamily, familyIDs map[string]struct{}, seen map[string]string) {
	t.Helper()
	if family.ID == "" {
		t.Fatal("token family has an empty ID")
	}
	if _, duplicate := familyIDs[family.ID]; duplicate {
		t.Fatalf("token family %q is declared more than once", family.ID)
	}
	familyIDs[family.ID] = struct{}{}
	if len(family.Examples) == 0 {
		t.Fatalf("token family %q has no examples", family.ID)
	}
	for _, example := range family.Examples {
		validateTokenExample(t, family.ID, example, seen)
	}
	for _, alias := range family.Aliases {
		validateTokenExample(t, family.ID, alias, seen)
	}
}

func validateTokenExample(t *testing.T, familyID, example string, seen map[string]string) {
	t.Helper()
	toks := tmpl.Lex(example)
	validateSingleVarSpan(t, familyID, example, toks)
	validateNonEmptyName(t, familyID, example, toks)
	recordTokenOwner(t, seen, dedupKey(toks[0]), familyID)
}

func validateSingleVarSpan(t *testing.T, familyID, example string, toks []tmpl.Token) {
	t.Helper()
	if len(toks) != 1 || toks[0].Kind != tmpl.KindVar {
		t.Fatalf("family %q: Lex(%q) = %#v, want one variable span", familyID, example, toks)
	}
	if toks[0].Raw != example {
		t.Fatalf("family %q: Lex(%q) raw = %q, want original spelling", familyID, example, toks[0].Raw)
	}
}

func validateNonEmptyName(t *testing.T, familyID, example string, toks []tmpl.Token) {
	t.Helper()
	if toks[0].Name != "" {
		return
	}
	if _, ok := positionalIndex(toks[0].Payload); !ok {
		t.Fatalf("family %q: Lex(%q) produced an empty variable name", familyID, example)
	}
}

func dedupKey(tok tmpl.Token) string {
	if tok.HasPayload {
		return tok.Name + ":payload"
	}
	return tok.Name
}

func recordTokenOwner(t *testing.T, seen map[string]string, key, familyID string) {
	t.Helper()
	if previous, ok := seen[key]; ok && previous != familyID {
		t.Fatalf("token %q appears in families %q and %q", key, previous, familyID)
	}
	seen[key] = familyID
}

func ExampleCommandTokenFamilies() {
	for _, family := range CommandTokenFamilies() {
		fmt.Printf("%s: %d examples\n", family.ID, len(family.Examples))
	}
	// Output:
	// message: 14 examples
	// pure: 8 examples
	// chatters: 3 examples
	// emotes: 4 examples
	// channel: 6 examples
	// viewer: 5 examples
	// modules: 7 examples
	// uses: 1 examples
	// store: 2 examples
	// external: 1 examples
}
