// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { badgeURL } from './events';
import type { OverlayEvent } from './feed';
import { selectVariation } from './variations';
import type { AlertConfig, Condition } from './widget-types';
import { settings } from './defaults';

const base = settings.alertbox.events.sub;
const event = (fields: Partial<OverlayEvent>): OverlayEvent => ({ id: 'e1', kind: 'sub', at: '2026-11-23T00:00:00Z', ...fields });
const config = (condition: Condition, value = ''): AlertConfig => ({ ...base, variations: [{ condition, value, overrides: { title: 'hit' } }] });
const title = (condition: Condition, value: string, fields: Partial<OverlayEvent>) => selectVariation(config(condition, value), event(fields)).title;

describe('alert variation conditions', () => {
  test.each([
    ['anonymous', '', { anonymous: true }, { anonymous: false }],
    ['is_gift', '', { kind: 'gift_sub' as const }, { kind: 'sub' as const }],
    ['prime', '', { tier: 'prime' as const }, { tier: '1000' as const }],
    ['upgrade', '', { upgrade: true }, {}],
    ['months_exact', '12', { months: 12 }, { months: 13 }],
    ['streak_min', '3', { streak: 3 }, { streak: 2 }]
  ] as const)('%s matches only the intended event', (condition, value, match, miss) => {
    expect([title(condition, value, match), title(condition, value, miss)]).toEqual(['hit', base.title]);
  });
});

describe('chat badge images', () => {
  test.each([
    ['5527c58c-fb7d-422d-b71b-f309dcb85cc1', 'https://static-cdn.jtvnw.net/badges/v1/5527c58c-fb7d-422d-b71b-f309dcb85cc1/2'],
    ['moderator', undefined],
    ['../x', undefined]
  ])('%s', (id, want) => {
    expect(badgeURL(id)).toBe(want);
  });
});
