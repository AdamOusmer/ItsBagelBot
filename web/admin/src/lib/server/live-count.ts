// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { dev } from '$app/env';
import { currentLiveBroadcasterIDs } from '#lib/server/live-broadcasters';
import { userLiveTotal } from '#lib/server/services';

const DEMO = dev && process.env.DEMO === '1';

export async function liveUserCount(actorId: string): Promise<number | null> {
  if (DEMO) {
    const { sampleUsers, sampleLiveUserIDs } = await import('#lib/server/demo-data');
    return sampleUsers.filter((user) => sampleLiveUserIDs.has(user.id)).length;
  }
  const ids = await currentLiveBroadcasterIDs();
  if (ids === null) return null;
  return userLiveTotal(actorId, ids).catch(() => null);
}
