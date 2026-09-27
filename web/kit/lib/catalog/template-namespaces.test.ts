// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { namespaceReplyTemplate } from './template-namespaces';
import { MODULE_CATALOG } from './index';
import { rehearseReply, rehearseTimer } from '../engine/rehearsal';

test('legacy module templates migrate without changing dynamic or unknown tokens', () => {
  const reply = { tokens: [{ name: 'valorant:tier', sample: 'Gold 2' }] };
  const legacy = 'Rank {TIER|unranked}; {if:tier=Gold 2:great:climb}; {random:1-6}; {unknown}; {valorant:tier}';
  const migrated = namespaceReplyTemplate('valorant', reply, legacy);
  expect(migrated).toBe('Rank {valorant:tier|unranked}; {if:valorant:tier=Gold 2:great:climb}; {random:1-6}; {unknown}; {valorant:tier}');
  expect(namespaceReplyTemplate('valorant', reply, migrated)).toBe(migrated);
  expect(namespaceReplyTemplate('valorant', reply, '{tier:unsupported}')).toBe('{tier:unsupported}');
  expect(namespaceReplyTemplate('valorant', reply, '{if:tier:ranked}')).toBe('{if:valorant:tier:ranked:}');
  expect(namespaceReplyTemplate('valorant', reply, '{if:tier:ranked|unranked}')).toBe('{if:valorant:tier:ranked:|unranked}');
  const ambiguous = '{if:tier:ranked {player}:unranked}';
  const kept = namespaceReplyTemplate('valorant', reply, ambiguous);
  expect(kept).toBe(ambiguous);
  expect(namespaceReplyTemplate('valorant', reply, kept)).toBe(kept);
  expect(rehearseReply(kept, { tier: 'Gold 2' })).toEqual(rehearseReply(ambiguous, { tier: 'Gold 2' }));
});

test('public module palettes and defaults rehearse with namespaced fields', () => {
  for (const mod of MODULE_CATALOG) {
    for (const reply of mod.replies) {
      for (const token of reply.tokens ?? []) expect(token.name.startsWith(`${mod.id}:`)).toBe(true);
      const samples = Object.fromEntries((reply.tokens ?? []).map((token) => [token.name, token.sample]));
      expect(rehearseReply(reply.defaultMessage, samples).flatMap((line) => line.segments).every((segment) => segment.kind !== 'unknown')).toBe(true);
    }
  }
});


test('timer rehearsal recognizes namespace facts alongside its legacy variables', () => {
  const lines = rehearseTimer('It is {time} on {time:date}, with {valorant:tier|unknown}');
  expect(lines).toHaveLength(1);
  expect(lines[0].segments.some((segment) => segment.kind === 'unknown')).toBe(false);
});

test('valid one-branch conditions and fallbacks keep their rendered response after migration', () => {
  const reply = { tokens: [{ name: 'valorant:tier', sample: 'Gold 2' }] };
  for (const legacy of ['{if:tier:ranked}', '{if:tier:ranked|unavailable}', '{if:tier=Gold 2:ranked:climb}', '{tier|unranked}']) {
    const migrated = namespaceReplyTemplate('valorant', reply, legacy);
    expect(namespaceReplyTemplate('valorant', reply, migrated)).toBe(migrated);
    for (const value of ['Gold 2', 'Silver 3', '']) {
      expect(rehearseReply(migrated, { 'valorant:tier': value })).toEqual(rehearseReply(legacy, { tier: value }));
    }
  }
});
