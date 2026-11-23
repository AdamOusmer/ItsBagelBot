// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { eventKinds, type FeedEvent, type FeedMessages } from './feed';

const root = new URL('../../../internal/domain/overlays/feed/testdata/', import.meta.url);
const golden: FeedMessages & { events: FeedEvent[]; event: FeedEvent } = JSON.parse(readFileSync(new URL('feed.json', root), 'utf8'));

describe('golden feed frames shared with Go', () => {
  test('carry every frame kind', () => {
    expect(Object.keys(golden).sort()).toEqual(['control', 'event', 'events', 'snapshot', 'stats', 'subathon']);
    expect(Object.keys(golden.snapshot).sort()).toEqual(['chat', 'control', 'events', 'stats', 'subathon']);
  });

  test('carry every event kind with a sequence', () => {
    expect(golden.events.map((event) => event.kind).sort()).toEqual([...eventKinds].sort());
    expect(golden.events.every((event) => event.seq > 0)).toBe(true);
  });

  test('carry the stats windows the runtime reads', () => {
    expect(Object.keys(golden.stats).sort()).toEqual([
      'all_time', 'all_time_top_cheerer', 'all_time_top_gifter', 'credits', 'latest', 'month', 'session', 'top_cheerer', 'top_gifter', 'week'
    ]);
    expect(Object.keys(golden.stats.session ?? {}).sort()).toEqual([
      'bits', 'follows', 'gifts', 'raids', 'redemptions', 'resubs', 'sub_points', 'subs', 'tiers'
    ]);
  });
});
