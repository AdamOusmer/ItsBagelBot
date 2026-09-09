// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tmpl

import (
	"slices"
	"testing"
)

// TestCondSplit pins where a conditional's three parts end: the cond key is
// everything before the LAST two ':' segments, the "=" cut inside it makes the
// equality form, and a payload survives on the cond's own key.
func TestCondSplit(t *testing.T) {
	cases := []struct {
		in      string
		refKey  string
		want    string
		hasWant bool
		then    string
		els     string
	}{
		{"{if:user:hi}", "user", "", false, "hi", ""},
		{"{if:user:hi:bye}", "user", "", false, "hi", "bye"},
		{"{if:touser=bob:yes:no}", "touser", "bob", true, "yes", "no"},
		{"{if:1=:none:some}", "1", "", true, "none", "some"},
		{"{if:count:deaths:none:some}", "count:deaths", "", false, "none", "some"},
		{"{if:count:deaths=0:none:some}", "count:deaths", "0", true, "none", "some"},
		{"{if:val.rank:one:two}", "val.rank", "", false, "one", "two"},
		{"{IF:User:Hi}", "user", "", false, "Hi", ""},
	}
	for _, tc := range cases {
		tok := Lex(tc.in)[0]
		cond, ok := tok.Cond()
		if !ok {
			t.Errorf("Cond(%q) = not a conditional", tc.in)
			continue
		}
		if cond.Ref.Key() != tc.refKey {
			t.Errorf("Cond(%q) ref = %q, want %q", tc.in, cond.Ref.Key(), tc.refKey)
		}
		if cond.Want != tc.want || cond.HasWant != tc.hasWant {
			t.Errorf("Cond(%q) want = %q(%v), want %q(%v)", tc.in, cond.Want, cond.HasWant, tc.want, tc.hasWant)
		}
		if cond.Then != tc.then || cond.Else != tc.els {
			t.Errorf("Cond(%q) branches = %q/%q, want %q/%q", tc.in, cond.Then, cond.Else, tc.then, tc.els)
		}
	}
}

// TestCondNeedsTwoParts pins the spans that are NOT conditionals, so a
// half-written one keeps its braces instead of quietly rendering nothing.
func TestCondNeedsTwoParts(t *testing.T) {
	for _, in := range []string{"{if}", "{if:}", "{if:user}", "{iffy:user:hi}"} {
		if _, ok := Lex(in)[0].Cond(); ok {
			t.Errorf("Cond(%q) = a conditional, want literal passthrough", in)
		}
		if got := Expand(in, func(string) (string, bool) { return "", false }); got != in {
			t.Errorf("Expand(%q) = %q, want it literal", in, got)
		}
	}
}

// TestCondBranchIsLiteral pins the no-nesting rule: a reference is one level
// deep, so a token spelled inside then or else is text and nothing expands it.
func TestCondBranchIsLiteral(t *testing.T) {
	repl := func(key string) (string, bool) {
		val, ok := map[string]string{"user": "bob", "touser": ""}[key]
		return val, ok
	}
	if got := Expand("{if:user:hi bob:hi nobody}", repl); got != "hi bob" {
		t.Errorf("then branch = %q", got)
	}
	// The else branch is reached and its brace text is copied out verbatim:
	// the span closes at the first '}', so the trailing one is literal too.
	if got := Expand("{if:touser:named:nobody {touser}}", repl); got != "nobody {touser}" {
		t.Errorf("else branch = %q", got)
	}
}

// TestWithCondRefs pins that the lexer surfaces the token a cond READS, which
// is what lets a planner mount the scope and batch the lookup before any
// rendering happens.
func TestWithCondRefs(t *testing.T) {
	plain := Lex("hi {user}")
	if got := WithCondRefs(plain); len(got) != len(plain) {
		t.Errorf("WithCondRefs on a plain template grew to %d tokens", len(got))
	}

	toks := Lex("{if:followage:back again:hi} and {if:count:deaths=0:clean:messy}")
	got := WithCondRefs(toks)
	if len(got) != len(toks)+2 {
		t.Fatalf("WithCondRefs = %d tokens, want %d", len(got), len(toks)+2)
	}
	refs := got[len(toks):]
	keys := []string{refs[0].Key(), refs[1].Key()}
	if want := []string{"followage", "count:deaths"}; !slices.Equal(keys, want) {
		t.Errorf("refs = %q, want %q", keys, want)
	}
	if !splitsAs(refs[1], "count", "deaths") {
		t.Errorf("ref split = %q/%q(%v), want count/deaths(true)", refs[1].Name, refs[1].Payload, refs[1].HasPayload)
	}
}

// splitsAs reports whether a cond's ref token carries the expected name and
// payload. A named predicate rather than three ORed comparisons at the call
// site: the assertion reads as the sentence it is checking.
func splitsAs(tok Token, name, payload string) bool {
	return tok.Name == name && tok.Payload == payload && tok.HasPayload
}

// TestCondUnknownStaysLiteral pins the whole-span literal rule: a cond naming
// something nothing resolves is a typo or a module that is off, and taking the
// else branch would hide both.
func TestCondUnknownStaysLiteral(t *testing.T) {
	repl := func(string) (string, bool) { return "", false }
	for _, in := range []string{"{if:missing:x}", "{if:missing:x:y}", "{if:missing=1:x:y}"} {
		if got := Expand(in, repl); got != in {
			t.Errorf("Expand(%q) = %q, want it literal", in, got)
		}
	}
}
