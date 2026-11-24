// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { FeedState } from './feed-state';
import { eventKinds, type FeedEvent, type FeedMessages } from './feed';

const root = new URL('../../../internal/domain/overlays/feed/testdata/', import.meta.url);
const golden: FeedMessages & { events: FeedEvent[]; event: FeedEvent } = JSON.parse(readFileSync(new URL('feed.json', root), 'utf8'));

describe('golden feed frames shared with Go', () => {
  test('carry every frame kind', () => {
    expect(Object.keys(golden).sort()).toEqual(['control', 'credits', 'event', 'events', 'snapshot', 'stats', 'subathon']);
    expect(Object.keys(golden.snapshot).sort()).toEqual(['chat', 'control', 'events', 'stats', 'subathon']);
  });

  test('credits frames merge by user id and survive stats frames', () => {
    const state = new FeedState();
    state.load(golden.snapshot);
    state.credits(golden.credits);
    state.stats({ ...golden.stats, credits: undefined });
    const cheer = state.snapshot.stats.credits?.cheer ?? [];
    expect(cheer.filter((entry) => entry.id === '101')).toEqual(golden.credits.cheer ?? []);
    expect(state.snapshot.stats.credits?.sub?.find((entry) => entry.id === '102')?.amount).toBe(2);
  });

  test('carry every event kind', () => {
    expect(golden.events.map((event) => event.kind).sort()).toEqual([...eventKinds].sort());
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
