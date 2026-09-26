// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
// @ts-ignore Bun supplies this module at test runtime.
import { describe, expect, test } from 'bun:test';
import type { LaneView } from '$lib/server/lanes';
import { groupLanes, laneTone } from './lane-view';

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
    expect(groups.map((group) => group.stream)).toEqual(['attention', 'busy', 'quiet']);
    expect(groups[1].lanes.map((row) => row.consumer)).toEqual(['backlog', 'redelivery', 'traffic', 'healthy']);
    expect(groups[1].pending).toBe(20);
    expect(groups[2].lanes.map((row) => row.consumer)).toEqual(['consumer2', 'consumer10']);
    expect(source[0].consumer).toBe('consumer10');
    expect(groupLanes([])).toEqual([]);
  });

  test('does not mark pull consumers with unknown connection as confirmed healthy', () => {
    expect(laneTone(lane('stream', 'pull', { connection: 'unknown' }))).toBe('neutral');
    expect(laneTone(lane('stream', 'backlog', { pending: 5 }))).toBe('warning');
    expect(laneTone(lane('stream', 'orphan', { orphan: true }))).toBe('error');
    expect(laneTone(lane('stream', 'healthy'))).toBe('success');
    expect(laneTone(lane('stream', 'temporary', { ephemeral: true }))).toBe('warning');
    expect(laneTone(lane('stream', 'redelivery', { redelivered: 1 }))).toBe('warning');
    expect(laneTone(lane('stream', 'unknown-backlog', { pending: 1, connection: 'unknown' }))).toBe('warning');
    expect(laneTone(lane('stream', 'unbound-temporary', { orphan: true, ephemeral: true }))).toBe('error');
  });
});
