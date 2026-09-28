import { describe, expect, test } from 'bun:test';
import { shapeQueue } from './songqueue-view';

const now = 1_000_000;
const entry = (tid: string, requester: string, at = now - 60_000) => ({
  tid,
  title: tid,
  dur: 1000,
  req_id: requester,
  req_name: requester,
  at
});

describe('Spotify queue card', () => {
  test('removes a skipped request and renumbers the survivors', () => {
    const view = shapeQueue(
      { up: [entry('skipped', 'alice'), entry('next', 'bob')] },
      { current: { id: 'external' }, up_next: [{ id: 'next' }] },
      now
    );
    expect(view.up.map((row) => row.title)).toEqual(['next']);
    expect(view.up[0].requester).toBe('bob');
  });

  test('shows the request Spotify actually started', () => {
    const view = shapeQueue(
      {
        current: entry('old', 'alice'),
        up: [entry('skipped', 'bob'), entry('playing', 'carol')]
      },
      { current: { id: 'playing' }, up_next: [] },
      now
    );
    expect(view.current?.title).toBe('playing');
    expect(view.up).toEqual([]);
  });

  test('keeps recent writes and requests beyond a full visible window', () => {
    const fresh = shapeQueue({ up: [entry('fresh', 'alice', now)] }, { current: { id: 'external' }, up_next: [] }, now);
    expect(fresh.up).toHaveLength(1);

    const full = shapeQueue(
      { up: [entry('hidden', 'alice')] },
      {
        current: { id: 'external' },
        up_next: Array.from({ length: 20 }, () => ({ id: 'other' }))
      },
      now
    );
    expect(full.up).toHaveLength(1);
  });

  test('uses the stored queue when Spotify is unreadable', () => {
    const view = shapeQueue({ up: [entry('waiting', 'alice')] }, null, now);
    expect(view.up[0].title).toBe('waiting');
  });

  test.each([
    ['attached for the current request', 'playing', { progress_ms: 42_000, duration_ms: 180_000, playing: true }, { positionMs: 42_000, durationMs: 180_000, playing: true }],
    ['clamped to the duration', 'playing', { progress_ms: 999_000, duration_ms: 180_000 }, { positionMs: 180_000, durationMs: 180_000, playing: false }],
    ['absent from an older service', 'playing', {}, undefined],
    ['absent when another track is current', 'external', { progress_ms: 1, duration_ms: 180_000 }, undefined]
  ])('progress is %s', (_name, currentId, wire, expected) => {
    const view = shapeQueue({ current: entry('playing', 'alice'), up: [] }, { current: { id: currentId }, up_next: [], ...wire }, now);
    expect(view.progress).toEqual(expected);
  });
});
