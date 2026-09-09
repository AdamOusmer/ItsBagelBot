// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// fakePeeks answers a canned value per counter name and records every name it
// was asked for, so "reading never bumps" and "a bumped counter is not read
// again" are asserted rather than assumed.
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

// countingBumps renders an incrementing value per counter, so a template that
// bumps and reads the same counter shows which value won.
type countingBumps struct {
	values map[string]string
	asked  []string
}

func (f *countingBumps) Bump(_ context.Context, name string, addressed bool) string {
	f.asked = append(f.asked, prefixed(name, addressed))
	return f.values[name]
}

func TestStoreReadsWithoutBumping(t *testing.T) {
	bumps := &countingBumps{values: map[string]string{"deaths": "43"}}
	peeks := &fakePeeks{values: map[string]string{"deaths": "42"}}
	chain := Chain{Store{Counters: bumps, Peeks: peeks}}

	assert.Equal(t, "42", render(t, "{count:Deaths}", chain, nil))
	assert.Equal(t, []string{"deaths"}, peeks.asked, "the payload folds before the read")
	assert.Empty(t, bumps.asked, "reading a counter never bumps it")
}

func TestStoreReadsATargetAddressedCounter(t *testing.T) {
	peeks := &fakePeeks{values: map[string]string{"shutups": "7"}}
	chain := Chain{Store{Counters: &countingBumps{}, Peeks: peeks}}

	assert.Equal(t, "7", render(t, "{count:target:shutups}", chain, nil))
	assert.Equal(t, []string{"target:shutups"}, peeks.asked)
}

// A counter nobody has ever bumped renders EMPTY, so its fallback speaks —
// never literal, which would claim the bot has no such token.
func TestStoreRendersAMissingCounterAsEmpty(t *testing.T) {
	peeks := &fakePeeks{values: map[string]string{}}
	chain := Chain{Store{Counters: &countingBumps{}, Peeks: peeks}}

	assert.Equal(t, "", render(t, "{count:nothing}", chain, nil))
	assert.Equal(t, "none yet", render(t, "{count:nothing|none yet}", chain, nil))
}

// The pinned decision: a template that bumps and reads one counter shows the
// POST-bump value on both spans, whichever order they were written in, and the
// read costs no round trip of its own.
func TestStoreReadsThePostBumpValue(t *testing.T) {
	bumps := &countingBumps{values: map[string]string{"deaths": "43"}}
	peeks := &fakePeeks{values: map[string]string{"deaths": "42"}}
	chain := Chain{Store{Counters: bumps, Peeks: peeks}}

	assert.Equal(t, "43 (43 today)", render(t, "{counter:deaths} ({count:Deaths} today)", chain, nil))
	assert.Equal(t, "43 (43 today)", render(t, "{count:deaths} ({counter:Deaths} today)", chain, nil),
		"written order does not change which number the two spans agree on")
	assert.Empty(t, peeks.asked, "the bumped value is reused rather than re-read")
}

// Every spelling that stays literal for {counter:…} stays literal for
// {count:…}: one grammar answers both.
func TestStoreLeavesUnusableReadSpansLiteral(t *testing.T) {
	peeks := &fakePeeks{}
	chain := Chain{Store{Counters: &countingBumps{}, Peeks: peeks}}

	for _, span := range []string{"{count}", "{count:}", "{count:target:}", "{count:bot:feeds}"} {
		assert.Equal(t, span, render(t, span, chain, nil), span)
	}
	assert.Empty(t, peeks.asked)
}

// A store wired for reads alone leaves {counter:…} literal rather than
// silently answering it without bumping, and the other way round.
func TestStoreOwnsOnlyTheTokensItsDepsAnswer(t *testing.T) {
	readOnly := Chain{Store{Peeks: &fakePeeks{values: map[string]string{"deaths": "42"}}}}
	assert.Equal(t, "42 {counter:deaths}", render(t, "{count:deaths} {counter:deaths}", readOnly, nil))

	bumpOnly := Chain{Store{Counters: &countingBumps{values: map[string]string{"deaths": "43"}}}}
	assert.Equal(t, "{count:deaths} 43", render(t, "{count:deaths} {counter:deaths}", bumpOnly, nil))
}
