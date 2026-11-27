// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { currentLiveBroadcasterIDs } from '#lib/server/live-broadcasters';
import { userLiveTotal } from '#lib/server/services';

export async function liveUserCount(actorId: string): Promise<number | null> {
  const ids = await currentLiveBroadcasterIDs();
  if (ids === null) return null;
  return userLiveTotal(actorId, ids).catch(() => null);
}
