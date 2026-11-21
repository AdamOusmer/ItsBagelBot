// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
// @ts-ignore Bun supplies this module at test runtime.
import { describe, expect, test } from 'bun:test';
import type { StatusTone } from '@bagel/kit/status-tone';
import type { LaneView } from '#lib/server/lanes';
import { currentAlias, groupLanes, laneTone, renameLane } from './lane-view';

const lane = (stream: string, consumer: string, changes: Partial<LaneView> = {}): LaneView => ({
  stream, consumer, display: consumer, subject: 'events.>', category: 'projection',
  ephemeral: false, orphan: false, pending: 0, inFlight: '0 / 2000', rate: '0 msg/s',
  redelivered: 0, ...changes
});

describe('lane presentation', () => {
  test('groups streams and prioritizes unbound consumers, backlog, then redeliveries', () => {
    const source = [
      lane('quiet', 'consumer10'), lane('busy', 'healthy'),
      lane('quiet', 'consumer2'), lane('busy', 'backlog', { pending: 20 }),
      lane('attention', 'orphan', { orphan: true }),
      lane('busy', 'redelivery', { redelivered: 3 }),
      lane('busy', 'traffic', { ratePerSecond: 12 })
    ];
    const groups = groupLanes(source);
    expect(groups.map(({ stream, lanes, pending }) => [stream, lanes.map((row) => row.consumer), pending])).toEqual([
      ['attention', ['orphan'], 0],
      ['busy', ['backlog', 'redelivery', 'traffic', 'healthy'], 20],
      ['quiet', ['consumer2', 'consumer10'], 0]
    ]);
    expect(source[0].consumer).toBe('consumer10');
    expect(groupLanes([])).toEqual([]);
  });

  test.each([
    [{ connection: 'unknown' }, 'neutral'],
    [{ pending: 5 }, 'warning'],
    [{ orphan: true }, 'danger'],
    [{}, 'success'],
    [{ ephemeral: true }, 'warning'],
    [{ redelivered: 1 }, 'warning'],
    [{ pending: 1, connection: 'unknown' }, 'warning'],
    [{ orphan: true, ephemeral: true }, 'danger']
  ] satisfies [Partial<LaneView>, StatusTone][])('gives lane attention precedence for %j', (changes, tone) => {
    expect(laneTone(lane('stream', 'consumer', changes))).toBe(tone);
  });

  test.each([
    ['', 'Group'],
    ['  operator alias  ', 'operator alias']
  ])('preserves the synthesized display when editing an alias (%j)', (input, display) => {
    const original = lane('stream', 'consumer', { display: 'Group', alias: '', defaultDisplay: 'Group' });
    expect(currentAlias(original)).toBe('');
    expect(renameLane(original, input)).toMatchObject({ alias: input.trim(), display });
  });
});
