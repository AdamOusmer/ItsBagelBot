// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import {
  degradedAnsweredTonight,
  type StreamMeta,
  type StreamCounters,
  type ChatVolume,
  type ActivityFeed,
  type AnsweredTonight
} from '#lib/overview-live';
import { streamMeta } from '#lib/server/stream';
import { streamCounters } from '#lib/server/stream-counters';
import { chatVolume } from '#lib/server/chat-volume';
import { activityFeed } from '#lib/server/activity';
import { shareRecent } from '#lib/server/recent-share';

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

const LANES_SHARE_MS = 3000;

const sharedLanes = shareRecent<OverviewLanes>(LANES_SHARE_MS);

function readLanes(uid: string): OverviewLanes {
  const feed = activityFeed(uid);
  return {
    stream: streamMeta(uid),
    counters: streamCounters(uid),
    volume: chatVolume(uid),
    feed,
    answered: feed.then(answeredFromFeed)
  };
}

export function overviewLanes(uid: string): OverviewLanes {
  return sharedLanes(uid, () => readLanes(uid));
}
