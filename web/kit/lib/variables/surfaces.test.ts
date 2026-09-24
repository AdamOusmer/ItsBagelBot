// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { lex, type VarToken } from '../engine/tmpl';
import { ownedByCore } from '../engine/rehearsal';
import { chipsFor } from './surfaces';

function soleSpan(chip: string): VarToken | null {
  const tokens = lex(chip);
  if (tokens.length !== 1 || tokens[0].kind !== 'var') return null;
  return tokens[0];
}

describe('custom-command chip strip (chipsFor(\'custom\'))', () => {
  const chips = chipsFor('custom')
    .map((c) => c.token)
    .filter((token) => !token.startsWith('{counter') && !token.startsWith('{urlfetch'));

  test('the palette is not empty', () => {
    expect(chips.length).toBeGreaterThan(30);
  });

  test('every chip inserts exactly one token', () => {
    for (const chip of chips) expect(soleSpan(chip)).not.toBeNull();
  });

  test('every chip names a token the core resolves', () => {
    for (const chip of chips) expect([chip, ownedByCore(soleSpan(chip)!.name)]).toEqual([chip, true]);
  });

  test('no chip carries a fallback', () => {
    for (const chip of chips) expect(soleSpan(chip)!.fallback).toBeNull();
  });
});

describe('pinned chips (was engine/common-tokens.ts\'s five-head cap)', () => {
  test('at most six pinned chips on the custom surface', () => {
    const pinned = chipsFor('custom').filter((c) => c.pinned);
    expect(pinned.length).toBeLessThanOrEqual(6);
  });

  test('pinned chips are exactly user, args, touser, random, uptime, if', () => {
    const pinned = chipsFor('custom')
      .filter((c) => c.pinned)
      .map((c) => c.token);
    expect(pinned).toEqual(['{user}', '{touser}', '{args}', '{random}', '{if:touser:hi there:hi everyone}', '{uptime}']);
  });
});
