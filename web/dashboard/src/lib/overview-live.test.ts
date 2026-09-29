// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import {
  clockFace,
  degradedActivityFeed,
  degradedAnsweredTonight,
  degradedChatVolume,
  degradedStreamCounters,
  degradedStreamMeta,
  isFreshChannel,
  mergeLive,
  type LiveSnapshot
} from './overview-live';

function snapshot(): LiveSnapshot {
  return {
    stream: { ...degradedStreamMeta(), ok: true, known: true, viewers: 4 },
    counters: { messages: '10', answered: '2', modActions: '0', ok: true },
    volume: { ...degradedChatVolume(), ok: true, peak: 3 },
    feed: { ...degradedActivityFeed(), ok: true },
    answered: { commands: [{ name: 'hi', count: 2 }], ok: true }
  };
}

describe('mergeLive', () => {
  test('a failed lane keeps the previous good value', () => {
    const first = mergeLive({}, snapshot());
    const next: LiveSnapshot = { ...snapshot(), feed: degradedActivityFeed(), stream: degradedStreamMeta() };
    const merged = mergeLive(first, next);
    expect(merged.feed?.ok).toBe(true);
    expect(merged.stream?.viewers).toBe(4);
  });

  test('a failed lane with no prior value stays absent', () => {
    const merged = mergeLive({}, { ...snapshot(), counters: degradedStreamCounters(), answered: degradedAnsweredTonight() });
    expect(merged.counters).toBeUndefined();
    expect(merged.answered).toBeUndefined();
    expect(merged.stream?.ok).toBe(true);
  });

  test('a good lane replaces the previous value', () => {
    const first = mergeLive({}, snapshot());
    const merged = mergeLive(first, { ...snapshot(), counters: { messages: '11', answered: '2', modActions: '0', ok: true } });
    expect(merged.counters?.messages).toBe('11');
  });
});

describe('clockFace', () => {
  test('empty and invalid input give an empty string', () => {
    expect(clockFace(null)).toBe('');
    expect(clockFace('nope')).toBe('');
  });

  test('uses the locale hour cycle', () => {
    const iso = '2026-01-01T15:05:00Z';
    expect(clockFace(iso, 'en')).toMatch(/[AP]M/);
    expect(clockFace(iso, 'fr')).not.toMatch(/[AP]M/);
  });
});

describe('isFreshChannel', () => {
  const fresh = { ...degradedStreamMeta(), ok: true };

  test('no stream history and no top commands is fresh', () => {
    expect(isFreshChannel(fresh, { ok: true, top: [] })).toBe(true);
  });

  test('known stream, top commands, or failed reads are not fresh', () => {
    expect(isFreshChannel({ ...fresh, known: true }, { ok: true, top: [] })).toBe(false);
    expect(isFreshChannel(fresh, { ok: true, top: [1] })).toBe(false);
    expect(isFreshChannel(fresh, { ok: false, top: [] })).toBe(false);
    expect(isFreshChannel(degradedStreamMeta(), { ok: true, top: [] })).toBe(false);
  });
});
