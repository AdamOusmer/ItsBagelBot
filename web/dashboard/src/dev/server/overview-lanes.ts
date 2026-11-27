// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { OverviewLanes } from '#lib/server/overview-lanes';
import { demoActivityFeed, demoAnsweredTonight, demoChatVolume, demoStreamCounters, demoStreamMeta } from '../fixtures';

export * from '#lib/server/overview-lanes';

export function overviewLanes(): OverviewLanes {
  return {
    stream: Promise.resolve(demoStreamMeta()),
    counters: Promise.resolve(demoStreamCounters),
    volume: Promise.resolve(demoChatVolume()),
    feed: Promise.resolve(demoActivityFeed()),
    answered: Promise.resolve(demoAnsweredTonight)
  };
}
