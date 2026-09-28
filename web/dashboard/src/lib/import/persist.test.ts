import { describe, expect, test } from 'bun:test';
import { parseSnapshot } from './persist';

const STAGES = ['pick', 'instructions', 'commands', 'extras', 'review', 'done'];

describe('parseSnapshot', () => {
  test('rejects junk', () => {
    expect(parseSnapshot(null, STAGES)).toBeNull();
    expect(parseSnapshot('not json', STAGES)).toBeNull();
    expect(parseSnapshot('[1]', STAGES)).toBeNull();
  });

  test('normalises unknown source and stage', () => {
    const snap = parseSnapshot(JSON.stringify({ source: 'nope', stage: 'zzz' }), STAGES);
    expect(snap?.source).toBe('');
    expect(snap?.stage).toBe('pick');
    expect(snap?.selected).toEqual({});
  });

  test('keeps a valid review session', () => {
    const raw = JSON.stringify({
      source: 'nightbot',
      stage: 'review',
      overwrite: true,
      previewResult: { stats: {}, manifest: {} },
      selected: { 'commands:0': false }
    });
    const snap = parseSnapshot(raw, STAGES);
    expect(snap?.source).toBe('nightbot');
    expect(snap?.stage).toBe('review');
    expect(snap?.overwrite).toBe(true);
    expect(snap?.selected['commands:0']).toBe(false);
  });
});
