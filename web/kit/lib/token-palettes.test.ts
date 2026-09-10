// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The guard on every insert-chip palette in the dashboard.
//
// A palette is UI copy — a hand-written list of the tokens a surface offers —
// and it stays that way: nothing here generates it. What it must never be is
// WRONG, and a palette can be wrong in two ways a reviewer does not see:
//
//  1. The chip is not one token. A chip inserts literal text, so a stray '}'
//     or '|' in the copy inserts a span the engine re-cuts, and the
//     broadcaster who clicked it gets a command that reads right and resolves
//     to something else.
//  2. The chip names a token nothing resolves. Chips outlive the tokens they
//     were written for; a renamed or dropped scope leaves a chip that inserts
//     a span chat prints verbatim, braces and all.
//
// Both are checked by reading the components' SOURCE rather than importing
// them: the lists are module-local consts inside .svelte files, and the point
// of the guard is that they can stay exactly where the person editing the copy
// expects to find them. This is the same shape as the Go side's
// internal/buildguard, which reads source to assert a fact no type can.

import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { describe, expect, test } from 'bun:test';
import { lex, type VarToken } from './engine/tmpl';
import { ownedByCore } from './engine/rehearsal';

const dashboard = join(dirname(import.meta.path), '..', '..', 'dashboard', 'src', 'lib', 'components');

// CHIP matches one palette entry's inserted text: `{ token: '…' …`.
const CHIP = /\{\s*token:\s*'([^']*)'/g;

function chipsOf(file: string): string[] {
  const source = readFileSync(join(dashboard, file), 'utf8');
  return [...source.matchAll(CHIP)].map((m) => m[1]);
}

/** The single var token a chip must lex to, or null when it is not one. */
function soleSpan(chip: string): VarToken | null {
  const tokens = lex(chip);
  if (tokens.length !== 1 || tokens[0].kind !== 'var') return null;
  return tokens[0];
}

describe('command token palette (ResponseEditor DEFAULT_TOKENS)', () => {
  const chips = chipsOf(join('commands', 'ResponseEditor.svelte'));

  test('the palette is not empty', () => {
    expect(chips.length).toBeGreaterThan(30);
  });

  test('every chip inserts exactly one token', () => {
    for (const chip of chips) expect(soleSpan(chip)).not.toBeNull();
  });

  test('every chip names a token the core resolves', () => {
    // ownedByCore is built from rehearsal's own scope chain, so this fails the
    // day a scope is renamed or dropped — which is the day the chip started
    // inserting a span chat would print verbatim.
    for (const chip of chips) expect([chip, ownedByCore(soleSpan(chip)!.name)]).toEqual([chip, true]);
  });

  test('no chip carries a fallback', () => {
    // The '|' grammar is real but is not documented on chips yet (the
    // args/fallback work owns that copy), so a '|' in a chip today is a typo
    // that would silently turn the tail into fallback text.
    for (const chip of chips) expect(soleSpan(chip)!.fallback).toBeNull();
  });
});

// --- the inline palettes -----------------------------------------------------

// A module-reply surface offers its OWN tokens ({color}, {track}, {reward}),
// which no command scope owns: they are resolved from that component's sample
// map. So the rule is one of the two — the core owns the name, or the
// component declares a sample for it — and a chip that satisfies neither is a
// chip nothing will expand.
const INLINE_PALETTES = [
  join('govee', 'GoveeRewardEditor.svelte'),
  join('spotify', 'SpotifyRewardEditor.svelte'),
  join('channelpoints', 'RewardEditor.svelte'),
  join('modules', 'TriggerRuleEditor.svelte')
];

function declaresSample(file: string, name: string): boolean {
  const source = readFileSync(join(dashboard, file), 'utf8');
  return new RegExp(`^\\s*${name}:`, 'm').test(source);
}

describe('inline reward and trigger palettes', () => {
  for (const file of INLINE_PALETTES) {
    test(file, () => {
      const chips = chipsOf(file);
      expect(chips.length).toBeGreaterThan(0);
      for (const chip of chips) {
        const span = soleSpan(chip);
        expect([chip, span !== null]).toEqual([chip, true]);
        expect([chip, span!.fallback]).toEqual([chip, null]);
        const resolved = ownedByCore(span!.name) || declaresSample(file, span!.name);
        expect([chip, resolved]).toEqual([chip, true]);
      }
    });
  }
});
