// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, mock, test } from 'bun:test';
import { stubSvelteKit } from '../../../test/sveltekit';

stubSvelteKit();

const reads: string[] = [];
const track = <T>(name: string, value: T) => async (uid: string) => {
  reads.push(`${name}:${uid}`);
  return value;
};

mock.module('$lib/server/stream', () => ({ streamMeta: track('stream', { ok: true }) }));
mock.module('$lib/server/stream-counters', () => ({ streamCounters: track('counters', { ok: true }) }));
mock.module('$lib/server/chat-volume', () => ({ chatVolume: track('volume', { ok: true }) }));
mock.module('$lib/server/activity', () => ({
  activityFeed: track('feed', {
    ok: true,
    rows: [
      { kind: 'command', text: '!hi there' },
      { kind: 'command', text: '!hi' },
      { kind: 'mod', text: 'timeout' }
    ]
  })
}));

const { overviewLanes } = await import('./overview-lanes');

async function settle(uid: string) {
  const lanes = overviewLanes(uid);
  return Promise.all([lanes.stream, lanes.counters, lanes.volume, lanes.feed, lanes.answered]);
}

describe('overviewLanes', () => {
  test('answered tonight derives from the same feed read', async () => {
    reads.length = 0;
    const [, , , , answered] = await settle('1');
    expect(reads.filter((r) => r.startsWith('feed:'))).toEqual(['feed:1']);
    expect(answered).toEqual({ commands: [{ name: '!hi', count: 2 }], ok: true });
  });

  test('a render and the stream tick right after it share one set of reads', async () => {
    reads.length = 0;
    await settle('2');
    await settle('2');
    expect(reads.toSorted()).toEqual(['counters:2', 'feed:2', 'stream:2', 'volume:2']);
  });

  test('boards do not share lanes', async () => {
    reads.length = 0;
    await settle('3');
    await settle('4');
    expect(reads.length).toBe(8);
  });
});
