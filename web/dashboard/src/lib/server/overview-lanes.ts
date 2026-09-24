// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import {
  degradedAnsweredTonight,
  type StreamMeta,
  type StreamCounters,
  type ChatVolume,
  type ActivityFeed,
  type AnsweredTonight
} from '$lib/overview-live';
import { streamMeta } from '$lib/server/stream';
import { streamCounters } from '$lib/server/stream-counters';
import { chatVolume } from '$lib/server/chat-volume';
import { activityFeed } from '$lib/server/activity';

const DEMO = dev && env.DEMO === '1';

function demoOr<T>(pick: (m: typeof import('$lib/server/demo-data')) => T, real: () => Promise<T>): Promise<T> {
  return DEMO ? import('$lib/server/demo-data').then(pick) : real();
}

function answeredFromFeed(feed: ActivityFeed): AnsweredTonight {
  if (!feed.ok) return degradedAnsweredTonight();
  const counts = new Map<string, number>();
  for (const row of feed.rows) {
    if (row.kind !== 'command') continue;
    const trigger = row.text.split(' ')[0];
    if (!trigger) continue;
    counts.set(trigger, (counts.get(trigger) ?? 0) + 1);
  }
  const commands = [...counts.entries()]
    .map(([name, count]) => ({ name, count }))
    .sort((a, b) => b.count - a.count)
    .slice(0, 5);
  return { commands, ok: true };
}

export type OverviewLanes = {
  stream: Promise<StreamMeta>;
  counters: Promise<StreamCounters>;
  volume: Promise<ChatVolume>;
  feed: Promise<ActivityFeed>;
  answered: Promise<AnsweredTonight>;
};

export function overviewLanes(uid: string): OverviewLanes {
  return {
    stream: demoOr<StreamMeta>((m) => m.demoStreamMeta(Date.now()), () => streamMeta(uid)),
    counters: demoOr<StreamCounters>((m) => m.demoStreamCounters, () => streamCounters(uid)),
    volume: demoOr<ChatVolume>((m) => m.demoChatVolume(), () => chatVolume(uid)),
    feed: demoOr<ActivityFeed>((m) => m.demoActivityFeed(Date.now()), () => activityFeed(uid)),

    answered: demoOr<AnsweredTonight>(
      (m) => m.demoAnsweredTonight,
      () => activityFeed(uid).then(answeredFromFeed)
    )
  };
}
