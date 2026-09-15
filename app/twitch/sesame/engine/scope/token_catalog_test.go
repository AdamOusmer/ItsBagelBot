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
			t.Run(family.ID+"/"+example, func(t *testing.T) {
				toks := tmpl.Lex(example)
				if len(toks) != 1 || toks[0].Kind != tmpl.KindVar {
					t.Fatalf("Lex(%q) = %#v, want one variable span", example, toks)
				}
				if toks[0].Raw != example {
					t.Fatalf("Lex(%q) raw = %q, want original spelling", example, toks[0].Raw)
				}
				if toks[0].Name == "" {
					t.Fatalf("Lex(%q) produced an empty variable name", example)
				}
				if previous, ok := seen[toks[0].Name]; ok && previous != family.ID {
					// Shared names are intentional across surfaces only when a
					// resolver family owns them in the corresponding scope. The
					// command catalogue must still not duplicate one example in
					// two families: that makes availability ambiguous to docs.
					t.Fatalf("token %q appears in families %q and %q", toks[0].Name, previous, family.ID)
				}
				seen[toks[0].Name] = family.ID
			})
		}
	}
}

func ExampleCommandTokenFamilies() {
	for _, family := range CommandTokenFamilies() {
		fmt.Printf("%s: %d examples\n", family.ID, len(family.Examples))
	}
	// Output:
	// message: 12 examples
	// pure: 8 examples
	// chatters: 2 examples
	// emotes: 4 examples
	// channel: 4 examples
	// viewer: 5 examples
	// modules: 6 examples
	// uses: 1 examples
	// store: 4 examples
	// external: 1 examples
}
