// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// fakePeeks answers a canned value per counter name and records every name it
// was asked for, so "both spellings read, neither bumps" and "one lookup
// answers both spellings" are asserted rather than assumed.
type fakePeeks struct {
	values map[string]string
	asked  []string
}

func (f *fakePeeks) Peek(_ context.Context, name string, addressed bool) string {
	f.asked = append(f.asked, prefixed(name, addressed))
	return f.values[name]
}

func prefixed(name string, addressed bool) string {
	if addressed {
		return "target:" + name
	}
	return name
}

func TestStoreReadsBothSpellingsFromOneLookup(t *testing.T) {
	peeks := &fakePeeks{values: map[string]string{"deaths": "42"}}
	chain := Chain{Store{Peeks: peeks}}

	assert.Equal(t, "42", render(t, "{count:Deaths}", chain, nil))
	assert.Equal(t, []string{"deaths"}, peeks.asked, "the payload folds before the read")

	peeks.asked = nil
	assert.Equal(t, "42 (42 too)", render(t, "{counter:deaths} ({count:Deaths} too)", chain, nil),
		"counter and count are aliases of the same read")
	assert.Equal(t, []string{"deaths"}, peeks.asked, "one counter named twice costs one lookup")
}

func TestStoreReadsATargetAddressedCounter(t *testing.T) {
	peeks := &fakePeeks{values: map[string]string{"shutups": "7"}}
	chain := Chain{Store{Peeks: peeks}}

	assert.Equal(t, "7", render(t, "{count:target:shutups}", chain, nil))
	assert.Equal(t, []string{"target:shutups"}, peeks.asked)
	assert.Equal(t, "7", render(t, "{counter:target:shutups}", chain, nil))
}

// A counter nobody has ever bumped (whether by the old token or the current
// command-run option) renders EMPTY, so its fallback speaks — never literal,
// which would claim the bot has no such token.
func TestStoreRendersAMissingCounterAsEmpty(t *testing.T) {
	peeks := &fakePeeks{values: map[string]string{}}
	chain := Chain{Store{Peeks: peeks}}

	assert.Equal(t, "", render(t, "{count:nothing}", chain, nil))
	assert.Equal(t, "none yet", render(t, "{count:nothing|none yet}", chain, nil))
}

// Neither spelling ever writes: this is the whole point of the read-only
// scope. There is no Counters/Bump dependency left to assert "never called"
// against — the type does not exist any more.
func TestStoreNeverWrites(t *testing.T) {
	peeks := &fakePeeks{values: map[string]string{"deaths": "42"}}
	chain := Chain{Store{Peeks: peeks}}

	assert.Equal(t, "42 42", render(t, "{counter:deaths} {count:deaths}", chain, nil))
}

// Every spelling that stays literal for {counter:…} stays literal for
// {count:…}: one grammar answers both. {count:bot:feeds} used to be reserved
// (an admin-only bot-scope counter); dropping that prefix means it is now an
// ordinary counter name, so it is no longer in this literal set.
func TestStoreLeavesUnusableReadSpansLiteral(t *testing.T) {
	peeks := &fakePeeks{}
	chain := Chain{Store{Peeks: peeks}}

	for _, span := range []string{"{count:}", "{count:target:}"} {
		assert.Equal(t, span, render(t, span, chain, nil), span)
	}
	assert.Empty(t, peeks.asked)
}

// Bare {count} is not this scope's: it is the {uses} alias, owned by Uses.
// With no Uses scope mounted, it stays literal like any other unowned span.
func TestStoreDoesNotOwnBareCount(t *testing.T) {
	chain := Chain{Store{Peeks: &fakePeeks{}}}
	assert.Equal(t, "{count}", render(t, "{count}", chain, nil))
}

// A Store with no Peeks dependency leaves every counter token literal rather
// than silently answering it as empty.
func TestStoreUnmountedLeavesTokensLiteral(t *testing.T) {
	chain := Chain{Store{}}
	assert.Equal(t, "{counter:deaths} {count:deaths}", render(t, "{counter:deaths} {count:deaths}", chain, nil))
}
