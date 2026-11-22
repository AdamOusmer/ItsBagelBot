// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { compatEvent, compatLoad } from './compat';
import type { OverlayEvent } from './feed';

const base = { id: 'e1', at: '2026-01-01T00:00:00Z', user: { id: '1', login: 'ann', name: 'Ann' } };

test.each([
  ['follow', { kind: 'follow' }, 'follower-latest', { name: 'Ann' }],
  ['sub', { kind: 'sub', months: 3, tier: '1000' }, 'subscriber-latest', { name: 'Ann', amount: 3, tier: '1000', message: '' }],
  ['cheer', { kind: 'cheer', amount: 100, message: { text: 'hi' } }, 'cheer-latest', { name: 'Ann', amount: 100, message: 'hi' }],
  ['raid', { kind: 'raid', amount: 40 }, 'raid-latest', { name: 'Ann', amount: 40 }]
] as const)('%s maps to a %s listener', (_name, partial, listener, event) => {
  expect(compatEvent({ ...base, ...partial } as OverlayEvent)).toEqual({
    name: 'onEventReceived',
    detail: { listener, event }
  });
});

test('load carries the field data and session counts', () => {
  const load = compatLoad({ title: 'Hi' }, { session: { follows: 2 } });
  expect(load.name).toBe('onWidgetLoad');
  expect(load.detail).toMatchObject({ fieldData: { title: 'Hi' }, session: { data: { 'follower-session': { count: 2 } } } });
});
