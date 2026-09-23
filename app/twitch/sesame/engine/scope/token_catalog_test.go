// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"fmt"
	"testing"

	"ItsBagelBot/pkg/tmpl"
)

// TestCommandTokenFamiliesUseTheSharedLexer guards the contract consumed by
// the public variables reference: every example in the runtime catalogue must
// be one complete span under pkg/tmpl's grammar, and its name must be the name
// a resolver receives. This catches documentation/catalogue examples that
// accidentally put a payload in the name, split a dotted token, or use an
// unmatched brace. It deliberately does not assert a value: module gates and
// network-backed dependencies belong to the scope tests beside their
// resolvers.
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
	// Aliases go through the same lexer check as Examples: a stale or
	// malformed alias spelling is exactly as wrong as a malformed example, and
	// parity.test.ts trusts these to be real, resolvable spans.
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

// validateSingleVarSpan checks that example lexes to exactly one variable
// span carrying its original spelling.
func validateSingleVarSpan(t *testing.T, familyID, example string, toks []tmpl.Token) {
	t.Helper()
	if len(toks) != 1 || toks[0].Kind != tmpl.KindVar {
		t.Fatalf("family %q: Lex(%q) = %#v, want one variable span", familyID, example, toks)
	}
	if toks[0].Raw != example {
		t.Fatalf("family %q: Lex(%q) raw = %q, want original spelling", familyID, example, toks[0].Raw)
	}
}

// validateNonEmptyName checks that an empty span name is a real span: only
// the positional leading slice ({:m}, message family) is allowed one, since
// everywhere else it is the catalogue accidentally carrying "{}" or
// "{:notanumber}", which is worth failing loudly on.
func validateNonEmptyName(t *testing.T, familyID, example string, toks []tmpl.Token) {
	t.Helper()
	if toks[0].Name != "" {
		return
	}
	if _, ok := positionalIndex(toks[0].Payload); !ok {
		t.Fatalf("family %q: Lex(%q) produced an empty variable name", familyID, example)
	}
}

// dedupKey is name PLUS whether the span carries a payload, not the name
// alone: {count} (uses, no payload) and {count:deaths} (store, a payload)
// are one name legitimately answered by two families, the exact split
// scope.Uses.Owns/scope.Store.Owns make by inspecting the whole Var rather
// than just its name (see scope.Scope.Owns's doc).
func dedupKey(tok tmpl.Token) string {
	if tok.HasPayload {
		return tok.Name + ":payload"
	}
	return tok.Name
}

// recordTokenOwner fails when key was already claimed by a different
// family — a collision on the (name, has-payload) pair, unlike a collision
// on the bare name, is still a real ambiguity worth failing on.
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
