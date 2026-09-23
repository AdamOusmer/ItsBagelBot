// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The guard on the custom-command chip strip (docs/specs/variables-catalog.md
// D5, D8). Was kit/lib/token-palettes.test.ts's "command token palette"
// block, which scraped ResponseEditor.svelte's source for `{ token: '…' }`
// literals; that scrape stopped having anything to find once
// ResponseEditor.svelte started rendering chipsFor('custom') directly
// (nothing hand-written left to read), so the block moved here to check the
// function itself instead of a component's use of it. The reward/trigger
// palettes token-palettes.test.ts also used to scrape (govee, spotify,
// channelpoints, triggers) are gone the same way: those four components now
// render chipsFor('reward:*'|'triggers') too, so their correctness is the
// catalog's — variables/parity.test.ts rules G and H already guard that a
// ReplyToken's hintKey resolves and matches the Go reply-token golden.
import { describe, expect, test } from 'bun:test';
import { lex, type VarToken } from '../engine/tmpl';
import { ownedByCore } from '../engine/rehearsal';
import { chipsFor } from './surfaces';

/** The single var token a chip must lex to, or null when it is not one. */
function soleSpan(chip: string): VarToken | null {
  const tokens = lex(chip);
  if (tokens.length !== 1 || tokens[0].kind !== 'var') return null;
  return tokens[0];
}

describe('custom-command chip strip (chipsFor(\'custom\'))', () => {
  // Mirrors VariablePalette's sheetFor('custom') filter: {counter} and
  // {urlfetch} chips are never inserted as literal text on the custom-command
  // sheet (each opens its own picker instead), so they are excluded here too
  // rather than asserted against ownedByCore — a filter unrelated to whether
  // the core resolves them (EXTERNAL_SCOPE mounts {urlfetch:…} on
  // commandChain now; see rehearsal.ts's own comment on it).
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

describe('pinned chips (was engine/common-tokens.ts\'s five-head cap)', () => {
  // ResponseEditor.svelte and CommandBuilder.astro both cap the custom
  // surface's chip strip to VariableDef.pinned entries; this is the one
  // place the cap itself is a number, not two components trusting it stayed
  // in sync with each other.
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
