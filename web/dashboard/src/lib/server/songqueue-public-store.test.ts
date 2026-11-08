// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import type { SongQueueEntry } from '@bagel/kit/server/songqueue-store';
import { MAX_SONG_ROWS } from '../songqueue-snapshot';
import { publicSongQueue } from './songqueue-public-store';

const NOW = 5_000_000;

const entry = (n: number, at = 1_000 * n): SongQueueEntry => ({
  tid: `t${n}`,
  title: `Song ${n}`,
  artists: ['A', 'B'],
  dur: 1,
  req_id: String(n),
  req_name: `viewer${n}`,
  at
});

describe('publicSongQueue', () => {
  test('rows carry title, joined artists, requester and enqueue time, never track ids or requester ids', () => {
    const view = publicSongQueue({ current: entry(1), up: [entry(2)] }, NOW);
    expect(view).toEqual({
      open: true,
      degraded: false,
      total: 1,
      current: { title: 'Song 1', artists: 'A, B', requester: 'viewer1', joinedAt: 1000 },
      up: [{ title: 'Song 2', artists: 'A, B', requester: 'viewer2', joinedAt: 2000 }]
    });
  });

  test('an empty document is an empty queue, not a degraded one', () => {
    expect(publicSongQueue({}, NOW)).toEqual({ open: true, degraded: false, total: 0, current: null, up: [] });
  });

  test('only the next rows are listed while the total counts them all', () => {
    const up = Array.from({ length: MAX_SONG_ROWS + 5 }, (_, i) => entry(i + 2));
    const view = publicSongQueue({ up }, NOW);
    expect([view.up.length, view.total]).toEqual([MAX_SONG_ROWS, MAX_SONG_ROWS + 5]);
  });

  test('an unknown enqueue time reads as just requested', () => {
    expect(publicSongQueue({ up: [entry(2, 0)] }, NOW).up[0].joinedAt).toBe(NOW);
  });
});
