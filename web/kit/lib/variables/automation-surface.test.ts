// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { eventKinds } from '../automations/event-kinds';
import { readConsoleTree } from '../i18n/tree-fs';
import type { MessageTree } from '../i18n/types';
import { chipsFor, pinnedFor, sheetFor } from './surfaces';

function lookup(tree: MessageTree, key: string): unknown {
  return key.split('.').reduce<unknown>((node, part) => (node as Record<string, unknown> | undefined)?.[part], tree);
}

describe('automation surface chips', () => {
  test('the base surface offers the viewer and argument tokens', () => {
    expect(chipsFor('automation').map((c) => c.token)).toEqual(['{user}', '{args}', '{arg:key}']);
  });

  test('an event kind adds one chip per event field', () => {
    for (const { kind, fields } of eventKinds()) {
      const tokens = chipsFor({ automation: kind }).map((c) => c.token);
      expect(tokens).toEqual(['{user}', '{args}', '{arg:key}', ...fields.map((f) => `{event.${f.name}}`)]);
    }
  });

  test('an unknown kind falls back to the base chips', () => {
    expect(chipsFor({ automation: 'nope' })).toEqual(chipsFor('automation'));
  });

  test('the sheet is the surface chips only and the strip is capped at six', () => {
    const surface = { automation: 'channel.channel_points_custom_reward_redemption.add' };
    expect(sheetFor(surface)).toEqual(chipsFor(surface));
    expect(pinnedFor(surface)).toHaveLength(6);
  });

  test.each(['en', 'fr'])('every chip hint resolves in the %s catalog', (locale) => {
    const tree = readConsoleTree(locale);
    const chips = eventKinds().flatMap((k) => chipsFor({ automation: k.kind }));
    for (const chip of chips) expect([chip.token, typeof lookup(tree, chip.hintKey!)]).toEqual([chip.token, 'string']);
  });
});
