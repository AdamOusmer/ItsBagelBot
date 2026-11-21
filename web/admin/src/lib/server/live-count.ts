// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { currentLiveBroadcasterIDs } from './live-broadcasters';

export async function liveUserCount(_actorId: string): Promise<number | null> {
  const ids = await currentLiveBroadcasterIDs();
  return ids?.length ?? null;
}
